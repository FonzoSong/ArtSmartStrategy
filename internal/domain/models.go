// Package domain defines transport-neutral business entities and constraints.
package domain

import "time"

const (
	RoleOwner  = "owner"
	RoleEditor = "editor"
	RoleViewer = "viewer"

	LifecyclePublished   = "已发布"
	LifecycleUnpublished = "未发布"

	WorkflowProductionPending = "待制作"
	WorkflowProductionDone    = "已制作"
	WorkflowSchedulePending   = "待排期"
	WorkflowScheduled         = "已排期"
	WorkflowPublished         = "已发布"

	// PerformanceStatus* are the canonical 正式演出 states. The 演出时间 expiry
	// rule (a show whose start time has passed becomes 已演出) is enforced by the
	// service layer; 已取消 is terminal and is never rewritten automatically.
	PerformancePreparing = "筹备中"
	PerformanceWarmingUp = "预热中"
	PerformancePlayed    = "已演出"
	PerformanceCanceled  = "已取消"
)

// PerformanceStatuses lists the canonical 正式演出 states in display order.
func PerformanceStatuses() []string {
	return []string{PerformancePreparing, PerformanceWarmingUp, PerformancePlayed, PerformanceCanceled}
}

// IsPerformanceStatus reports whether value is a canonical 正式演出 state.
func IsPerformanceStatus(value string) bool {
	for _, status := range PerformanceStatuses() {
		if status == value {
			return true
		}
	}
	return false
}

// Team is an isolated organization workspace.
type Team struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// User is an authenticated member of one team.
type User struct {
	ID           int64     `json:"id"`
	TeamID       int64     `json:"team_id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// Content is a content asset with explicit lifecycle and workflow state.
type Content struct {
	ID                string    `json:"id"`
	TeamID            int64     `json:"team_id"`
	Title             string    `json:"title"`
	Category          string    `json:"category"`
	Platform          string    `json:"platform"`
	PublishedAt       time.Time `json:"published_at"`
	Duration          int       `json:"duration"`
	Views             int64     `json:"views"`
	Likes             int64     `json:"likes"`
	Comments          int64     `json:"comments"`
	Saves             int64     `json:"saves"`
	Shares            int64     `json:"shares"`
	Completion        float64   `json:"completion"`
	TotalInteractions int64     `json:"total_interactions"`
	Tags              []string  `json:"tags"`
	Source            string    `json:"source"`
	Lifecycle         string    `json:"lifecycle"`
	Materials         string    `json:"materials"`
	CoverURL          string    `json:"cover_url"`
	Owner             string    `json:"owner"`
	Notes             string    `json:"notes"`
	ProfileVisits     int64     `json:"profile_visits"`
	Follows           int64     `json:"follows"`
	Attendance        int64     `json:"attendance"`
	Tickets           int64     `json:"tickets"`
	CommentText       string    `json:"comment_text"`
	Adopted           bool      `json:"adopted"`
	RecommendationID  *string   `json:"recommendation_id,omitempty"`
	PerformanceID     *string   `json:"performance_id,omitempty"`
	OwnerUserID       *int64    `json:"owner_user_id,omitempty"`
	WorkflowState     string    `json:"workflow_state"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	EngagementRate    float64   `json:"engagement_rate"`
}

// Performance is one 正式演出: the booking record that owns the promotional
// videos of a single show. Promotional content links to it through
// content.performance_id, so a show is the grouping container rather than a
// parallel content model.
type Performance struct {
	ID              string    `json:"id"`
	TeamID          int64     `json:"team_id"`
	Title           string    `json:"title"`
	Venue           string    `json:"venue"`
	StartsAt        time.Time `json:"starts_at"`
	BookingStatus   string    `json:"booking_status"`
	PerformanceType string    `json:"performance_type"`
	TicketLink      string    `json:"ticket_link"`
	OwnerUserID     *int64    `json:"owner_user_id,omitempty"`
	Owner           string    `json:"owner"`
	Notes           string    `json:"notes"`
	CreatedAt       time.Time `json:"created_at"`
}

// Tag is a searchable team tag; inactive tags remain attached to old content.
type Tag struct {
	ID        int64     `json:"id"`
	TeamID    int64     `json:"team_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Active    bool      `json:"active"`
	Count     int64     `json:"count"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// Schedule is a planned publication with source and actual result metrics.
type Schedule struct {
	ID                  string    `json:"id"`
	TeamID              int64     `json:"team_id"`
	ContentID           *string   `json:"content_id,omitempty"`
	RecommendationID    *string   `json:"recommendation_id,omitempty"`
	PlannedDate         string    `json:"planned_date"`
	PlannedTime         string    `json:"planned_time"`
	Title               string    `json:"title"`
	Category            string    `json:"category"`
	Platform            string    `json:"platform"`
	Owner               string    `json:"owner"`
	Reason              string    `json:"reason"`
	Status              string    `json:"status"`
	Source              string    `json:"source"`
	SourceLabel         string    `json:"source_label"`
	OperatorID          *int64    `json:"operator_id,omitempty"`
	Operator            string    `json:"operator"`
	PredictionViewsLow  *int64    `json:"prediction_views_low,omitempty"`
	PredictionViewsHigh *int64    `json:"prediction_views_high,omitempty"`
	ActualViews         *int64    `json:"actual_views,omitempty"`
	ActualLikes         *int64    `json:"actual_likes,omitempty"`
	ActualComments      *int64    `json:"actual_comments,omitempty"`
	ActualSaves         *int64    `json:"actual_saves,omitempty"`
	ActualShares        *int64    `json:"actual_shares,omitempty"`
	ActualCompletion    *float64  `json:"actual_completion,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// Claims holds an authenticated account identity from a signed session.
type Claims struct {
	UserID    int64  `json:"user_id"`
	TeamID    int64  `json:"team_id"`
	Role      string `json:"role"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	SessionID string `json:"session_id"`
}

// APIError is the stable error shape shared by the TypeScript client.
type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}
