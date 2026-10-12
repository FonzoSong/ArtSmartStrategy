// SPDX-License-Identifier: Apache-2.0
// Command migrate-sqlite verifies and optionally imports the legacy SQLite DB.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	schemamigrate "github.com/arts-smart/strategy/internal/migrate"
	"github.com/arts-smart/strategy/internal/service"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	_ "modernc.org/sqlite"
)

var tableOrder = []string{
	"team", "user_account", "user_session", "ai_config", "benchmark",
	"tag", "performance_booking", "recommendation", "content",
	"content_tag", "schedule", "audit_log", "ai_call_log", "content_rule",
	"rule_execution_log", "hypothesis", "content_workflow_event",
}

func main() {
	source := flag.String("source", "data/arts_smart.db", "legacy SQLite database path")
	apply := flag.Bool("apply", false, "apply import after verification; default is dry run")
	flag.Parse()
	if err := migrateSQLite(*source, *apply); err != nil {
		log.Fatal(err)
	}
}

func migrateSQLite(source string, apply bool) error {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		return errors.New("DATABASE_URL must point to the target PostgreSQL service")
	}
	appSecret := os.Getenv("APP_SECRET")
	if apply && len(appSecret) < 32 {
		return errors.New("APP_SECRET (32+ characters) is required to safely migrate stored model credentials")
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	if _, err = os.Stat(absolute); err != nil {
		return fmt.Errorf("source SQLite file unavailable: %w", err)
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String() + "?mode=ro"
	sqliteDB, err := sql.Open("sqlite", uri)
	if err != nil {
		return err
	}
	defer sqliteDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err = sqliteDB.PingContext(ctx); err != nil {
		return fmt.Errorf("read legacy SQLite database: %w", err)
	}
	pg, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer pg.Close()
	if err = pg.PingContext(ctx); err != nil {
		return fmt.Errorf("connect target PostgreSQL: %w", err)
	}
	if apply {
		if err = (schemamigrate.Runner{}).Apply(ctx, pg); err != nil {
			return fmt.Errorf("prepare PostgreSQL schema: %w", err)
		}
	} else {
		var schemaReady bool
		if err = pg.QueryRowContext(ctx, `SELECT to_regclass('public.content') IS NOT NULL`).Scan(&schemaReady); err != nil {
			return err
		}
		if !schemaReady {
			return errors.New("target schema is not installed; run the Go API migrations first, then rerun dry-run")
		}
	}
	if err = verifyEmptyTarget(ctx, pg); err != nil {
		return err
	}
	tables, err := sourceTables(ctx, sqliteDB)
	if err != nil {
		return err
	}
	counts := make(map[string]int64)
	for _, table := range tableOrder {
		if !tables[table] {
			continue
		}
		var count int64
		query := "SELECT count(*) FROM " + quoteSQLite(table)
		if err = sqliteDB.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return err
		}
		counts[table] = count
		fmt.Printf("%s: %d rows\n", table, count)
	}
	if len(counts) == 0 {
		return errors.New("no recognized legacy application tables found")
	}
	if !apply {
		fmt.Println("Dry run only. Review counts, then rerun with --apply.")
		return nil
	}
	tx, err := pg.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range tableOrder {
		if !tables[table] {
			continue
		}
		if err = copyTable(ctx, sqliteDB, tx, table, counts[table], appSecret); err != nil {
			return fmt.Errorf("import %s: %w", table, err)
		}
	}
	if tables["content"] {
		_, err = tx.ExecContext(ctx, `UPDATE content AS c SET owner_user_id=(SELECT MIN(u.id) FROM user_account AS u WHERE u.team_id=c.team_id AND u.name=c.owner) WHERE c.owner_user_id IS NULL AND (SELECT COUNT(*) FROM user_account AS u WHERE u.team_id=c.team_id AND u.name=c.owner)=1`)
		if err != nil {
			return fmt.Errorf("link legacy content owners: %w", err)
		}
	}
	if err = resetSequences(ctx, tx); err != nil {
		return err
	}
	if err = verifyCounts(ctx, tx, counts); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	fmt.Println("Import committed. Source SQLite file was left unchanged.")
	return nil
}

func verifyEmptyTarget(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			return err
		}
		if table == "schema_migrations" || table == "system_rule_set" || table == "alembic_version" {
			continue
		}
		var count int64
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM \""+table+"\"").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("target table %s contains %d rows; import requires an empty target", table, count)
		}
	}
	return rows.Err()
}

func sourceTables(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		result[name] = true
	}
	return result, rows.Err()
}

func copyTable(ctx context.Context, source *sql.DB, tx *sql.Tx, table string, expected int64, appSecret string) error {
	rows, err := source.QueryContext(ctx, "SELECT * FROM "+quoteSQLite(table))
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	pgColumns, err := destinationTypes(ctx, tx, table)
	if err != nil {
		return err
	}
	sourceSet := make(map[string]bool, len(columns))
	insertColumns := append([]string(nil), columns...)
	for _, name := range columns {
		sourceSet[name] = true
	}
	for name, info := range pgColumns {
		if sourceSet[name] {
			continue
		}
		if !info.nullable && !info.hasDefault {
			insertColumns = append(insertColumns, name)
		}
	}
	quoted := make([]string, len(insertColumns))
	placeholders := make([]string, len(insertColumns))
	for i, name := range insertColumns {
		quoted[i] = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	insert := "INSERT INTO \"" + table + "\" (" + strings.Join(quoted, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ")"
	var copied int64
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err = rows.Scan(destinations...); err != nil {
			return err
		}
		row := make(map[string]any, len(columns)+4)
		for i, name := range columns {
			row[name] = convert(values[i], pgColumns[name].dataType)
		}
		if table == "tag" {
			name, _ := row["name"].(string)
			row["normalized_name"] = cases.Fold().String(norm.NFKC.String(strings.TrimSpace(name)))
			row["is_active"] = true
			row["created_by_user_id"] = nil
			row["created_at"] = time.Now().UTC()
		}
		if table == "content" {
			row["cover_url"] = ""
			row["performance_id"] = nil
			row["owner_user_id"] = nil
			if row["lifecycle"] == "已发布" {
				row["workflow_state"] = "已发布"
			} else {
				row["workflow_state"] = "未发布"
			}
		}
		if table == "schedule" {
			row["source"] = "manual"
			row["created_by_user_id"] = nil
			row["updated_at"] = row["created_at"]
		}
		if table == "ai_config" {
			if raw, ok := row["api_key"].(string); ok && raw != "" && !strings.HasPrefix(raw, "enc:v1:") {
				sealed, sealErr := service.SealSecret(appSecret, raw)
				if sealErr != nil {
					return fmt.Errorf("encrypt legacy model credential: %w", sealErr)
				}
				row["api_key"] = sealed
			}
		}
		out := make([]any, len(insertColumns))
		for i, name := range insertColumns {
			value, exists := row[name]
			if !exists {
				value = migrationDefault(table, name, row)
			}
			out[i] = convert(value, pgColumns[name].dataType)
		}
		if _, err = tx.ExecContext(ctx, insert, out...); err != nil {
			return err
		}
		copied++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if copied != expected {
		return fmt.Errorf("source count changed during read: expected %d, read %d", expected, copied)
	}
	return nil
}

type pgColumn struct {
	dataType   string
	nullable   bool
	hasDefault bool
}

func destinationTypes(ctx context.Context, tx *sql.Tx, table string) (map[string]pgColumn, error) {
	rows, err := tx.QueryContext(ctx, `SELECT column_name,data_type,is_nullable,column_default FROM information_schema.columns WHERE table_schema='public' AND table_name=$1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]pgColumn)
	for rows.Next() {
		var name, kind, nullable string
		var defaultValue sql.NullString
		if err = rows.Scan(&name, &kind, &nullable, &defaultValue); err != nil {
			return nil, err
		}
		result[name] = pgColumn{dataType: kind, nullable: nullable == "YES", hasDefault: defaultValue.Valid}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("target table %s does not exist", table)
	}
	return result, rows.Err()
}

func migrationDefault(table, name string, row map[string]any) any {
	switch table + "." + name {
	case "tag.normalized_name":
		return cases.Fold().String(norm.NFKC.String(strings.TrimSpace(fmt.Sprint(row["name"]))))
	case "tag.is_active":
		return true
	case "tag.created_at":
		return time.Now().UTC()
	case "content.cover_url":
		return ""
	case "content.performance_id", "content.owner_user_id":
		return nil
	case "content.workflow_state":
		if row["lifecycle"] == "已发布" {
			return "已发布"
		}
		return "未发布"
	case "schedule.source":
		return "manual"
	case "schedule.created_by_user_id":
		return nil
	case "schedule.updated_at":
		return row["created_at"]
	}
	return nil
}

func convert(value any, kind string) any {
	switch value := value.(type) {
	case int64:
		if kind == "boolean" {
			return value != 0
		}
	case []byte:
		if kind == "boolean" {
			return string(value) == "1" || strings.EqualFold(string(value), "true")
		}
		return string(value)
	case string:
		if kind == "timestamp without time zone" || kind == "timestamp with time zone" {
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999", "2006-01-02 15:04:05", "2006-01-02"} {
				if parsed, err := time.Parse(layout, value); err == nil {
					return parsed
				}
			}
		}
	}
	return value
}

func resetSequences(ctx context.Context, tx *sql.Tx) error {
	tables := []string{"team", "user_account", "ai_config", "audit_log", "benchmark", "tag", "ai_call_log", "content_rule", "rule_execution_log", "content_workflow_event"}
	sort.Strings(tables)
	for _, table := range tables {
		_, err := tx.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence($1,'id'), COALESCE((SELECT MAX(id) FROM `+`"`+table+`"`+`),1), EXISTS(SELECT 1 FROM `+`"`+table+`"`+`))`, table)
		if err != nil {
			return fmt.Errorf("reset %s sequence: %w", table, err)
		}
	}
	return nil
}

func verifyCounts(ctx context.Context, tx *sql.Tx, counts map[string]int64) error {
	for table, expected := range counts {
		var actual int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM \""+table+"\"").Scan(&actual); err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("count mismatch in %s: source=%d target=%d", table, expected, actual)
		}
	}
	return nil
}

func quoteSQLite(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
