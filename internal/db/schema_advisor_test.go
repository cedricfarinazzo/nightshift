package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestAnalyzeSchema_UpToDate(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sqlDB.Close()

	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	analysis, err := AnalyzeSchema(sqlDB)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(analysis.MissingMigrations) != 0 {
		t.Fatalf("expected no missing migrations, got %d", len(analysis.MissingMigrations))
	}
	if len(analysis.UnexpectedObjects) != 0 {
		t.Fatalf("expected no unexpected objects, got %v", analysis.UnexpectedObjects)
	}
}

func TestAnalyzeSchema_MissingMigrations(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY, applied_at DATETIME)`); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO schema_version (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, 1); err != nil {
		t.Fatalf("insert schema_version: %v", err)
	}

	analysis, err := AnalyzeSchema(sqlDB)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	migs := GetMigrations()
	expected := 0
	for _, m := range migs {
		if m.Version > 1 {
			expected++
		}
	}
	if len(analysis.MissingMigrations) != expected {
		t.Fatalf("expected %d missing migrations, got %d", expected, len(analysis.MissingMigrations))
	}
}

func TestAnalyzeSchema_Drift(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sqlDB.Close()

	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE drift_extra (id INTEGER);`); err != nil {
		t.Fatalf("create drift table: %v", err)
	}
	analysis, err := AnalyzeSchema(sqlDB)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	found := false
	for _, tname := range analysis.UnexpectedObjects {
		if tname == "drift_extra" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected drift_extra to be reported in UnexpectedObjects, got %v", analysis.UnexpectedObjects)
	}
}
