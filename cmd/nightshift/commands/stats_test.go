package commands

import (
	"os"
	"strings"
	"testing"
)

func TestRunStats_MalformedConfig(t *testing.T) {
	// Malformed YAML causes config.Load() to fail → runStats returns non-nil error.
	t.Setenv("HOME", t.TempDir())
	withTempCwd(t, "key: [unclosed bracket")

	err := runStats(false, "all")
	if err == nil {
		t.Fatal("expected error for malformed config, got nil")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("error should mention 'loading config', got: %v", err)
	}
}

func TestRunStats_MissingConfig(t *testing.T) {
	// No config file → config.Load() returns defaults → proceeds past config load.
	t.Setenv("HOME", t.TempDir())
	withTempCwd(t, "")

	err := runStats(false, "all")
	// May fail at DB open or stats compute, but must NOT fail at config load.
	if err != nil && strings.Contains(err.Error(), "loading config") {
		t.Errorf("should not fail on config load with missing file, got: %v", err)
	}
}

func TestRunStats_MalformedConfig_PeriodLastNight(t *testing.T) {
	// Period "last-night" path: runStats calls config.Load() at top → fails there.
	t.Setenv("HOME", t.TempDir())
	withTempCwd(t, "key: [unclosed bracket")

	err := runStats(false, "last-night")
	if err == nil {
		t.Fatal("expected error for malformed config, got nil")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("error should mention 'loading config', got: %v", err)
	}
}

func TestFilterStatsByPeriod_MalformedConfig_LastNight(t *testing.T) {
	// filterStatsByPeriod with last-night calls config.Load() internally.
	// Needs at least one run report in reportsDir to get past the early-return on empty runs.
	t.Setenv("HOME", t.TempDir())
	withTempCwd(t, "key: [unclosed bracket")

	reportsDir := t.TempDir()
	fakeReport := `{"start_time":"2024-01-15T22:00:00Z","end_time":"2024-01-15T23:00:00Z","tasks":[]}`
	if err := os.WriteFile(
		reportsDir+"/run-2024-01-15-220000.json",
		[]byte(fakeReport), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, err := filterStatsByPeriod(nil, nil, reportsDir, "last-night")
	if err == nil {
		t.Fatal("expected error for malformed config in filterStatsByPeriod, got nil")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("error should mention 'loading config', got: %v", err)
	}
}
