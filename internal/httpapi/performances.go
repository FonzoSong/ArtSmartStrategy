package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/service"
)

// loadExistingScheduleLoad fills the planning policy with the team's current
// pending plan load, so generation and adoption share one frequency counter.
func (s *Server) loadExistingScheduleLoad(ctx context.Context, teamID int64, policy *service.SlotPolicy) error {
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT planned_date,platform FROM schedule WHERE team_id=$1 AND status='待发布'`, teamID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var planned time.Time
		var platform string
		if err = rows.Scan(&planned, &platform); err != nil {
			return err
		}
		date := planned.Format("2006-01-02")
		year, week := planned.ISOWeek()
		policy.ExistingDaily[date]++
		policy.ExistingPlatformWeekly[platform+":"+strconv.Itoa(year)+":"+strconv.Itoa(week)]++
	}
	return rows.Err()
}

// loadPublishedSamples returns only the caller's own published records, which is
// the sole historical input the planner is allowed to use.
func (s *Server) loadPublishedSamples(ctx context.Context, teamID int64) ([]service.PerformanceSample, error) {
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT category,platform,published_at,views,likes,comments,saves,shares,completion FROM content WHERE team_id=$1 AND lifecycle='已发布' AND workflow_state='已发布' ORDER BY published_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	samples := []service.PerformanceSample{}
	for rows.Next() {
		var item service.PerformanceSample
		if err = rows.Scan(&item.Category, &item.Platform, &item.PublishedAt, &item.Views, &item.Likes, &item.Comments, &item.Saves, &item.Shares, &item.Completion); err != nil {
			return nil, err
		}
		samples = append(samples, item)
	}
	return samples, rows.Err()
}

// performanceComparisonRow is one show's aggregated pre-heat effect.
type performanceComparisonRow struct {
	ID                string  `json:"id"`
	Title             string  `json:"title"`
	Venue             string  `json:"venue"`
	BookingStatus     string  `json:"booking_status"`
	PerformanceType   string  `json:"performance_type"`
	StartsAt          string  `json:"starts_at"`
	PublishedCount    int64   `json:"published_count"`
	UnpublishedCount  int64   `json:"unpublished_count"`
	TotalViews        int64   `json:"total_views"`
	TotalInteractions int64   `json:"total_interactions"`
	AvgCompletion     float64 `json:"avg_completion"`
	InteractionRate   float64 `json:"interaction_rate"`
	SampleSufficient  bool    `json:"sample_sufficient"`
}

// performanceComparison aggregates published effect data per 正式演出 so different
// shows can be compared using the same fields the analysis page already uses.
// Shows without published videos are reported with zero samples instead of being
// dropped, and the team baseline travels with the response for context.
func (s *Server) performanceComparison(w http.ResponseWriter, r *http.Request) {
	teamID := claimsFrom(r).TeamID
	minSample := 3
	_ = s.Store.DB.QueryRowContext(r.Context(), `SELECT min_sample FROM ai_config WHERE team_id=$1 ORDER BY updated_at DESC LIMIT 1`, teamID).Scan(&minSample)
	rows, err := s.Store.QueryMaps(r.Context(), `
		SELECT p.id, p.title, p.venue, p.starts_at, p.booking_status, p.performance_type,
		       COUNT(c.id) FILTER (WHERE c.lifecycle='已发布') AS published_count,
		       COUNT(c.id) FILTER (WHERE c.lifecycle='未发布') AS unpublished_count,
		       COALESCE(SUM(c.views) FILTER (WHERE c.lifecycle='已发布'),0) AS total_views,
		       COALESCE(SUM(c.total_interactions) FILTER (WHERE c.lifecycle='已发布'),0) AS total_interactions,
		       COALESCE(AVG(c.completion) FILTER (WHERE c.lifecycle='已发布'),0) AS avg_completion
		FROM performance_booking p
		LEFT JOIN content c ON c.performance_id=p.id AND c.team_id=p.team_id
		WHERE p.team_id=$1
		GROUP BY p.id
		ORDER BY p.starts_at DESC, p.title`, teamID)
	if err != nil {
		writeError(w, r, 500, "stats_failed", "统计演出预热效果失败")
		return
	}
	items := make([]performanceComparisonRow, 0, len(rows))
	for _, row := range rows {
		views := intField(row, "total_views")
		interactions := intField(row, "total_interactions")
		rate := 0.0
		if views > 0 {
			rate = 100 * float64(interactions) / float64(views)
		}
		published := intField(row, "published_count")
		items = append(items, performanceComparisonRow{
			ID: stringField(row, "id"), Title: stringField(row, "title"), Venue: stringField(row, "venue"),
			BookingStatus: stringField(row, "booking_status"), PerformanceType: stringField(row, "performance_type"),
			StartsAt:       timestampField(row, "starts_at"),
			PublishedCount: published, UnpublishedCount: intField(row, "unpublished_count"),
			TotalViews: views, TotalInteractions: interactions,
			AvgCompletion:   roundTo(floatField(row, "avg_completion"), 2),
			InteractionRate: roundTo(rate, 2), SampleSufficient: published >= int64(minSample),
		})
	}
	baseline, err := s.Store.QueryMaps(r.Context(), `
		SELECT COUNT(*) AS sample_count,
		       COALESCE(AVG(completion),0) AS avg_completion,
		       COALESCE(SUM(total_interactions),0) AS total_interactions,
		       COALESCE(SUM(views),0) AS total_views
		FROM content WHERE team_id=$1 AND lifecycle='已发布'`, teamID)
	if err != nil {
		writeError(w, r, 500, "stats_failed", "统计团队基准失败")
		return
	}
	baselineRow := map[string]any{}
	if len(baseline) > 0 {
		baselineRow = baseline[0]
	}
	baseViews, baseInteractions := intField(baselineRow, "total_views"), intField(baselineRow, "total_interactions")
	baseRate := 0.0
	if baseViews > 0 {
		baseRate = 100 * float64(baseInteractions) / float64(baseViews)
	}
	writeJSON(w, 200, map[string]any{
		"items":      items,
		"min_sample": minSample,
		"baseline": map[string]any{
			"sample_count":     intField(baselineRow, "sample_count"),
			"avg_completion":   roundTo(floatField(baselineRow, "avg_completion"), 2),
			"interaction_rate": roundTo(baseRate, 2),
		},
	})
}

// scheduleAdjustments lists the recorded manual changes of one plan.
func (s *Server) scheduleAdjustments(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ScheduleAdjustments(r.Context(), claimsFrom(r).TeamID, r.PathValue("id"))
	if err != nil {
		writeError(w, r, 500, "schedule_adjustments_failed", "读取排期调整记录失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}

// describeScheduleRow renders a plan as the text stored in the adjustment log, so
// a later reader can tell what the schedule looked like before and after an edit.
// Empty parts are dropped instead of leaving dangling separators.
func describeScheduleRow(row map[string]any) string {
	parts := []string{
		strings.TrimSpace(stringField(row, "planned_date") + " " + stringField(row, "planned_time")),
		stringField(row, "platform"), stringField(row, "title"),
	}
	if owner := stringField(row, "owner"); owner != "" {
		parts = append(parts, "负责人 "+owner)
	}
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}
