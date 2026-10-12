package migrate

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreOrderedAndComplete(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 3 {
		t.Fatalf("expected schema, rule seed, and cleanup migrations, got %d", len(migrations))
	}
	if migrations[0].version != "0001_initial" || migrations[1].version != "0002_seed_rules" || migrations[2].version != "0003_remove_import_only_rules" {
		t.Fatalf("unexpected migration order: %#v", migrations)
	}
	if !strings.Contains(migrations[2].sql, "content.import") {
		t.Fatal("cleanup migration does not remove rules tied to file import")
	}
}

func TestSplitStatementsRespectsQuotedSemicolons(t *testing.T) {
	statements := splitStatements(`-- note
CREATE TABLE "x" (value TEXT); INSERT INTO x VALUES ('a;b');`)
	if len(statements) != 2 {
		t.Fatalf("statements = %#v", statements)
	}
	if statements[1] != "INSERT INTO x VALUES ('a;b')" {
		t.Fatalf("second statement = %q", statements[1])
	}
}
