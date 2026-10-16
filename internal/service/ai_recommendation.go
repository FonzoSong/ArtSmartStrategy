package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

// AIModelConfig contains the provider endpoint and model selected for a team.
// API keys are passed separately and are never included in the prompt.
type AIModelConfig struct {
	BaseURL string
	Model   string
}

// AIRecommendationConstraints are user preferences. The saved team schedule
// policy remains authoritative when output is validated and applied.
type AIRecommendationConstraints struct {
	StartDate          string  `json:"startDate"`
	WorkingDays        []int   `json:"workingDays"`
	DailyCapacityHours float64 `json:"dailyCapacityHours"`
	IncludeWeekends    *bool   `json:"includeWeekends"`
}

// AIRecommendationOptions adds request-specific guidance without changing the
// database-backed scheduling policy.
type AIRecommendationOptions struct {
	Strategy      string                      `json:"strategy"`
	Constraints   AIRecommendationConstraints `json:"constraints"`
	ReferenceTime time.Time                   `json:"-"`
	Timeout       time.Duration               `json:"-"`
}

// AIRecommendationResponseError retains a bounded raw model response for
// internal diagnostics after the schema-validation retry is exhausted.
type AIRecommendationResponseError struct {
	ValidationError string
	RawResponse     string
}

func (e *AIRecommendationResponseError) Error() string {
	return "模型响应未通过结构化校验：" + e.ValidationError
}

// GenerateAIRecommendations builds a same-tag-only metrics prompt, requests
// structured JSON from an OpenAI-compatible model, validates it against the
// candidate set and policy, and returns only normalized recommendations.
func GenerateAIRecommendations(ctx context.Context, model AIModelConfig, key string, candidates []domain.Content, samples []PerformanceSample, policy SlotPolicy, options ...AIRecommendationOptions) ([]Suggestion, error) {
	option := AIRecommendationOptions{Strategy: "balanced"}
	if len(options) > 0 {
		option = options[0]
		if option.Strategy == "" {
			option.Strategy = "balanced"
		}
	}
	if option.ReferenceTime.IsZero() {
		option.ReferenceTime = time.Now()
	}
	tagMatchedHistory := make([]map[string]any, 0, len(candidates))
	promptCandidates := make([]domain.Content, 0, len(candidates))
	for _, candidate := range candidates {
		matching := TagMatchedSamples(candidate, samples)
		promptHistory := append([]PerformanceSample(nil), matching...)
		if len(promptHistory) > 50 {
			promptHistory = promptHistory[:50]
		}
		for historyIndex := range promptHistory {
			promptHistory[historyIndex].Title = truncateAIText(promptHistory[historyIndex].Title, 240)
			promptHistory[historyIndex].Tags = truncateAITags(promptHistory[historyIndex].Tags)
		}
		candidateForPrompt := candidate
		candidateForPrompt.Title = truncateAIText(candidate.Title, 240)
		candidateForPrompt.Tags = truncateAITags(candidate.Tags)
		promptCandidates = append(promptCandidates, candidateForPrompt)
		tagMatchedHistory = append(tagMatchedHistory, map[string]any{
			"target_content_id":       candidate.ID,
			"shared_tags":             truncateAITags(candidate.Tags),
			"published_history":       promptHistory,
			"metric_aggregates":       aggregateTagMetrics(matching, option.ReferenceTime),
			"truncated_history_count": len(matching) - len(promptHistory),
		})
	}
	promptData, err := json.Marshal(map[string]any{"strategy": option.Strategy, "constraints": option.Constraints, "candidates": promptCandidates, "tag_matched_history": tagMatchedHistory, "schedule_policy": policy})
	if err != nil {
		return nil, errors.New("准备 AI 分析输入失败")
	}
	if len(promptData) > 1<<20 {
		return nil, errors.New("同 Tag 历史数据过多，无法安全放入 AI 请求")
	}
	systemPrompt := "你是内容排期分析器。只为输入中待排期内容排序。历史表现只能参考该内容共享 Tag 的已发布记录，不得将无共享 Tag 的记录当成依据；只使用给定指标，不得编造缺失的观看时长或表现。必须遵守排期规则集，用户偏好不能覆盖规则集。样本不足或指标缺失时，必须在 reason 或 risk_notes 中说明。只返回一个 JSON 对象，格式为 {\"recommendations\":[{\"target_content_id\":\"候选 ID\",\"planned_date\":\"YYYY-MM-DD\",\"time_slot\":\"HH:MM\",\"priority\":\"high|medium|low\",\"priority_order\":1,\"reason\":\"依据\",\"confidence\":0.0,\"risk_notes\":\"风险或冲突\"}]}。confidence 必须是 0 到 1 的数字。不得生成输入外的内容 ID、Tag 或指标，也不得返回 Markdown。"
	timeout := option.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	userPrompt := string(promptData)
	var previousRaw, previousValidationError string
	for attempt := 0; attempt < 2; attempt++ {
		raw, requestErr := requestAICompletion(requestCtx, model, key, systemPrompt, userPrompt, attempt)
		if requestErr != nil {
			if errors.Is(requestErr, context.DeadlineExceeded) {
				return nil, fmt.Errorf("AI 等待超过配置上限 %s: %w", timeout, context.DeadlineExceeded)
			}
			if attempt > 0 && previousRaw != "" {
				return nil, &AIRecommendationResponseError{ValidationError: "重试请求失败：" + requestErr.Error() + "；首次校验失败：" + previousValidationError, RawResponse: truncateAIText(previousRaw, 8000)}
			}
			return nil, requestErr
		}
		generated, validationErr := parseAIRecommendationResponse(raw)
		if validationErr == nil {
			var validated []Suggestion
			validated, validationErr = ValidateAIRecommendations(option.ReferenceTime, generated, candidates, samples, policy)
			if validationErr == nil {
				return validated, nil
			}
		}
		if attempt == 0 {
			previousRaw, previousValidationError = raw, validationErr.Error()
			userPrompt += "\n\n上一次 JSON 未通过校验：" + validationErr.Error() + "。请仅返回修正后的完整 JSON 对象。"
			continue
		}
		return nil, &AIRecommendationResponseError{ValidationError: validationErr.Error(), RawResponse: truncateAIText(raw, 8000)}
	}
	return nil, errors.New("模型未返回有效建议")
}

func requestAICompletion(ctx context.Context, model AIModelConfig, key, systemPrompt, userPrompt string, attempt int) (string, error) {
	messages := []map[string]string{{"role": "system", "content": systemPrompt}, {"role": "user", "content": userPrompt}}
	if attempt > 0 {
		messages = append(messages, map[string]string{"role": "user", "content": "只返回符合 JSON schema 的建议，不要添加 Markdown 或解释。"})
	}
	body, err := json.Marshal(map[string]any{
		"model": model.Model, "temperature": 0.2, "response_format": map[string]string{"type": "json_object"}, "messages": messages,
	})
	if err != nil {
		return "", errors.New("编码 AI 请求失败")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenAIURL(model.BaseURL, "/chat/completions"), strings.NewReader(string(body)))
	if err != nil {
		return "", errors.New("构造 AI 请求失败")
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("AI 请求等待已超时: %w", context.DeadlineExceeded)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", fmt.Errorf("AI 请求已取消: %w", context.Canceled)
		}
		return "", errors.New("网络请求失败")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("服务返回 HTTP %d", response.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&completion); err != nil || len(completion.Choices) == 0 {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("AI 响应读取等待已超时: %w", context.DeadlineExceeded)
		}
		return "", errors.New("模型响应格式无效")
	}
	return completion.Choices[0].Message.Content, nil
}

func parseAIRecommendationResponse(raw string) ([]StructuredAIRecommendation, error) {
	var generated struct {
		Recommendations []StructuredAIRecommendation `json:"recommendations"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&generated); err != nil || generated.Recommendations == nil {
		return nil, errors.New("模型没有返回预期 JSON")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("模型返回了多个 JSON 值")
	}
	return generated.Recommendations, nil
}

func truncateAIText(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return string(runes)
}

func truncateAITags(tags []string) []string {
	limited := make([]string, 0, len(tags))
	for _, tag := range tags {
		if value := truncateAIText(tag, 80); value != "" {
			limited = append(limited, value)
			if len(limited) == 20 {
				break
			}
		}
	}
	return limited
}

type metricTrend struct {
	RecentMean   *float64 `json:"recent_mean"`
	PreviousMean *float64 `json:"previous_mean"`
	ChangePct    *float64 `json:"change_percent"`
}

type metricAggregate struct {
	SampleSize int         `json:"sample_size"`
	Mean       float64     `json:"mean"`
	Median     float64     `json:"median"`
	Trend7d    metricTrend `json:"trend_7d"`
	Trend30d   metricTrend `json:"trend_30d"`
}

func aggregateTagMetrics(samples []PerformanceSample, now time.Time) map[string]metricAggregate {
	metrics := map[string]func(PerformanceSample) float64{
		"plays":           func(s PerformanceSample) float64 { return float64(s.Views) },
		"likes":           func(s PerformanceSample) float64 { return float64(s.Likes) },
		"comments":        func(s PerformanceSample) float64 { return float64(s.Comments) },
		"shares":          func(s PerformanceSample) float64 { return float64(s.Shares) },
		"favorites":       func(s PerformanceSample) float64 { return float64(s.Saves) },
		"completion_rate": func(s PerformanceSample) float64 { return s.Completion },
		"profile_visits":  func(s PerformanceSample) float64 { return float64(s.ProfileVisits) },
		"follows":         func(s PerformanceSample) float64 { return float64(s.Follows) },
		"attendance":      func(s PerformanceSample) float64 { return float64(s.Attendance) },
		"tickets":         func(s PerformanceSample) float64 { return float64(s.Tickets) },
	}
	result := make(map[string]metricAggregate, len(metrics))
	for name, read := range metrics {
		values := make([]float64, 0, len(samples))
		for _, sample := range samples {
			values = append(values, read(sample))
		}
		sort.Float64s(values)
		if len(values) == 0 {
			continue
		}
		sum := 0.0
		for _, value := range values {
			sum += value
		}
		result[name] = metricAggregate{SampleSize: len(values), Mean: sum / float64(len(values)), Median: median(values), Trend7d: calculateMetricTrend(samples, read, now, 7), Trend30d: calculateMetricTrend(samples, read, now, 30)}
	}
	return result
}

func median(sortedValues []float64) float64 {
	middle := len(sortedValues) / 2
	if len(sortedValues)%2 == 1 {
		return sortedValues[middle]
	}
	return (sortedValues[middle-1] + sortedValues[middle]) / 2
}

func calculateMetricTrend(samples []PerformanceSample, read func(PerformanceSample) float64, now time.Time, days int) metricTrend {
	recent, previous := make([]float64, 0), make([]float64, 0)
	recentStart, previousStart := now.AddDate(0, 0, -days), now.AddDate(0, 0, -2*days)
	for _, sample := range samples {
		if sample.PublishedAt.After(recentStart) && !sample.PublishedAt.After(now) {
			recent = append(recent, read(sample))
		} else if sample.PublishedAt.After(previousStart) && !sample.PublishedAt.After(recentStart) {
			previous = append(previous, read(sample))
		}
	}
	trend := metricTrend{}
	if len(recent) > 0 {
		recentMean := average(recent)
		trend.RecentMean = &recentMean
	}
	if len(previous) > 0 {
		previousMean := average(previous)
		trend.PreviousMean = &previousMean
	}
	if trend.RecentMean != nil && trend.PreviousMean != nil && *trend.PreviousMean != 0 {
		recentMean, previousMean := *trend.RecentMean, *trend.PreviousMean
		change := (recentMean - previousMean) / previousMean * 100
		trend.ChangePct = &change
	}
	return trend
}

func average(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

// OpenAIURL appends a chat-completions path without duplicating the /v1 prefix.
func OpenAIURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	for strings.HasSuffix(base, "/v1/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + path
}
