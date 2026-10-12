package main

import (
	"testing"
	"time"
)

func TestLegacyImportDefaultsPreserveLifecycleAndWorkflow(t *testing.T) {
	if got := migrationDefault("content", "workflow_state", map[string]any{"lifecycle": "已发布"}); got != "已发布" {
		t.Fatalf("published workflow = %v", got)
	}
	if got := migrationDefault("content", "workflow_state", map[string]any{"lifecycle": "未发布"}); got != "未发布" {
		t.Fatalf("unpublished workflow = %v", got)
	}
	if got := migrationDefault("schedule", "updated_at", map[string]any{"created_at": "legacy-time"}); got != "legacy-time" {
		t.Fatalf("schedule updated_at = %v", got)
	}
}

func TestSQLiteValuesConvertToPostgresTypes(t *testing.T) {
	if got := convert(int64(1), "boolean"); got != true {
		t.Fatalf("boolean conversion = %v", got)
	}
	got := convert("2026-10-12 18:30:00", "timestamp without time zone")
	if _, ok := got.(time.Time); !ok {
		t.Fatalf("timestamp conversion type = %T", got)
	}
}
