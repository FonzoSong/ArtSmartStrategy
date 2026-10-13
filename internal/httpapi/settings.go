package httpapi

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// aiConfigInput is the wire contract of POST /api/ai/configs (creating an extra
// model profile). It accepts the same optional fields the settings page submits.
type aiConfigInput struct {
	Name              string          `json:"name"`
	BaseURL           string          `json:"base_url"`
	Model             string          `json:"model"`
	APIKey            string          `json:"api_key"`
	Temperature       float64         `json:"temperature"`
	RequestMode       string          `json:"request_mode"`
	AuthType          string          `json:"auth_type"`
	MinSample         int             `json:"min_sample"`
	CompletionWeight  float64         `json:"score_completion_weight"`
	InteractionWeight float64         `json:"score_interaction_weight"`
	Enabled           *bool           `json:"enabled"`
	FallbackEnabled   *bool           `json:"fallback_enabled"`
	PlatformWeights   json.RawMessage `json:"platform_weights"`
}

// modelSettings is the resolved, persisted model configuration.
type modelSettings struct {
	Name               string
	BaseURL            string
	Model              string
	MinSample          int
	CompletionWeight   float64
	InteractionWeight  float64
	Temperature        float64
	RequestMode        string
	AuthType           string
	Enabled            bool
	FallbackEnabled    bool
	PlatformWeightsRaw string
}

// modelSettingsRequest is the wire contract of PUT /api/settings. Every optional
// field is a pointer so "not sent" keeps the stored value while an explicit value
// (including an empty string or false) is still honoured. The optional fields
// cover exactly what the settings page submits.
type modelSettingsRequest struct {
	Name                *string         `json:"name"`
	BaseURL             *string         `json:"base_url"`
	Model               *string         `json:"model"`
	APIKey              *string         `json:"api_key"`
	MinSample           *int            `json:"min_sample"`
	CompletionWeight    *float64        `json:"score_completion_weight"`
	InteractionWeight   *float64        `json:"score_interaction_weight"`
	Temperature         *float64        `json:"temperature"`
	RequestMode         *string         `json:"request_mode"`
	AuthType            *string         `json:"auth_type"`
	Enabled             *bool           `json:"enabled"`
	FallbackEnabled     *bool           `json:"fallback_enabled"`
	PlatformWeights     json.RawMessage `json:"platform_weights"`
	PlatformWeightsJSON *string         `json:"platform_weights_json"`
}

// defaultModelSettings mirrors the built-in fallbacks used when a team has no
// saved configuration yet.
func defaultModelSettings(baseURL, model string) modelSettings {
	return modelSettings{
		Name:               "默认模型",
		BaseURL:            baseURL,
		Model:              model,
		MinSample:          3,
		CompletionWeight:   .6,
		InteractionWeight:  .4,
		Temperature:        .3,
		RequestMode:        "base",
		AuthType:           "bearer",
		Enabled:            true,
		FallbackEnabled:    true,
		PlatformWeightsRaw: "{}",
	}
}

// mergeModelSettings applies a request on top of the stored configuration and
// validates the result. Invalid values are reported per field instead of being
// silently coerced.
func mergeModelSettings(current modelSettings, in modelSettingsRequest) (modelSettings, error) {
	result := current
	if in.Name != nil {
		result.Name = strings.TrimSpace(*in.Name)
	}
	if in.BaseURL != nil {
		result.BaseURL = strings.TrimSpace(*in.BaseURL)
	}
	if in.Model != nil {
		result.Model = strings.TrimSpace(*in.Model)
	}
	if in.MinSample != nil {
		result.MinSample = *in.MinSample
	}
	if in.CompletionWeight != nil {
		result.CompletionWeight = *in.CompletionWeight
	}
	if in.InteractionWeight != nil {
		result.InteractionWeight = *in.InteractionWeight
	}
	if in.Temperature != nil {
		result.Temperature = *in.Temperature
	}
	if in.RequestMode != nil && strings.TrimSpace(*in.RequestMode) != "" {
		result.RequestMode = strings.TrimSpace(*in.RequestMode)
	}
	if in.AuthType != nil && strings.TrimSpace(*in.AuthType) != "" {
		result.AuthType = strings.TrimSpace(*in.AuthType)
	}
	if in.Enabled != nil {
		result.Enabled = *in.Enabled
	}
	if in.FallbackEnabled != nil {
		result.FallbackEnabled = *in.FallbackEnabled
	}
	weights := in.PlatformWeights
	if len(weights) == 0 && in.PlatformWeightsJSON != nil && strings.TrimSpace(*in.PlatformWeightsJSON) != "" {
		weights = json.RawMessage(*in.PlatformWeightsJSON)
	}
	if len(weights) > 0 {
		var probe map[string]any
		if err := json.Unmarshal(weights, &probe); err != nil {
			// An explicitly empty value clears the stored template.
			if strings.TrimSpace(string(weights)) == `""` || strings.TrimSpace(string(weights)) == "null" {
				result.PlatformWeightsRaw = "{}"
			} else {
				return result, errors.New("平台权重模板必须是 JSON 对象")
			}
		} else {
			result.PlatformWeightsRaw = string(weights)
		}
	}
	if result.Name == "" {
		result.Name = "默认模型"
	}
	if result.MinSample < 1 || result.MinSample > 1000 {
		return result, errors.New("样本门槛应为 1 到 1000")
	}
	if result.CompletionWeight < 0 || result.CompletionWeight > 1 || result.InteractionWeight < 0 || result.InteractionWeight > 1 {
		return result, errors.New("评分权重应为 0 到 1 之间")
	}
	if result.Temperature < 0 || result.Temperature > 2 {
		return result, errors.New("temperature 应为 0 到 2 之间")
	}
	if result.BaseURL != "" {
		parsed, err := url.Parse(result.BaseURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return result, errors.New("Base URL 必须是 http(s) 地址")
		}
	}
	if result.PlatformWeightsRaw == "" {
		result.PlatformWeightsRaw = "{}"
	}
	return result, nil
}
