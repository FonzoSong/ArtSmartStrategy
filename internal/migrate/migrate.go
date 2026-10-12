// Package migrate applies versioned PostgreSQL schema migrations.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

// CurrentAlembicHead is the last schema version supported by the legacy app.
const CurrentAlembicHead = "0006_workflow_rules_tags"

// Runner serializes migration application across service instances.
type Runner struct {
	Logger *slog.Logger
}

type migration struct {
	version string
	sql     string
}

// Apply installs the schema, or adopts a verified current Alembic schema.
func (r Runner) Apply(ctx context.Context, db *sql.DB) error {
	logger := r.Logger
	if logger == nil {
		logger = slog.Default()
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock(781239042)`); err != nil {
		return fmt.Errorf("lock schema migration: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(781239042)`)
	}()
	if _, err = conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(160) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create schema migration metadata: %w", err)
	}
	if err := validateExistingSchema(ctx, conn); err != nil {
		return err
	}
	files, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, item := range files {
		var applied bool
		err := conn.QueryRowContext(
			ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`,
			item.version,
		).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", item.version, err)
		}
		if applied {
			continue
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", item.version, err)
		}
		for _, statement := range splitStatements(item.sql) {
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				break
			}
		}
		if err == nil {
			_, err = tx.ExecContext(
				ctx,
				`INSERT INTO schema_migrations (version) VALUES ($1)`,
				item.version,
			)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", item.version, err)
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", item.version, err)
		}
		logger.Info("database migration applied", "version", item.version)
	}
	return nil
}

func splitStatements(source string) []string {
	statements := []string{}
	var current strings.Builder
	inSingle, inDouble, inLineComment := false, false, false
	for i := 0; i < len(source); i++ {
		char := source[i]
		if inLineComment {
			if char == '\n' {
				inLineComment = false
				current.WriteByte(char)
			}
			continue
		}
		if !inSingle && !inDouble && char == '-' && i+1 < len(source) && source[i+1] == '-' {
			inLineComment = true
			i++
			continue
		}
		if char == '\'' && !inDouble {
			current.WriteByte(char)
			if inSingle && i+1 < len(source) && source[i+1] == '\'' {
				current.WriteByte(source[i+1])
				i++
				continue
			}
			inSingle = !inSingle
			continue
		}
		if char == '"' && !inSingle {
			current.WriteByte(char)
			if inDouble && i+1 < len(source) && source[i+1] == '"' {
				current.WriteByte(source[i+1])
				i++
				continue
			}
			inDouble = !inDouble
			continue
		}
		if char == ';' && !inSingle && !inDouble {
			if statement := strings.TrimSpace(current.String()); statement != "" {
				statements = append(statements, statement)
			}
			current.Reset()
			continue
		}
		current.WriteByte(char)
	}
	if statement := strings.TrimSpace(current.String()); statement != "" {
		statements = append(statements, statement)
	}
	return statements
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.Glob(migrationFiles, "sql/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("no embedded database migrations found")
	}
	sort.Strings(entries)
	result := make([]migration, 0, len(entries))
	for _, name := range entries {
		contents, err := migrationFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		base := name[strings.LastIndex(name, "/")+1:]
		version := strings.TrimSuffix(base, ".sql")
		result = append(result, migration{version: version, sql: string(contents)})
	}
	return result, nil
}

func validateExistingSchema(ctx context.Context, conn *sql.Conn) error {
	var existing bool
	if err := conn.QueryRowContext(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations)`,
	).Scan(&existing); err != nil {
		return fmt.Errorf("inspect Go migration history: %w", err)
	}
	if existing {
		return nil
	}
	existing = false
	err := conn.QueryRowContext(
		ctx,
		`SELECT to_regclass('public.content') IS NOT NULL`,
	).Scan(&existing)
	if err != nil || !existing {
		return err
	}
	var alembicExists bool
	if err = conn.QueryRowContext(
		ctx,
		`SELECT to_regclass('public.alembic_version') IS NOT NULL`,
	).Scan(&alembicExists); err != nil {
		return fmt.Errorf("inspect existing schema: %w", err)
	}
	if !alembicExists {
		return errors.New(
			"untracked PostgreSQL schema detected; export and verify it before Go migration",
		)
	}
	var head string
	if err = conn.QueryRowContext(
		ctx, `SELECT version_num FROM alembic_version LIMIT 1`,
	).Scan(&head); err != nil {
		return fmt.Errorf("read existing Alembic version: %w", err)
	}
	if head != CurrentAlembicHead {
		return fmt.Errorf(
			"existing PostgreSQL schema is at Alembic %q; Go service expects %q; upgrade and verify before cutover",
			head,
			CurrentAlembicHead,
		)
	}
	return nil
}
