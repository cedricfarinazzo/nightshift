package snapshots

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/db"
	"github.com/marcus/nightshift/internal/usage"
)

// --- API client stubs ---

type fakeAnthropicAPI struct {
	resp usage.AnthropicQuotaResponse
	err  error
}

func (f fakeAnthropicAPI) FetchQuotas(_ context.Context) (usage.AnthropicQuotaResponse, error) {
	return f.resp, f.err
}

type fakeCodexAPI struct {
	resp *usage.CodexUsageResponse
	err  error
}

func (f fakeCodexAPI) FetchUsage(_ context.Context) (*usage.CodexUsageResponse, error) {
	return f.resp, f.err
}

type fakeCopilotAPI struct {
	resp *usage.CopilotUserResponse
	err  error
}

func (f fakeCopilotAPI) FetchQuotas(_ context.Context) (*usage.CopilotUserResponse, error) {
	return f.resp, f.err
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	database, err := db.Open(filepath.Join(home, "nightshift.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// --- Active mode tests ---

func TestActiveModeClaudeAPISuccess(t *testing.T) {
	database := openTestDB(t)
	resetAt := time.Now().Add(time.Hour)
	apiResp := usage.AnthropicQuotaResponse{
		"seven_day": {Utilization: 0.42, ResetsAt: resetAt, IsEnabled: true},
		"five_hour": {Utilization: 0.10, ResetsAt: resetAt, IsEnabled: true},
	}
	collector := NewCollectorWithAPIs(database, time.Monday, fakeAnthropicAPI{resp: apiResp}, nil, nil)
	snap, err := collector.TakeSnapshot(context.Background(), "claude")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}
	if snap.Source != "api" {
		t.Fatalf("source = %q, want api", snap.Source)
	}
	if snap.ScrapedPct == nil || *snap.ScrapedPct < 41.9 || *snap.ScrapedPct > 42.1 {
		t.Fatalf("scraped pct = %v, want ~42", snap.ScrapedPct)
	}
}

func TestActiveModeClaudeAPIFail_ReturnsError(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, time.Monday, fakeAnthropicAPI{err: errors.New("api down")}, nil, nil)
	_, err := collector.TakeSnapshot(context.Background(), "claude")
	if err == nil {
		t.Fatal("expected error on API failure")
	}
}

func TestActiveModeClaudeNilClient_ReturnsError(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollector(database, time.Monday)
	_, err := collector.TakeSnapshot(context.Background(), "claude")
	if err == nil {
		t.Fatal("expected error when API client is nil")
	}
}

func TestActiveModeCodexAPISuccess(t *testing.T) {
	database := openTestDB(t)
	resetAt := usage.UnixTime{Time: time.Now().Add(time.Hour)}
	apiResp := &usage.CodexUsageResponse{
		RateLimit: &usage.CodexAPIRateLimit{
			PrimaryWindow: &usage.CodexWindow{
				UsedPercent: 55.0,
				ResetAt:     resetAt,
			},
		},
	}
	collector := NewCollectorWithAPIs(database, time.Monday, nil, fakeCodexAPI{resp: apiResp}, nil)
	snap, err := collector.TakeSnapshot(context.Background(), "codex")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}
	if snap.Source != "api" {
		t.Fatalf("source = %q, want api", snap.Source)
	}
	if snap.ScrapedPct == nil || *snap.ScrapedPct != 55.0 {
		t.Fatalf("scraped pct = %v, want 55", snap.ScrapedPct)
	}
}

func TestActiveModeCodexAPIFail_ReturnsError(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, time.Monday, nil, fakeCodexAPI{err: errors.New("api down")}, nil)
	_, err := collector.TakeSnapshot(context.Background(), "codex")
	if err == nil {
		t.Fatal("expected error on API failure")
	}
}

func TestActiveModeCopilotAPISuccess(t *testing.T) {
	database := openTestDB(t)
	apiResp := &usage.CopilotUserResponse{
		QuotaResetDate: "2026-05-20",
		Quotas: usage.CopilotQuotaMap{
			"premium_interactions": {PercentRemaining: 30.0, Unlimited: false},
		},
	}
	collector := NewCollectorWithAPIs(database, time.Monday, nil, nil, fakeCopilotAPI{resp: apiResp})
	snap, err := collector.TakeSnapshot(context.Background(), "copilot")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}
	if snap.Source != "api" {
		t.Fatalf("source = %q, want api", snap.Source)
	}
	if snap.ScrapedPct == nil || *snap.ScrapedPct != 70.0 {
		t.Fatalf("scraped pct = %v, want 70", snap.ScrapedPct)
	}
	if snap.WeeklyResetTime != "2026-05-20" {
		t.Fatalf("weekly reset = %q, want 2026-05-20", snap.WeeklyResetTime)
	}
}

func TestActiveModeCopilotAPIFail_ReturnsError(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, time.Monday, nil, nil, fakeCopilotAPI{err: errors.New("api down")})
	_, err := collector.TakeSnapshot(context.Background(), "copilot")
	if err == nil {
		t.Fatal("expected error on API failure")
	}
}

func TestActiveModeCopilotNilResponse_ReturnsError(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, time.Monday, nil, nil, fakeCopilotAPI{resp: nil})
	_, err := collector.TakeSnapshot(context.Background(), "copilot")
	if err == nil {
		t.Fatal("expected error when copilot API returns nil response")
	}
}

func TestClampPct(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{50.0, 50.0},
		{0.0, 0.0},
		{100.0, 100.0},
		{-5.0, 0.0},
		{105.0, 100.0},
	}
	for _, tc := range cases {
		if got := clampPct(tc.in); got != tc.want {
			t.Errorf("clampPct(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSourcePersistedAndReadBack(t *testing.T) {
	database := openTestDB(t)
	apiResp := usage.AnthropicQuotaResponse{
		"seven_day": {Utilization: 0.30, IsEnabled: true},
	}
	collector := NewCollectorWithAPIs(database, time.Monday, fakeAnthropicAPI{resp: apiResp}, nil, nil)
	_, err := collector.TakeSnapshot(context.Background(), "claude")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}
	snaps, err := collector.GetLatest("claude", 1)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot")
	}
	if snaps[0].Source != "api" {
		t.Fatalf("source = %q, want api", snaps[0].Source)
	}
}

func TestPruneSnapshots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	collector := NewCollector(database, time.Monday)

	oldTime := time.Now().AddDate(0, 0, -3)
	weekStart := startOfWeek(oldTime, time.Monday)
	weekNumber, year := weekStart.ISOWeek()
	if _, err := database.SQL().Exec(
		`INSERT INTO snapshots (provider, timestamp, week_start, local_tokens, local_daily, scraped_pct, inferred_budget, day_of_week, hour_of_day, week_number, year)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"claude",
		oldTime,
		weekStart,
		10,
		2,
		nil,
		nil,
		int(oldTime.Weekday()),
		oldTime.Hour(),
		weekNumber,
		year,
	); err != nil {
		t.Fatalf("insert old snapshot: %v", err)
	}

	deleted, err := collector.Prune(1)
	if err != nil {
		t.Fatalf("prune snapshots: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 row deleted, got %d", deleted)
	}
}
