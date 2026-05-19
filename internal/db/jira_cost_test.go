package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/usage"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// --- Jira runs ---

func TestSaveAndGetJiraRun(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	started := time.Date(2024, 6, 1, 22, 0, 0, 0, time.UTC)
	if err := db.SaveJiraRun(ctx, "run-1", "VC", started); err != nil {
		t.Fatalf("SaveJiraRun: %v", err)
	}

	run, err := db.GetJiraRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetJiraRun: %v", err)
	}
	if run == nil {
		t.Fatal("expected non-nil run")
	}
	if run.RunID != "run-1" {
		t.Errorf("RunID = %q, want run-1", run.RunID)
	}
	if run.ProjectKey != "VC" {
		t.Errorf("ProjectKey = %q, want VC", run.ProjectKey)
	}
	if run.EndedAt != nil {
		t.Error("EndedAt should be nil before UpdateJiraRun")
	}
}

func TestGetJiraRun_NotFound(t *testing.T) {
	db := openTestDB(t)
	run, err := db.GetJiraRun(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run != nil {
		t.Error("expected nil for missing run")
	}
}

func TestUpdateJiraRun(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	started := time.Date(2024, 6, 1, 22, 0, 0, 0, time.UTC)
	_ = db.SaveJiraRun(ctx, "run-2", "VC", started)

	ended := started.Add(time.Hour)
	if err := db.UpdateJiraRun(ctx, "run-2", ended, 5, 4, 1); err != nil {
		t.Fatalf("UpdateJiraRun: %v", err)
	}

	run, _ := db.GetJiraRun(ctx, "run-2")
	if run.TicketsProcessed != 5 {
		t.Errorf("TicketsProcessed = %d, want 5", run.TicketsProcessed)
	}
	if run.TicketsCompleted != 4 {
		t.Errorf("TicketsCompleted = %d, want 4", run.TicketsCompleted)
	}
	if run.TicketsFailed != 1 {
		t.Errorf("TicketsFailed = %d, want 1", run.TicketsFailed)
	}
	if run.EndedAt == nil {
		t.Error("EndedAt should be set after update")
	}
}

func TestGetLatestJiraRunID_Empty(t *testing.T) {
	db := openTestDB(t)
	id, err := db.GetLatestJiraRunID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty string, got %q", id)
	}
}

func TestGetLatestJiraRunID_ReturnsNewest(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	t1 := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 2, 10, 0, 0, 0, time.UTC)
	_ = db.SaveJiraRun(ctx, "old-run", "VC", t1)
	_ = db.SaveJiraRun(ctx, "new-run", "VC", t2)

	id, err := db.GetLatestJiraRunID(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "new-run" {
		t.Errorf("expected new-run, got %q", id)
	}
}

// --- Jira ticket results ---

func TestSaveAndGetJiraTicketResults(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	started := time.Now().UTC()
	_ = db.SaveJiraRun(ctx, "run-3", "VC", started)

	r := JiraTicketResult{
		RunID:        "run-3",
		TicketKey:    "VC-42",
		Status:       "completed",
		DurationMs:   5000,
		PhaseReached: "implement",
		PRURL:        "https://github.com/org/repo/pull/7",
	}
	if err := db.SaveJiraTicketResult(ctx, r); err != nil {
		t.Fatalf("SaveJiraTicketResult: %v", err)
	}

	results, err := db.GetJiraTicketResults(ctx, "run-3")
	if err != nil {
		t.Fatalf("GetJiraTicketResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	got := results[0]
	if got.TicketKey != "VC-42" {
		t.Errorf("TicketKey = %q, want VC-42", got.TicketKey)
	}
	if got.PRURL != "https://github.com/org/repo/pull/7" {
		t.Errorf("PRURL = %q", got.PRURL)
	}
	if got.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want 5000", got.DurationMs)
	}
}

func TestSaveJiraTicketResult_EmptyOptionalFields(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_ = db.SaveJiraRun(ctx, "run-4", "VC", time.Now())
	r := JiraTicketResult{
		RunID:     "run-4",
		TicketKey: "VC-10",
		Status:    "failed",
		ErrorMsg:  "agent timeout",
	}
	if err := db.SaveJiraTicketResult(ctx, r); err != nil {
		t.Fatalf("SaveJiraTicketResult: %v", err)
	}

	results, _ := db.GetJiraTicketResults(ctx, "run-4")
	if results[0].ErrorMsg != "agent timeout" {
		t.Errorf("ErrorMsg = %q, want 'agent timeout'", results[0].ErrorMsg)
	}
	if results[0].PRURL != "" {
		t.Errorf("PRURL should be empty, got %q", results[0].PRURL)
	}
}

// --- Jira phase logs ---

func TestSaveAndGetJiraPhaseLogs(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_ = db.SaveJiraRun(ctx, "run-5", "VC", time.Now())
	l := JiraPhaseLog{
		RunID:      "run-5",
		TicketKey:  "VC-55",
		Phase:      "plan",
		Provider:   "claude",
		Model:      "claude-sonnet-4-6",
		StartedAt:  time.Date(2024, 6, 1, 22, 0, 0, 0, time.UTC),
		DurationMs: 12000,
		ExitOk:     true,
		Output:     "plan output",
	}
	if err := db.SaveJiraPhaseLog(ctx, l); err != nil {
		t.Fatalf("SaveJiraPhaseLog: %v", err)
	}

	logs, err := db.GetJiraPhaseLogs(ctx, "run-5", "", "")
	if err != nil {
		t.Fatalf("GetJiraPhaseLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	got := logs[0]
	if got.Phase != "plan" {
		t.Errorf("Phase = %q, want plan", got.Phase)
	}
	if !got.ExitOk {
		t.Error("ExitOk should be true")
	}
	if got.Output != "plan output" {
		t.Errorf("Output = %q", got.Output)
	}
}

func TestGetJiraPhaseLogs_FilterByTicketAndPhase(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_ = db.SaveJiraRun(ctx, "run-6", "VC", time.Now())
	for _, phase := range []string{"plan", "implement", "plan"} {
		_ = db.SaveJiraPhaseLog(ctx, JiraPhaseLog{
			RunID: "run-6", TicketKey: "VC-1", Phase: phase, StartedAt: time.Now(),
		})
	}
	_ = db.SaveJiraPhaseLog(ctx, JiraPhaseLog{
		RunID: "run-6", TicketKey: "VC-2", Phase: "plan", StartedAt: time.Now(),
	})

	// filter by ticket
	logs, _ := db.GetJiraPhaseLogs(ctx, "run-6", "VC-1", "")
	if len(logs) != 3 {
		t.Errorf("expected 3 logs for VC-1, got %d", len(logs))
	}

	// filter by phase
	logs, _ = db.GetJiraPhaseLogs(ctx, "run-6", "", "implement")
	if len(logs) != 1 {
		t.Errorf("expected 1 implement log, got %d", len(logs))
	}

	// filter by both
	logs, _ = db.GetJiraPhaseLogs(ctx, "run-6", "VC-1", "plan")
	if len(logs) != 2 {
		t.Errorf("expected 2 plan logs for VC-1, got %d", len(logs))
	}
}

// --- Cost snapshots ---

func TestSaveAndGetLatestCostSnapshot(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	budget := 100.0
	used := 25.0
	remaining := 75.0
	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	s := &usage.CostSnapshot{
		Provider:    "anthropic",
		TotalBudget: &budget,
		Used:        &used,
		Remaining:   &remaining,
		Currency:    "USD",
		Period:      "monthly",
		CapturedAt:  ts,
	}
	if err := db.SaveCostSnapshot(ctx, s); err != nil {
		t.Fatalf("SaveCostSnapshot: %v", err)
	}

	got, err := db.GetLatestCostSnapshot(ctx, "anthropic")
	if err != nil {
		t.Fatalf("GetLatestCostSnapshot: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if got.Provider != "anthropic" {
		t.Errorf("Provider = %q, want anthropic", got.Provider)
	}
	if got.Used == nil || *got.Used != 25.0 {
		t.Errorf("Used = %v, want 25.0", got.Used)
	}
	if got.TotalBudget == nil || *got.TotalBudget != 100.0 {
		t.Errorf("TotalBudget = %v, want 100.0", got.TotalBudget)
	}
}

func TestGetLatestCostSnapshot_NotFound(t *testing.T) {
	db := openTestDB(t)
	got, err := db.GetLatestCostSnapshot(context.Background(), "codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing provider")
	}
}

func TestGetLatestCostSnapshot_ReturnsNewest(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	v1 := 10.0
	v2 := 20.0
	t1 := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 2, 10, 0, 0, 0, time.UTC)

	_ = db.SaveCostSnapshot(ctx, &usage.CostSnapshot{Provider: "anthropic", Used: &v1, CapturedAt: t1})
	_ = db.SaveCostSnapshot(ctx, &usage.CostSnapshot{Provider: "anthropic", Used: &v2, CapturedAt: t2})

	got, _ := db.GetLatestCostSnapshot(ctx, "anthropic")
	if got.Used == nil || *got.Used != 20.0 {
		t.Errorf("expected newest snapshot (used=20), got %v", got.Used)
	}
}

func TestSaveCostSnapshot_NilPointer(t *testing.T) {
	db := openTestDB(t)
	err := db.SaveCostSnapshot(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil snapshot")
	}
}

func TestSaveCostSnapshot_NilOptionalFields(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	s := &usage.CostSnapshot{
		Provider:   "copilot",
		CapturedAt: time.Now().UTC(),
	}
	if err := db.SaveCostSnapshot(ctx, s); err != nil {
		t.Fatalf("SaveCostSnapshot with nil fields: %v", err)
	}

	got, _ := db.GetLatestCostSnapshot(ctx, "copilot")
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.TotalBudget != nil || got.Used != nil || got.Remaining != nil {
		t.Error("optional fields should be nil when not set")
	}
}

func TestGetYesterdayCostSnapshot(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	v := 50.0
	_ = db.SaveCostSnapshot(ctx, &usage.CostSnapshot{
		Provider: "codex", Used: &v, CapturedAt: yesterday,
	})

	got, err := db.GetYesterdayCostSnapshot(ctx, "codex")
	if err != nil {
		t.Fatalf("GetYesterdayCostSnapshot: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil snapshot from yesterday")
	}
	if got.Used == nil || *got.Used != 50.0 {
		t.Errorf("Used = %v, want 50.0", got.Used)
	}
}

func TestGetYesterdayCostSnapshot_TodayNotReturned(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	v := 99.0
	_ = db.SaveCostSnapshot(ctx, &usage.CostSnapshot{
		Provider: "codex", Used: &v, CapturedAt: time.Now().UTC(),
	})

	got, err := db.GetYesterdayCostSnapshot(ctx, "codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("today's snapshot should not be returned by GetYesterdayCostSnapshot")
	}
}

// --- Helpers ---

func TestNullableFloat64(t *testing.T) {
	if nullableFloat64(nil) != nil {
		t.Error("nil pointer should return nil")
	}
	v := 3.14
	if nullableFloat64(&v) != 3.14 {
		t.Error("non-nil pointer should return value")
	}
}

func TestNullableString(t *testing.T) {
	if nullableString("") != nil {
		t.Error("empty string should return nil")
	}
	if nullableString("hello") != "hello" {
		t.Error("non-empty string should return value")
	}
}
