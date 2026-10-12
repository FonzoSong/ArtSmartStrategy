package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrDuplicateEmail = errors.New("email already exists")
	ErrOwnerExists    = errors.New("initial owner already exists")
)

// HasOwner reports whether any owner account exists.
func (r *Postgres) HasOwner(ctx context.Context) (bool, error) {
	var exists bool
	err := r.DB.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM user_account WHERE role_name = 'owner')
	`).Scan(&exists)
	return exists, err
}

// SetupOwner creates or renames the initial team and atomically adds its owner.
func (r *Postgres) SetupOwner(
	ctx context.Context,
	teamName, email, name, passwordHash string,
) (domain.User, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(9812047)`); err != nil {
		return domain.User{}, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM user_account WHERE role_name = 'owner')
	`).Scan(&exists); err != nil {
		return domain.User{}, err
	}
	if exists {
		return domain.User{}, ErrOwnerExists
	}
	var teamID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO team (id, name, created_at) VALUES (1, $1, NOW())
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, strings.TrimSpace(teamName)).Scan(&teamID)
	if err != nil {
		return domain.User{}, err
	}
	user, err := insertUser(ctx, tx, teamID, email, name, domain.RoleOwner, passwordHash)
	if err != nil {
		return domain.User{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_log (team_id, actor, action, entity_type, entity_id, detail, created_at)
		VALUES ($1, $2, 'setup', 'user', $3, '创建团队管理员', NOW())
	`, teamID, user.Name, fmt.Sprint(user.ID)); err != nil {
		return domain.User{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

// RegisterTeam creates a workspace and its first owner in one transaction.
func (r *Postgres) RegisterTeam(
	ctx context.Context,
	teamName, email, name, passwordHash string,
) (domain.User, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback()
	var teamID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO team (name, created_at) VALUES ($1, NOW()) RETURNING id
	`, strings.TrimSpace(teamName)).Scan(&teamID)
	if err != nil {
		return domain.User{}, err
	}
	user, err := insertUser(ctx, tx, teamID, email, name, domain.RoleOwner, passwordHash)
	if err != nil {
		return domain.User{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_log (team_id, actor, action, entity_type, entity_id, detail, created_at)
		VALUES ($1, $2, 'register_team', 'team', $3, '创建新团队', NOW())
	`, teamID, user.Name, fmt.Sprint(teamID)); err != nil {
		return domain.User{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func insertUser(
	ctx context.Context,
	tx *sql.Tx,
	teamID int64,
	email, name, role, passwordHash string,
) (domain.User, error) {
	var user domain.User
	err := tx.QueryRowContext(ctx, `
		INSERT INTO user_account (team_id, email, name, role_name, password_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		RETURNING id, team_id, email, name, role_name, password_hash, created_at
	`, teamID, strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(name), role, passwordHash).Scan(
		&user.ID,
		&user.TeamID,
		&user.Email,
		&user.Name,
		&user.Role,
		&user.PasswordHash,
		&user.CreatedAt,
	)
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return domain.User{}, ErrDuplicateEmail
	}
	return user, err
}

// CreateSession persists a revocable, time-limited login session.
func (r *Postgres) CreateSession(
	ctx context.Context,
	sessionID string,
	userID int64,
	expires time.Time,
) error {
	_, err := r.DB.ExecContext(ctx, `
		INSERT INTO user_session (id, user_id, expires_at, revoked)
		VALUES ($1, $2, $3, FALSE)
	`, sessionID, userID, expires)
	return err
}

// RevokeSession marks a session revoked only for its owning user.
func (r *Postgres) RevokeSession(
	ctx context.Context,
	sessionID string,
	userID int64,
) error {
	result, err := r.DB.ExecContext(ctx, `
		UPDATE user_session SET revoked = TRUE WHERE id = $1 AND user_id = $2
	`, sessionID, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// AddMember creates a team-scoped editor/viewer and records the owner action.
func (r *Postgres) AddMember(
	ctx context.Context,
	teamID int64,
	actor string,
	email, name, role, passwordHash string,
) (domain.User, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback()
	user, err := insertUser(ctx, tx, teamID, email, name, role, passwordHash)
	if err != nil {
		return domain.User{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_log (team_id, actor, action, entity_type, entity_id, detail, created_at)
		VALUES ($1, $2, 'invite', 'user', $3, $4, NOW())
	`, teamID, actor, fmt.Sprint(user.ID), "新增成员 "+user.Email+" / "+user.Role); err != nil {
		return domain.User{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.User{}, err
	}
	return user, nil
}
