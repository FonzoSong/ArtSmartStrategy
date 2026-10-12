package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

// statsFilter is the shared statistics scope for the dashboard and the PDF
// export, so both always report the same numbers for the same filters.
type statsFilter struct {
	RangeLabel string
	From       time.Time
	To         time.Time
	Platform   string
	Category   string
}

// previous returns the equally long window directly before the current one.
func (f statsFilter) previous() (time.Time, time.Time) {
	length := f.To.Sub(f.From)
	return f.From.Add(-length), f.From
}

func (f statsFilter) empty() bool { return f.From.IsZero() || f.To.IsZero() }

// where builds the published-content predicate and its arguments.
func (f statsFilter) where(startIndex int) (string, []any) {
	clauses := []string{}
	args := []any{}
	if !f.empty() {
		clauses = append(clauses, fmt.Sprintf("published_at >= $%d", startIndex+len(args)))
		args = append(args, f.From)
		clauses = append(clauses, fmt.Sprintf("published_at < $%d", startIndex+len(args)))
		args = append(args, f.To)
	}
	if f.Platform != "" {
		clauses = append(clauses, fmt.Sprintf("platform = $%d", startIndex+len(args)))
		args = append(args, f.Platform)
	}
	if f.Category != "" {
		clauses = append(clauses, fmt.Sprintf("category = $%d", startIndex+len(args)))
		args = append(args, f.Category)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

// parseStatsFilter reads the range, platform and category query parameters.
func parseStatsFilter(r *http.Request) (statsFilter, error) {
	query := r.URL.Query()
	filter := statsFilter{
		Platform: strings.TrimSpace(query.Get("platform")),
		Category: strings.TrimSpace(query.Get("category")),
	}
	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	filter.To = startOfToday.AddDate(0, 0, 1)
	switch strings.TrimSpace(query.Get("range")) {
	case "", "30d":
		filter.RangeLabel = "近 30 天"
		filter.From = startOfToday.AddDate(0, 0, -29)
	case "7d":
		filter.RangeLabel = "近 7 天"
		filter.From = startOfToday.AddDate(0, 0, -6)
	case "90d":
		filter.RangeLabel = "近 90 天"
		filter.From = startOfToday.AddDate(0, 0, -89)
	case "all":
		filter.RangeLabel = "全部历史"
	case "custom":
		from, err := time.Parse("2006-01-02", query.Get("from"))
		if err != nil {
			return filter, fmt.Errorf("开始日期格式必须为 YYYY-MM-DD")
		}
		to, err := time.Parse("2006-01-02", query.Get("to"))
		if err != nil {
			return filter, fmt.Errorf("结束日期格式必须为 YYYY-MM-DD")
		}
		if to.Before(from) {
			return filter, fmt.Errorf("结束日期不能早于开始日期")
		}
		filter.RangeLabel = from.Format("2006-01-02") + " 至 " + to.Format("2006-01-02")
		filter.From = from
		filter.To = to.AddDate(0, 0, 1)
	default:
		return filter, fmt.Errorf("不支持的时间范围")
	}
	return filter, nil
}

// metricSet is one set of published-content numbers.
type metricSet struct {
	PublishedCount    int64   `json:"published_count"`
	TotalViews        int64   `json:"total_views"`
	TotalLikes        int64   `json:"total_likes"`
	TotalComments     int64   `json:"total_comments"`
	TotalSaves        int64   `json:"total_saves"`
	TotalInteractions int64   `json:"total_interactions"`
	AvgCompletion     float64 `json:"avg_completion"`
	InteractionRate   float64 `json:"interaction_rate"`
}

// kpiComparison carries the previous-period numbers plus the deltas. Deltas stay
// null when the previous window has no data, so no growth is invented.
type kpiComparison struct {
	PublishedCount    *float64 `json:"published_count_change"`
	TotalViews        *float64 `json:"total_views_change"`
	TotalLikes        *float64 `json:"total_likes_change"`
	TotalComments     *float64 `json:"total_comments_change"`
	TotalSaves        *float64 `json:"total_saves_change"`
	TotalInteractions *float64 `json:"total_interactions_change"`
	AvgCompletion     *float64 `json:"avg_completion_change"`
	InteractionRate   *float64 `json:"interaction_rate_change"`
}

type trendPoint struct {
	Bucket            string  `json:"bucket"`
	PublishedCount    int64   `json:"published_count"`
	TotalViews        int64   `json:"total_views"`
	TotalInteractions int64   `json:"total_interactions"`
	AvgCompletion     float64 `json:"avg_completion"`
}

type platformRow struct {
	Name              string  `json:"name"`
	PublishedCount    int64   `json:"published_count"`
	TotalViews        int64   `json:"total_views"`
	TotalInteractions int64   `json:"total_interactions"`
	InteractionRate   float64 `json:"interaction_rate"`
	AvgCompletion     float64 `json:"avg_completion"`
}

type categoryRow struct {
	Name              string `json:"name"`
	PublishedCount    int64  `json:"published_count"`
	TotalViews        int64  `json:"total_views"`
	TotalInteractions int64  `json:"total_interactions"`
}

type workflowRow struct {
	State string `json:"state"`
	Count int64  `json:"count"`
}

// dashboardResponse is what the operations overview renders and what the PDF
// export prints, guaranteed to share one computation.
type dashboardResponse struct {
	RangeLabel  string        `json:"range_label"`
	Filters     []string      `json:"filters"`
	Current     metricSet     `json:"current"`
	Previous    metricSet     `json:"previous"`
	HasPrevious bool          `json:"has_previous"`
	Changes     kpiComparison `json:"changes"`
	Trend       []trendPoint  `json:"trend"`
	Platforms   []platformRow `json:"platforms"`
	Categories  []categoryRow `json:"categories"`
	Workflow    []workflowRow `json:"workflow"`
	GeneratedAt time.Time     `json:"generated_at"`
}

func (s *Server) buildDashboard(ctx context.Context, teamID int64, filter statsFilter) (dashboardResponse, error) {
	response := dashboardResponse{RangeLabel: filter.RangeLabel, GeneratedAt: time.Now(), Platforms: []platformRow{}, Categories: []categoryRow{}, Trend: []trendPoint{}, Workflow: []workflowRow{}}
	if !filter.empty() {
		response.Filters = append(response.Filters, filter.RangeLabel)
	}
	if filter.Platform != "" {
		response.Filters = append(response.Filters, "平台："+filter.Platform)
	}
	if filter.Category != "" {
		response.Filters = append(response.Filters, "类型："+filter.Category)
	}
	current, err := s.metrics(ctx, teamID, filter, filter.From, filter.To)
	if err != nil {
		return response, err
	}
	response.Current = current
	if !filter.empty() {
		from, to := filter.previous()
		previous, err := s.metrics(ctx, teamID, filter, from, to)
		if err != nil {
			return response, err
		}
		response.Previous = previous
		response.HasPrevious = previous.PublishedCount > 0
		if response.HasPrevious {
			response.Changes = kpiComparison{
				PublishedCount:    changePointer(previous.PublishedCount, current.PublishedCount),
				TotalViews:        changePointer(previous.TotalViews, current.TotalViews),
				TotalLikes:        changePointer(previous.TotalLikes, current.TotalLikes),
				TotalComments:     changePointer(previous.TotalComments, current.TotalComments),
				TotalSaves:        changePointer(previous.TotalSaves, current.TotalSaves),
				TotalInteractions: changePointer(previous.TotalInteractions, current.TotalInteractions),
				AvgCompletion:     changePointerFloat(previous.AvgCompletion, current.AvgCompletion),
				InteractionRate:   changePointerFloat(previous.InteractionRate, current.InteractionRate),
			}
		}
	}
	if response.Trend, err = s.trend(ctx, teamID, filter); err != nil {
		return response, err
	}
	if response.Platforms, err = s.platformBreakdown(ctx, teamID, filter); err != nil {
		return response, err
	}
	if response.Categories, err = s.categoryBreakdown(ctx, teamID, filter); err != nil {
		return response, err
	}
	if response.Workflow, err = s.workflowBreakdown(ctx, teamID, filter); err != nil {
		return response, err
	}
	return response, nil
}

// metrics aggregates one window of published content.
func (s *Server) metrics(ctx context.Context, teamID int64, filter statsFilter, from, to time.Time) (metricSet, error) {
	window := filter
	window.From, window.To = from, to
	where, args := window.where(2)
	query := `SELECT COUNT(*) AS published_count, COALESCE(SUM(views),0) AS total_views, COALESCE(SUM(likes),0) AS total_likes,
		COALESCE(SUM(comments),0) AS total_comments, COALESCE(SUM(saves),0) AS total_saves,
		COALESCE(SUM(total_interactions),0) AS total_interactions, COALESCE(AVG(completion),0) AS avg_completion,
		CASE WHEN COALESCE(SUM(views),0)=0 THEN 0 ELSE 100.0*COALESCE(SUM(total_interactions),0)/SUM(views) END AS interaction_rate
		FROM content WHERE team_id=$1 AND lifecycle='已发布'` + where
	rows, err := s.Store.QueryMaps(ctx, query, append([]any{teamID}, args...)...)
	if err != nil {
		return metricSet{}, err
	}
	if len(rows) == 0 {
		return metricSet{}, nil
	}
	row := rows[0]
	return metricSet{
		PublishedCount:    intField(row, "published_count"),
		TotalViews:        intField(row, "total_views"),
		TotalLikes:        intField(row, "total_likes"),
		TotalComments:     intField(row, "total_comments"),
		TotalSaves:        intField(row, "total_saves"),
		TotalInteractions: intField(row, "total_interactions"),
		AvgCompletion:     roundTo(floatField(row, "avg_completion"), 2),
		InteractionRate:   roundTo(floatField(row, "interaction_rate"), 2),
	}, nil
}

// trend groups the window by day (short ranges) or by month.
func (s *Server) trend(ctx context.Context, teamID int64, filter statsFilter) ([]trendPoint, error) {
	bucket := "date_trunc('month', published_at)"
	if filter.empty() || filter.To.Sub(filter.From) <= 31*24*time.Hour {
		bucket = "date_trunc('day', published_at)"
	}
	where, args := filter.where(2)
	query := `SELECT to_char(` + bucket + `,'YYYY-MM-DD') AS bucket, COUNT(*) AS published_count,
		COALESCE(SUM(views),0) AS total_views, COALESCE(SUM(total_interactions),0) AS total_interactions,
		COALESCE(AVG(completion),0) AS avg_completion
		FROM content WHERE team_id=$1 AND lifecycle='已发布'` + where + ` GROUP BY 1 ORDER BY 1`
	rows, err := s.Store.QueryMaps(ctx, query, append([]any{teamID}, args...)...)
	if err != nil {
		return nil, err
	}
	points := make([]trendPoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, trendPoint{
			Bucket:            stringField(row, "bucket"),
			PublishedCount:    intField(row, "published_count"),
			TotalViews:        intField(row, "total_views"),
			TotalInteractions: intField(row, "total_interactions"),
			AvgCompletion:     roundTo(floatField(row, "avg_completion"), 2),
		})
	}
	return points, nil
}

func (s *Server) platformBreakdown(ctx context.Context, teamID int64, filter statsFilter) ([]platformRow, error) {
	where, args := filter.where(2)
	query := `SELECT platform AS name, COUNT(*) AS published_count, COALESCE(SUM(views),0) AS total_views,
		COALESCE(SUM(total_interactions),0) AS total_interactions, COALESCE(AVG(completion),0) AS avg_completion,
		CASE WHEN COALESCE(SUM(views),0)=0 THEN 0 ELSE 100.0*COALESCE(SUM(total_interactions),0)/SUM(views) END AS interaction_rate
		FROM content WHERE team_id=$1 AND lifecycle='已发布'` + where + ` GROUP BY platform ORDER BY SUM(views) DESC NULLS LAST, platform`
	rows, err := s.Store.QueryMaps(ctx, query, append([]any{teamID}, args...)...)
	if err != nil {
		return nil, err
	}
	result := make([]platformRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, platformRow{
			Name:              stringField(row, "name"),
			PublishedCount:    intField(row, "published_count"),
			TotalViews:        intField(row, "total_views"),
			TotalInteractions: intField(row, "total_interactions"),
			InteractionRate:   roundTo(floatField(row, "interaction_rate"), 2),
			AvgCompletion:     roundTo(floatField(row, "avg_completion"), 2),
		})
	}
	return result, nil
}

func (s *Server) categoryBreakdown(ctx context.Context, teamID int64, filter statsFilter) ([]categoryRow, error) {
	where, args := filter.where(2)
	query := `SELECT category AS name, COUNT(*) AS published_count, COALESCE(SUM(views),0) AS total_views,
		COALESCE(SUM(total_interactions),0) AS total_interactions
		FROM content WHERE team_id=$1 AND lifecycle='已发布'` + where + ` GROUP BY category ORDER BY SUM(views) DESC NULLS LAST, category`
	rows, err := s.Store.QueryMaps(ctx, query, append([]any{teamID}, args...)...)
	if err != nil {
		return nil, err
	}
	result := make([]categoryRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, categoryRow{
			Name:              stringField(row, "name"),
			PublishedCount:    intField(row, "published_count"),
			TotalViews:        intField(row, "total_views"),
			TotalInteractions: intField(row, "total_interactions"),
		})
	}
	return result, nil
}

// workflowBreakdown counts the current backlog per category. 未发布 is derived
// from the publication outcome, exactly like the records page category.
func (s *Server) workflowBreakdown(ctx context.Context, teamID int64, filter statsFilter) ([]workflowRow, error) {
	clauses := []string{"team_id=$1"}
	args := []any{teamID}
	if filter.Platform != "" {
		clauses = append(clauses, fmt.Sprintf("platform=$%d", len(args)+1))
		args = append(args, filter.Platform)
	}
	if filter.Category != "" {
		clauses = append(clauses, fmt.Sprintf("category=$%d", len(args)+1))
		args = append(args, filter.Category)
	}
	where := " WHERE " + strings.Join(clauses, " AND ")
	query := `SELECT workflow_state AS state, COUNT(*) AS count FROM content` + where + ` GROUP BY workflow_state`
	rows, err := s.Store.QueryMaps(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, row := range rows {
		counts[stringField(row, "state")] = intField(row, "count")
	}
	lifecycleQuery := `SELECT lifecycle AS lifecycle, COUNT(*) AS count FROM content` + where + ` GROUP BY lifecycle`
	lifecycleRows, err := s.Store.QueryMaps(ctx, lifecycleQuery, args...)
	if err != nil {
		return nil, err
	}
	lifecycleCounts := map[string]int64{}
	for _, row := range lifecycleRows {
		lifecycleCounts[stringField(row, "lifecycle")] = intField(row, "count")
	}
	result := make([]workflowRow, 0, len(domain.CategoryOrder())-1)
	for _, category := range domain.CategoryOrder() {
		if category == domain.CategoryAll {
			continue
		}
		count := counts[string(category)]
		if category == domain.CategoryUnpublished || category == domain.CategoryPublished {
			count = lifecycleCounts[string(category)]
		}
		result = append(result, workflowRow{State: string(category), Count: count})
	}
	return result, nil
}

func changePointer(from, to int64) *float64 {
	if from == 0 {
		return nil
	}
	value := roundTo(100*(float64(to)-float64(from))/float64(from), 2)
	return &value
}

func changePointerFloat(from, to float64) *float64 {
	if from == 0 {
		return nil
	}
	value := roundTo(100*(to-from)/from, 2)
	return &value
}

// dashboardStats serves the filtered statistics used by the overview page.
func (s *Server) dashboardStats(w http.ResponseWriter, r *http.Request) {
	filter, err := parseStatsFilter(r)
	if err != nil {
		writeError(w, r, 422, "stats_filter_invalid", err.Error())
		return
	}
	response, err := s.buildDashboard(r.Context(), claimsFrom(r).TeamID, filter)
	if err != nil {
		writeError(w, r, 500, "stats_failed", "统计数据加载失败")
		return
	}
	writeJSON(w, 200, response)
}
