package migrate

import "testing"

func TestEmbeddedMigrationsAreOrderedAndComplete(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 2 {
		t.Fatalf("expected schema and rule seed migrations, got %d", len(migrations))
	}
	if migrations[0].version != "0001_initial" || migrations[1].version != "0002_seed_rules" {
		t.Fatalf("unexpected migration order: %#v", migrations)
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
