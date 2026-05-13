package snapshots

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/db"
	"github.com/marcus/nightshift/internal/tmux"
	"github.com/marcus/nightshift/internal/usage"
)

type fakeClaude struct {
	weekly int64
	daily  int64
	err    error
}

func (f fakeClaude) GetWeeklyUsage() (int64, error) { return f.weekly, f.err }
func (f fakeClaude) GetTodayUsage() (int64, error)  { return f.daily, f.err }

type fakeScraper struct {
	claudePct        float64
	codexPct         float64
	sessionResetTime string
	weeklyResetTime  string
}

func (f fakeScraper) ScrapeClaudeUsage(ctx context.Context) (tmux.UsageResult, error) {
	return tmux.UsageResult{
		Provider:         "claude",
		WeeklyPct:        f.claudePct,
		SessionResetTime: f.sessionResetTime,
		WeeklyResetTime:  f.weeklyResetTime,
		ScrapedAt:        time.Now(),
	}, nil
}

func (f fakeScraper) ScrapeCodexUsage(ctx context.Context) (tmux.UsageResult, error) {
	return tmux.UsageResult{
		Provider:         "codex",
		WeeklyPct:        f.codexPct,
		SessionResetTime: f.sessionResetTime,
		WeeklyResetTime:  f.weeklyResetTime,
		ScrapedAt:        time.Now(),
	}, nil
}

type fakeCodex struct {
	files        []string
	dailyTokens  int64
	weeklyTokens int64
	err          error
}

func (f fakeCodex) ListSessionFiles() ([]string, error) { return f.files, f.err }
func (f fakeCodex) GetTodayTokens() (int64, error)      { return f.dailyTokens, f.err }
func (f fakeCodex) GetWeeklyTokens() (int64, error)     { return f.weeklyTokens, f.err }

func TestTakeSnapshotInsertsClaude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	collector := NewCollector(database, fakeClaude{weekly: 700, daily: 120}, nil, nil, fakeScraper{claudePct: 50}, time.Monday)

	_, err = collector.TakeSnapshot(context.Background(), "claude")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}

	latest, err := collector.GetLatest("claude", 1)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(latest))
	}

	snap := latest[0]
	if snap.LocalTokens != 700 {
		t.Fatalf("local tokens = %d", snap.LocalTokens)
	}
	if snap.LocalDaily != 120 {
		t.Fatalf("local daily = %d", snap.LocalDaily)
	}
	if snap.ScrapedPct == nil || *snap.ScrapedPct != 50 {
		t.Fatalf("scraped pct = %v", snap.ScrapedPct)
	}
	if snap.InferredBudget == nil || *snap.InferredBudget != 1400 {
		t.Fatalf("inferred budget = %v", snap.InferredBudget)
	}

	weekStart := startOfWeek(snap.Timestamp, time.Monday)
	if !snap.WeekStart.Equal(weekStart) {
		t.Fatalf("week_start = %v, want %v", snap.WeekStart, weekStart)
	}
}

func TestTakeSnapshotCodexWithTokenData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	codex := fakeCodex{weeklyTokens: 35000, dailyTokens: 5000}
	collector := NewCollector(database, nil, codex, nil, fakeScraper{codexPct: 42}, time.Monday)

	snap, err := collector.TakeSnapshot(context.Background(), "codex")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}

	if snap.LocalTokens != 35000 {
		t.Fatalf("local tokens = %d, want 35000", snap.LocalTokens)
	}
	if snap.LocalDaily != 5000 {
		t.Fatalf("local daily = %d, want 5000", snap.LocalDaily)
	}
	if snap.ScrapedPct == nil || *snap.ScrapedPct != 42 {
		t.Fatalf("scraped pct = %v, want 42", snap.ScrapedPct)
	}
	// With token data + scraped pct, inferred budget = 35000 / (42/100) ≈ 83333
	if snap.InferredBudget == nil {
		t.Fatalf("inferred budget = nil, want computed value")
	}
	if *snap.InferredBudget != 83333 {
		t.Fatalf("inferred budget = %d, want 83333", *snap.InferredBudget)
	}
}

func TestTakeSnapshotCodexNoTokenData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	collector := NewCollector(database, nil, fakeCodex{}, nil, fakeScraper{codexPct: 42}, time.Monday)

	snap, err := collector.TakeSnapshot(context.Background(), "codex")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}

	if snap.LocalTokens != 0 {
		t.Fatalf("local tokens = %d, want 0", snap.LocalTokens)
	}
	if snap.ScrapedPct == nil || *snap.ScrapedPct != 42 {
		t.Fatalf("scraped pct = %v, want 42", snap.ScrapedPct)
	}
	// No local tokens, so inferred budget must be nil
	if snap.InferredBudget != nil {
		t.Fatalf("inferred budget = %v, want nil", snap.InferredBudget)
	}
}

func TestCodexTokenTotalsReturnsTokenData(t *testing.T) {
	weekly, daily, err := codexTokenTotals(fakeCodex{
		files:        []string{"/some/path.jsonl"},
		weeklyTokens: 50000,
		dailyTokens:  8000,
	})
	if err != nil {
		t.Fatalf("codexTokenTotals: %v", err)
	}
	if weekly != 50000 {
		t.Fatalf("weekly tokens = %d, want 50000", weekly)
	}
	if daily != 8000 {
		t.Fatalf("daily tokens = %d, want 8000", daily)
	}
}

func TestCodexTokenTotalsNoData(t *testing.T) {
	weekly, daily, err := codexTokenTotals(fakeCodex{})
	if err != nil {
		t.Fatalf("codexTokenTotals: %v", err)
	}
	if weekly != 0 {
		t.Fatalf("weekly tokens = %d, want 0", weekly)
	}
	if daily != 0 {
		t.Fatalf("daily tokens = %d, want 0", daily)
	}
}

func TestCodexTokenTotalsPropagatesErrors(t *testing.T) {
	_, _, err := codexTokenTotals(fakeCodex{err: context.DeadlineExceeded})
	if err == nil {
		t.Fatal("expected error from codexTokenTotals")
	}
}

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

// --- Passive mode tests ---

func TestPassiveModeSourceIsFile(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollector(database, fakeClaude{weekly: 100, daily: 10}, nil, nil, nil, time.Monday)
	snap, err := collector.TakeSnapshot(context.Background(), "claude")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}
	if snap.Source != "file" {
		t.Fatalf("source = %q, want file", snap.Source)
	}
}

// --- Active mode tests ---

func TestActiveModeClaudeAPISuccess(t *testing.T) {
	database := openTestDB(t)
	resetAt := time.Now().Add(time.Hour)
	apiResp := usage.AnthropicQuotaResponse{
		"seven_day": {Utilization: 0.42, ResetsAt: resetAt, IsEnabled: true},
		"five_hour":  {Utilization: 0.10, ResetsAt: resetAt, IsEnabled: true},
	}
	collector := NewCollectorWithAPIs(database, fakeClaude{weekly: 500, daily: 80}, nil, nil, nil, time.Monday, "active",
		fakeAnthropicAPI{resp: apiResp}, nil, nil)
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
	// Local token totals populated from file provider so inferred_budget can be computed.
	if snap.LocalTokens != 500 {
		t.Fatalf("local tokens = %d, want 500", snap.LocalTokens)
	}
	if snap.InferredBudget == nil {
		t.Fatalf("inferred budget = nil, want computed value")
	}
}

func TestActiveModeClaudeAPIFail_ReturnsError(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, nil, nil, nil, nil, time.Monday, "active",
		fakeAnthropicAPI{err: errors.New("api down")}, nil, nil)
	_, err := collector.TakeSnapshot(context.Background(), "claude")
	if err == nil {
		t.Fatal("expected error in active mode on API failure")
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
	collector := NewCollectorWithAPIs(database, nil, fakeCodex{weeklyTokens: 1000, dailyTokens: 100}, nil, nil, time.Monday, "active",
		nil, fakeCodexAPI{resp: apiResp}, nil)
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
	// Local token totals populated from file provider so inferred_budget can be computed.
	if snap.LocalTokens != 1000 {
		t.Fatalf("local tokens = %d, want 1000", snap.LocalTokens)
	}
	if snap.InferredBudget == nil {
		t.Fatalf("inferred budget = nil, want computed value")
	}
}

func TestActiveModeCodexAPIFail_ReturnsError(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, nil, nil, nil, nil, time.Monday, "active",
		nil, fakeCodexAPI{err: errors.New("api down")}, nil)
	_, err := collector.TakeSnapshot(context.Background(), "codex")
	if err == nil {
		t.Fatal("expected error in active mode on API failure")
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
	collector := NewCollectorWithAPIs(database, nil, nil, nil, nil, time.Monday, "active",
		nil, nil, fakeCopilotAPI{resp: apiResp})
	snap, err := collector.TakeSnapshot(context.Background(), "copilot")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}
	if snap.Source != "api" {
		t.Fatalf("source = %q, want api", snap.Source)
	}
	// 100 - 30 = 70% used
	if snap.ScrapedPct == nil || *snap.ScrapedPct != 70.0 {
		t.Fatalf("scraped pct = %v, want 70", snap.ScrapedPct)
	}
	if snap.WeeklyResetTime != "2026-05-20" {
		t.Fatalf("weekly reset = %q, want 2026-05-20", snap.WeeklyResetTime)
	}
}

func TestActiveModeCopilotAPIFail_ReturnsError(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, nil, nil, nil, nil, time.Monday, "active",
		nil, nil, fakeCopilotAPI{err: errors.New("api down")})
	_, err := collector.TakeSnapshot(context.Background(), "copilot")
	if err == nil {
		t.Fatal("expected error in active mode on API failure")
	}
}

func TestActiveModeCopilotNilResponse_ReturnsError(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, nil, nil, nil, nil, time.Monday, "active",
		nil, nil, fakeCopilotAPI{resp: nil})
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

// --- Hybrid mode tests ---

func TestHybridModeAPISuccess_SourceAPI(t *testing.T) {
	database := openTestDB(t)
	apiResp := usage.AnthropicQuotaResponse{
		"seven_day": {Utilization: 0.60, IsEnabled: true},
	}
	collector := NewCollectorWithAPIs(database, fakeClaude{weekly: 100, daily: 10}, nil, nil, nil, time.Monday, "hybrid",
		fakeAnthropicAPI{resp: apiResp}, nil, nil)
	snap, err := collector.TakeSnapshot(context.Background(), "claude")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}
	if snap.Source != "api" {
		t.Fatalf("source = %q, want api", snap.Source)
	}
}

func TestHybridModeAPIFail_FallsBackToFile(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, fakeClaude{weekly: 200, daily: 20}, nil, nil, nil, time.Monday, "hybrid",
		fakeAnthropicAPI{err: errors.New("api down")}, nil, nil)
	snap, err := collector.TakeSnapshot(context.Background(), "claude")
	if err != nil {
		t.Fatalf("hybrid fallback should not error: %v", err)
	}
	if snap.Source != "file" {
		t.Fatalf("source = %q, want file after fallback", snap.Source)
	}
	if snap.LocalTokens != 200 {
		t.Fatalf("local tokens = %d, want 200", snap.LocalTokens)
	}
}

func TestHybridModeCodexAPIFail_FallsBackToFile(t *testing.T) {
	database := openTestDB(t)
	collector := NewCollectorWithAPIs(database, nil, fakeCodex{weeklyTokens: 500, dailyTokens: 50}, nil, nil, time.Monday, "hybrid",
		nil, fakeCodexAPI{err: errors.New("api down")}, nil)
	snap, err := collector.TakeSnapshot(context.Background(), "codex")
	if err != nil {
		t.Fatalf("hybrid fallback should not error: %v", err)
	}
	if snap.Source != "file" {
		t.Fatalf("source = %q, want file after fallback", snap.Source)
	}
}

func TestHybridModeCopilotAPIFail_FallsBackToFile(t *testing.T) {
	database := openTestDB(t)
	copilot := fakeCopilotUsage{weekly: 100, daily: 10}
	collector := NewCollectorWithAPIs(database, nil, nil, copilot, nil, time.Monday, "hybrid",
		nil, nil, fakeCopilotAPI{err: errors.New("api down")})
	snap, err := collector.TakeSnapshot(context.Background(), "copilot")
	if err != nil {
		t.Fatalf("hybrid fallback should not error: %v", err)
	}
	if snap.Source != "file" {
		t.Fatalf("source = %q, want file after fallback", snap.Source)
	}
}

// --- Source persisted and read back ---

func TestSourcePersistedAndReadBack(t *testing.T) {
	database := openTestDB(t)
	apiResp := usage.AnthropicQuotaResponse{
		"seven_day": {Utilization: 0.30, IsEnabled: true},
	}
	collector := NewCollectorWithAPIs(database, nil, nil, nil, nil, time.Monday, "active",
		fakeAnthropicAPI{resp: apiResp}, nil, nil)
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

// fakeCopilotUsage implements CopilotUsage for tests.
type fakeCopilotUsage struct {
	weekly int64
	daily  int64
	err    error
}

func (f fakeCopilotUsage) GetWeeklyTokens() (int64, error) { return f.weekly, f.err }
func (f fakeCopilotUsage) GetTodayTokens() (int64, error)  { return f.daily, f.err }

// --- Original tests below ---

func TestTakeSnapshotStoresResetTimes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	scraper := fakeScraper{
		claudePct:        50,
		sessionResetTime: "9pm (America/Los_Angeles)",
		weeklyResetTime:  "Feb 8 at 10am (America/Los_Angeles)",
	}
	collector := NewCollector(database, fakeClaude{weekly: 700, daily: 120}, nil, nil, scraper, time.Monday)

	_, err = collector.TakeSnapshot(context.Background(), "claude")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}

	latest, err := collector.GetLatest("claude", 1)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(latest))
	}

	snap := latest[0]
	if snap.SessionResetTime != "9pm (America/Los_Angeles)" {
		t.Fatalf("session reset = %q, want %q", snap.SessionResetTime, "9pm (America/Los_Angeles)")
	}
	if snap.WeeklyResetTime != "Feb 8 at 10am (America/Los_Angeles)" {
		t.Fatalf("weekly reset = %q, want %q", snap.WeeklyResetTime, "Feb 8 at 10am (America/Los_Angeles)")
	}
}

func TestTakeSnapshotCodexResetTimes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	scraper := fakeScraper{
		codexPct:         42,
		sessionResetTime: "20:15",
		weeklyResetTime:  "20:08 on 9 Feb",
	}
	collector := NewCollector(database, nil, fakeCodex{}, nil, scraper, time.Monday)

	snap, err := collector.TakeSnapshot(context.Background(), "codex")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}

	if snap.SessionResetTime != "20:15" {
		t.Fatalf("session reset = %q, want %q", snap.SessionResetTime, "20:15")
	}
	if snap.WeeklyResetTime != "20:08 on 9 Feb" {
		t.Fatalf("weekly reset = %q, want %q", snap.WeeklyResetTime, "20:08 on 9 Feb")
	}
}

func TestTakeSnapshotEmptyResetTimes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	// scraper with no reset times
	collector := NewCollector(database, fakeClaude{weekly: 100, daily: 10}, nil, nil, fakeScraper{claudePct: 25}, time.Monday)

	_, err = collector.TakeSnapshot(context.Background(), "claude")
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}

	latest, err := collector.GetLatest("claude", 1)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}

	snap := latest[0]
	if snap.SessionResetTime != "" {
		t.Fatalf("session reset = %q, want empty", snap.SessionResetTime)
	}
	if snap.WeeklyResetTime != "" {
		t.Fatalf("weekly reset = %q, want empty", snap.WeeklyResetTime)
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

	collector := NewCollector(database, fakeClaude{}, nil, nil, nil, time.Monday)

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
