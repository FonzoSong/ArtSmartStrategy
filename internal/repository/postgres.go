// Package repository implements PostgreSQL persistence and transactions.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
)

// Postgres owns the shared connection pool used by repository operations.
type Postgres struct {
	DB *sql.DB
}

// New constructs a PostgreSQL repository.
func New(db *sql.DB) *Postgres { return &Postgres{DB: db} }

// Ping checks the managed database with the request context deadline.
func (r *Postgres) Ping(ctx context.Context) error {
	return r.DB.PingContext(ctx)
}

// QueryMaps maps result rows to JSON-friendly maps using database column names.
func (r *Postgres) QueryMaps(
	ctx context.Context,
	query string,
	args ...any,
) ([]map[string]any, error) {
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for index := range values {
			dest[index] = &values[index]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		item := make(map[string]any, len(columns))
		for index, key := range columns {
			item[key] = normalizeValue(values[index])
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case time.Time:
		return typed
	default:
		return value
	}
}

// GetUserByEmail performs a case-insensitive account lookup.
func (r *Postgres) GetUserByEmail(
	ctx context.Context,
	email string,
) (domain.User, error) {
	var user domain.User
	err := r.DB.QueryRowContext(ctx, `
		SELECT id, team_id, email, name, role_name, password_hash, created_at
		FROM user_account WHERE lower(email) = lower($1)
	`, strings.TrimSpace(email)).Scan(
		&user.ID,
		&user.TeamID,
		&user.Email,
		&user.Name,
		&user.Role,
		&user.PasswordHash,
		&user.CreatedAt,
	)
	return user, err
}

// GetUser returns a team account by its primary key.
func (r *Postgres) GetUser(
	ctx context.Context,
	id int64,
) (domain.User, error) {
	var user domain.User
	err := r.DB.QueryRowContext(ctx, `
		SELECT id, team_id, email, name, role_name, password_hash, created_at
		FROM user_account WHERE id = $1
	`, id).Scan(
		&user.ID,
		&user.TeamID,
		&user.Email,
		&user.Name,
		&user.Role,
		&user.PasswordHash,
		&user.CreatedAt,
	)
	return user, err
}

// GetSession verifies that a stored, unrevoked session belongs to the user.
func (r *Postgres) GetSession(
	ctx context.Context,
	sessionID string,
	userID int64,
) (time.Time, bool, error) {
	var expires time.Time
	var revoked bool
	err := r.DB.QueryRowContext(ctx, `
		SELECT expires_at, revoked FROM user_session
		WHERE id = $1 AND user_id = $2
	`, sessionID, userID).Scan(&expires, &revoked)
	return expires, revoked, err
}

// ListContents returns all team content ordered by publication time.
func (r *Postgres) ListContents(
	ctx context.Context,
	teamID int64,
) ([]map[string]any, error) {
	items, err := r.QueryMaps(ctx, `
		SELECT c.*, c.published_at AS date,
		       COALESCE((SELECT json_agg(t.name ORDER BY t.name)
		                 FROM content_tag ct JOIN tag t ON t.id = ct.tag_id
		                 WHERE ct.content_id = c.id),
	                c.tags_json::json) AS tags
		FROM content c
		WHERE c.team_id = $1
		ORDER BY c.published_at
	`, teamID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if raw, ok := item["tags"].(string); ok {
			var tags []string
			if err := json.Unmarshal([]byte(raw), &tags); err == nil {
				item["tags"] = tags
			} else {
				item["tags"] = []string{}
			}
		}
		delete(item, "tags_json")
		item["score"] = nil
	}
	return items, nil
}

// GetContent returns one team-scoped record or sql.ErrNoRows.
func (r *Postgres) GetContent(
	ctx context.Context,
	teamID int64,
	contentID string,
) (map[string]any, error) {
	items, err := r.QueryMaps(ctx, `
		SELECT c.*, c.published_at AS date,
		       COALESCE((SELECT json_agg(t.name ORDER BY t.name)
		                 FROM content_tag ct JOIN tag t ON t.id = ct.tag_id
		                 WHERE ct.content_id = c.id),
	                c.tags_json::json) AS tags
		FROM content c WHERE c.team_id = $1 AND c.id = $2
	`, teamID, contentID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	item := items[0]
	if raw, ok := item["tags"].(string); ok {
		var tags []string
		if err := json.Unmarshal([]byte(raw), &tags); err != nil {
			return nil, fmt.Errorf("decode content tags: %w", err)
		}
		item["tags"] = tags
	}
	delete(item, "tags_json")
	return item, nil
}

// TeamName gets a team name while enforcing the caller's data scope.
func (r *Postgres) TeamName(
	ctx context.Context,
	teamID int64,
) (string, error) {
	var name string
	err := r.DB.QueryRowContext(
		ctx, `SELECT name FROM team WHERE id = $1`, teamID,
	).Scan(&name)
	return name, err
}

// ListTeamRows runs a parameterized query and returns a non-nil empty list.
func (r *Postgres) ListTeamRows(
	ctx context.Context,
	query string,
	teamID int64,
) ([]map[string]any, error) {
	if !strings.Contains(query, "$1") {
		return nil, errors.New("team query must include a parameterized team id")
	}
	rows, err := r.QueryMaps(ctx, query, teamID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []map[string]any{}, nil
	}
	return rows, nil
}

// ListPerformances returns current-team performance bookings with owner labels.
func (r *Postgres) ListPerformances(ctx context.Context, teamID int64) ([]map[string]any, error) {
	return r.ListTeamRows(ctx, `SELECT p.*,u.name AS owner_name FROM performance_booking p LEFT JOIN user_account u ON u.id=p.owner_user_id WHERE p.team_id=$1 ORDER BY p.starts_at`, teamID)
}

// ListTags returns team tags with usage counts and creator display names.
func (r *Postgres) ListTags(ctx context.Context, teamID int64) ([]map[string]any, error) {
	return r.ListTeamRows(ctx, `SELECT t.id,t.name,t.color,t.is_active AS active,t.created_at,u.name AS created_by,COUNT(ct.content_id) AS count FROM tag t LEFT JOIN user_account u ON u.id=t.created_by_user_id LEFT JOIN content_tag ct ON ct.tag_id=t.id WHERE t.team_id=$1 GROUP BY t.id,u.name ORDER BY t.name`, teamID)
}

// CreateTag creates a normalized team-scoped label, returning an existing tag
// when its normalized form already exists.
func (r *Postgres) CreateTag(ctx context.Context, teamID, actorID int64, name string) (map[string]any, error) {
	normalizedName, normalized, err := NormalizeTag(name)
	if err != nil {
		return nil, err
	}
	var id int64
	err = r.DB.QueryRowContext(ctx, `INSERT INTO tag (team_id,name,normalized_name,color,is_active,created_by_user_id,created_at) VALUES ($1,$2,$3,'#647383',TRUE,$4,NOW()) ON CONFLICT (team_id,normalized_name) DO UPDATE SET normalized_name=EXCLUDED.normalized_name RETURNING id`, teamID, normalizedName, normalized, actorID).Scan(&id)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryMaps(ctx, `SELECT id,name,color,is_active AS active,created_at FROM tag WHERE id=$1`, id)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// CreatePerformanceBooking records a team-owned performance and its audit row.
func (r *Postgres) CreatePerformanceBooking(ctx context.Context, teamID, actorID int64, actor string, input domain.Performance) (map[string]any, error) {
	if strings.TrimSpace(input.Title) == "" || input.StartsAt.IsZero() {
		return nil, errors.New("演出名称和开始时间为必填项")
	}
	if input.ID == "" {
		input.ID = newUUID()
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if input.OwnerUserID != nil {
		if err = tx.QueryRowContext(ctx, `SELECT name FROM user_account WHERE id=$1 AND team_id=$2`, *input.OwnerUserID, teamID).Scan(&input.Owner); err != nil {
			return nil, errors.New("负责人不存在或不属于当前团队")
		}
	}
	if input.BookingStatus == "" {
		input.BookingStatus = "已预订"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO performance_booking (id,team_id,title,venue,starts_at,booking_status,owner_user_id,notes,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())`, input.ID, teamID, input.Title, input.Venue, input.StartsAt, input.BookingStatus, input.OwnerUserID, input.Notes)
	if err != nil {
		return nil, err
	}
	if err = insertAudit(ctx, tx, teamID, actor, "create", "performance", input.ID, input.Title); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"id": input.ID, "team_id": teamID, "title": input.Title, "venue": input.Venue, "starts_at": input.StartsAt, "booking_status": input.BookingStatus, "owner_user_id": input.OwnerUserID, "owner": input.Owner, "notes": input.Notes}, nil
}
