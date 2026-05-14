package reporting

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadRunResults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")

	now := time.Date(2024, 6, 1, 22, 0, 0, 0, time.UTC)
	r := &RunResults{
		Date:      now,
		StartTime: now,
		EndTime:   now.Add(time.Hour),
		Tasks: []TaskResult{
			{Project: "/p", TaskType: "lint-fix", Title: "ok", Status: "completed", TokensUsed: 500},
		},
	}

	if err := SaveRunResults(r, path); err != nil {
		t.Fatalf("SaveRunResults: %v", err)
	}

	got, err := LoadRunResults(path)
	if err != nil {
		t.Fatalf("LoadRunResults: %v", err)
	}
	if len(got.Tasks) != 1 {
		t.Errorf("Tasks len = %d, want 1", len(got.Tasks))
	}
	if got.Tasks[0].TokensUsed != 500 {
		t.Errorf("TokensUsed = %d, want 500", got.Tasks[0].TokensUsed)
	}
	if !got.StartTime.Equal(now) {
		t.Errorf("StartTime mismatch")
	}
}

func TestSaveRunResults_NilResults(t *testing.T) {
	dir := t.TempDir()
	err := SaveRunResults(nil, filepath.Join(dir, "r.json"))
	if err == nil {
		t.Error("expected error for nil results")
	}
}

func TestSaveRunResults_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "x", "y", "results.json")
	if err := SaveRunResults(&RunResults{}, nested); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestLoadRunResults_MissingFile(t *testing.T) {
	_, err := LoadRunResults("/nonexistent/path/results.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadRunResults_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(path, []byte("not json {{{"), 0644)
	_, err := LoadRunResults(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDefaultRunResultsPath(t *testing.T) {
	ts := time.Date(2024, 6, 1, 22, 30, 0, 0, time.UTC)
	p := DefaultRunResultsPath(ts)
	if p == "" {
		t.Error("expected non-empty path")
	}
	// Should end with timestamped filename
	base := filepath.Base(p)
	if base != "run-2024-06-01-223000.json" {
		t.Errorf("unexpected filename: %s", base)
	}
}

func TestDefaultReportsDir(t *testing.T) {
	d := DefaultReportsDir()
	if d == "" {
		t.Error("expected non-empty dir")
	}
}
