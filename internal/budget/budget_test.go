package budget

import (
	"context"
	"testing"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/config"
)

// mockClaudeProvider implements ClaudeUsageProvider for testing.
type mockClaudeProvider struct {
	usedPercent float64
	err         error
	source      string
}

func (m *mockClaudeProvider) Name() string { return "claude" }
func (m *mockClaudeProvider) GetHourlyCapacity(_ context.Context, maxPercent int) (HourlyCapacityResult, error) {
	if m.err != nil {
		return HourlyCapacityResult{Source: "none"}, m.err
	}
	remaining := float64(maxPercent) - m.usedPercent
	var cap float64
	if remaining > 0 {
		cap = remaining / float64(maxPercent)
	}
	return HourlyCapacityResult{
		Capacity:          cap,
		BottleneckWindow:  "mock",
		BottleneckUsedPct: m.usedPercent,
		Source:            m.source,
	}, nil
}

// mockCodexProvider implements CodexUsageProvider for testing.
type mockCodexProvider struct {
	usedPercent float64
	resetTime   time.Time
	err         error
}

func (m *mockCodexProvider) Name() string { return "codex" }
func (m *mockCodexProvider) GetHourlyCapacity(_ context.Context, maxPercent int) (HourlyCapacityResult, error) {
	if m.err != nil {
		return HourlyCapacityResult{Source: "none"}, m.err
	}
	remaining := float64(maxPercent) - m.usedPercent
	var cap float64
	if remaining > 0 {
		cap = remaining / float64(maxPercent)
	}
	return HourlyCapacityResult{
		Capacity:          cap,
		BottleneckWindow:  "mock",
		BottleneckUsedPct: m.usedPercent,
		Source:            "api",
	}, nil
}
func (m *mockCodexProvider) GetResetTime(mode string) (time.Time, error) {
	return m.resetTime, m.err
}

func TestCheckProviders_OK(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{
			MaxPercent: 80,
		},
	}
	claude := &mockClaudeProvider{usedPercent: 50}
	mgr := NewManager(cfg, claude, nil, nil)

	results, err := mgr.CheckProviders([]string{"claude"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.OK {
		t.Errorf("expected OK=true for 50%% used with 80%% limit, got reason: %s", r.Reason)
	}
	if r.Allowance == nil {
		t.Fatal("expected non-nil Allowance")
	}
	if r.Allowance.BottleneckUsedPct != 50 {
		t.Errorf("BottleneckUsedPct = %f, want 50", r.Allowance.BottleneckUsedPct)
	}
	if r.Allowance.MaxPercent != 80 {
		t.Errorf("MaxPercent = %d, want 80", r.Allowance.MaxPercent)
	}
}

func TestCheckProviders_Exhausted(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{
			MaxPercent: 80,
		},
	}
	claude := &mockClaudeProvider{usedPercent: 85}
	mgr := NewManager(cfg, claude, nil, nil)

	results, err := mgr.CheckProviders([]string{"claude"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := results[0]
	if r.OK {
		t.Error("expected OK=false for 85%% used with 80%% limit")
	}
	if r.Reason == "" {
		t.Error("expected non-empty Reason")
	}
}

func TestCheckProviders_IgnoreBudget(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{
			MaxPercent: 80,
		},
	}
	claude := &mockClaudeProvider{usedPercent: 99}
	mgr := NewManager(cfg, claude, nil, nil)

	results, err := mgr.CheckProviders([]string{"claude"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].OK {
		t.Error("expected OK=true with ignoreBudget=true")
	}
}

func TestCheckProviders_AtLimit(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{
			MaxPercent: 80,
		},
	}
	// Exactly at limit: remaining = 0 → capacity = 0 → NOT OK.
	claude := &mockClaudeProvider{usedPercent: 80}
	mgr := NewManager(cfg, claude, nil, nil)

	results, _ := mgr.CheckProviders([]string{"claude"}, false)
	if results[0].OK {
		t.Error("expected OK=false when usedPercent == maxPercent")
	}
}

func TestCheckProviders_DefaultMaxPercent(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{
			MaxPercent: 0, // zero → use default
		},
	}
	claude := &mockClaudeProvider{usedPercent: 50}
	mgr := NewManager(cfg, claude, nil, nil)

	results, _ := mgr.CheckProviders([]string{"claude"}, false)
	if !results[0].OK {
		t.Error("expected OK=true at 50%% with default max percent")
	}
	if results[0].Allowance.MaxPercent != config.DefaultMaxPercent {
		t.Errorf("MaxPercent = %d, want %d", results[0].Allowance.MaxPercent, config.DefaultMaxPercent)
	}
}

func TestCheckProviders_ProviderError(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{MaxPercent: 80},
	}
	mgr := NewManager(cfg, nil, nil, nil) // no providers

	results, err := mgr.CheckProviders([]string{"claude"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].OK {
		t.Error("expected OK=false on provider error without ignoreBudget")
	}
}

func TestCheckProviders_ProviderError_IgnoreBudget(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{MaxPercent: 80},
	}
	mgr := NewManager(cfg, nil, nil, nil)

	results, _ := mgr.CheckProviders([]string{"claude"}, true)
	if !results[0].OK {
		t.Error("expected OK=true on provider error with ignoreBudget=true")
	}
}

func TestCheckProviders_MultipleProviders(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{MaxPercent: 80},
	}
	claude := &mockClaudeProvider{usedPercent: 90} // exhausted
	codex := &mockCodexProvider{usedPercent: 40}   // OK
	mgr := NewManager(cfg, claude, codex, nil)

	results, _ := mgr.CheckProviders([]string{"claude", "codex"}, false)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].OK {
		t.Error("claude should be exhausted")
	}
	if !results[1].OK {
		t.Error("codex should be OK")
	}
}

func TestGetUsedPercent_Errors(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{MaxPercent: 80},
	}

	mgr := NewManager(cfg, nil, nil, nil)
	_, err := mgr.GetUsedPercent("claude")
	if err == nil {
		t.Error("expected error for missing claude provider")
	}

	_, err = mgr.GetUsedPercent("codex")
	if err == nil {
		t.Error("expected error for missing codex provider")
	}

	_, err = mgr.GetUsedPercent("unknown")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestSummary(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{
			MaxPercent: 80,
		},
	}

	claude := &mockClaudeProvider{usedPercent: 25}
	mgr := NewManager(cfg, claude, nil, nil)

	summary, err := mgr.Summary("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary == "" {
		t.Error("summary should not be empty")
	}
	if !contains(summary, "claude") {
		t.Error("summary should contain provider name")
	}
	if !contains(summary, "OK") {
		t.Error("summary should contain OK status")
	}
}

func TestSummary_Exhausted(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{MaxPercent: 80},
	}
	claude := &mockClaudeProvider{usedPercent: 85}
	mgr := NewManager(cfg, claude, nil, nil)

	summary, _ := mgr.Summary("claude")
	if !contains(summary, "exhausted") {
		t.Errorf("expected 'exhausted' in summary, got: %s", summary)
	}
}

func TestAllowanceResult_Source(t *testing.T) {
	cfg := &config.Config{
		Budget: config.BudgetConfig{MaxPercent: 80},
	}
	claude := &mockClaudeProvider{usedPercent: 25, source: "api"}
	mgr := NewManager(cfg, claude, nil, nil)

	results, _ := mgr.CheckProviders([]string{"claude"}, false)
	if results[0].Allowance.Source != "api" {
		t.Errorf("Source = %q, want %q", results[0].Allowance.Source, "api")
	}
}

// TestComputeWindowCapacity verifies the hourly capacity formula across scenarios.
func TestComputeWindowCapacity(t *testing.T) {
	tests := []struct {
		name        string
		usedPct     float64
		maxPct      float64
		windowHours float64
		resetHours  float64
		wantMin     float64 // capacity should be >= wantMin
		wantMax     float64 // capacity should be <= wantMax
	}{
		{
			name:    "exhausted window",
			usedPct: 95, maxPct: 90, windowHours: 5, resetHours: 1,
			wantMin: 0, wantMax: 0,
		},
		{
			name:    "nearly depleted but expiring — gets boost",
			usedPct: 80, maxPct: 90, windowHours: 5, resetHours: 0.5,
			wantMin: 1.0, wantMax: 1.0, // expiring fires first: (10/90)*5/0.5=1.11 → capped 1.0
		},
		{
			name:    "expiring with capacity — boost",
			usedPct: 50, maxPct: 90, windowHours: 5, resetHours: 0.5,
			wantMin: 0.9, wantMax: 1.0, // capped at 1.0
		},
		{
			name:    "behind pace — boost",
			usedPct: 20, maxPct: 90, windowHours: 5, resetHours: 2,
			wantMin: 0.7, wantMax: 1.0,
		},
		{
			name:    "on pace",
			usedPct: 45, maxPct: 90, windowHours: 5, resetHours: 2.5,
			wantMin: 0.45, wantMax: 0.6,
		},
		{
			name:    "weekly on pace",
			usedPct: 46, maxPct: 90, windowHours: 168, resetHours: 83,
			wantMin: 0.45, wantMax: 0.55,
		},
		{
			name:    "weekly nearly done but expiring — gets boost",
			usedPct: 85, maxPct: 90, windowHours: 168, resetHours: 10,
			wantMin: 0.9, wantMax: 1.0, // expiring (10h < 42h): (5/90)*168/10=0.93
		},
		{
			name:    "nearly depleted with time to go — no boost",
			usedPct: 80, maxPct: 90, windowHours: 5, resetHours: 2,
			wantMin: 0, wantMax: 0.12, // not expiring (2h > 1.25h): 10/90 = 11.1%
		},
		// Boundary: remaining exactly at nearlyDepletedThreshold (15.0) — NOT < 15, goes to pace check.
		{
			name:    "remaining exactly at threshold — pace branch",
			usedPct: 75, maxPct: 90, windowHours: 5, resetHours: 2,
			wantMin: 0.15, wantMax: 1.0, // 15/90=16.7% × paceFactor; pace check fires, not nearly-depleted
		},
		// Boundary: remaining just below threshold (14.9) — nearly-depleted fires.
		{
			name:    "remaining just below threshold — nearly-depleted branch",
			usedPct: 75.1, maxPct: 90, windowHours: 5, resetHours: 2,
			wantMin: 0, wantMax: 0.167, // not expiring; 14.9/90 = 16.6% raw fraction
		},
		// Boundary: resetHours exactly at windowHours/4 — NOT < window/4, goes to pace check.
		{
			name:    "reset exactly at window/4 boundary — pace branch",
			usedPct: 45, maxPct: 90, windowHours: 5, resetHours: 1.25,
			wantMin: 0.4, wantMax: 1.0, // (5/4=1.25): not expiring, pace fires
		},
		// Boundary: resetHours just below windowHours/4 — expiring fires.
		{
			name:    "reset just below window/4 — expiring branch",
			usedPct: 45, maxPct: 90, windowHours: 5, resetHours: 1.24,
			wantMin: 0.9, wantMax: 1.0, // expiring: (45/90)*5/1.24 ≈ 2.0 → capped 1.0
		},
		// Normal: ahead of pace (headroomRate > idealRate) — no boost.
		{
			name:    "ahead of pace — no boost",
			usedPct: 10, maxPct: 90, windowHours: 5, resetHours: 4,
			wantMin: 0.85, wantMax: 0.9, // headroom=80/4=20 > ideal=90/5=18; paceFactor=1; (80/90)=88.9%
		},
		// exhausted exactly at limit.
		{
			name:    "used equals max — exhausted",
			usedPct: 90, maxPct: 90, windowHours: 5, resetHours: 1,
			wantMin: 0, wantMax: 0,
		},
		// zero resetHours guard: should not divide by zero.
		{
			name:    "zero resetHours — guarded",
			usedPct: 50, maxPct: 90, windowHours: 5, resetHours: 0,
			wantMin: 0.9, wantMax: 1.0, // resetHours clamped to 0.01 → expiring → capped 1.0
		},
		// monthly window (720h), well within pace.
		{
			name:    "monthly window on pace",
			usedPct: 45, maxPct: 90, windowHours: 720, resetHours: 360,
			wantMin: 0.45, wantMax: 0.6, // symmetric midpoint, pace factor ≈ 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeWindowCapacity(tt.usedPct, tt.maxPct, tt.windowHours, tt.resetHours)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("computeWindowCapacity(%.0f, %.0f, %.0f, %.1f) = %.3f, want [%.3f, %.3f]",
					tt.usedPct, tt.maxPct, tt.windowHours, tt.resetHours, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// mockClaudeProviderWithWindows returns a specific HourlyCapacityResult with Windows.
type mockClaudeProviderWithWindows struct {
	result HourlyCapacityResult
	err    error
}

func (m *mockClaudeProviderWithWindows) Name() string { return "claude" }
func (m *mockClaudeProviderWithWindows) GetHourlyCapacity(_ context.Context, _ int) (HourlyCapacityResult, error) {
	return m.result, m.err
}

func TestGetHourlyCapacity_OK(t *testing.T) {
	cfg := &config.Config{Budget: config.BudgetConfig{MaxPercent: 80}}
	windows := []WindowCapacity{
		{Name: "five_hour", UsedPct: 40, ResetIn: 3 * time.Hour, Capacity: 0.5},
		{Name: "seven_day", UsedPct: 30, ResetIn: 100 * time.Hour, Capacity: 0.7},
	}
	claude := &mockClaudeProviderWithWindows{result: HourlyCapacityResult{
		Capacity: 0.5, BottleneckWindow: "five_hour", BottleneckUsedPct: 40,
		Windows: windows, Source: "api",
	}}
	mgr := NewManager(cfg, claude, nil, nil)

	hcr, err := mgr.GetHourlyCapacity(context.Background(), "claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hcr.Capacity != 0.5 {
		t.Errorf("Capacity = %f, want 0.5", hcr.Capacity)
	}
	if hcr.BottleneckWindow != "five_hour" {
		t.Errorf("BottleneckWindow = %q, want five_hour", hcr.BottleneckWindow)
	}
	if len(hcr.Windows) != 2 {
		t.Errorf("Windows len = %d, want 2", len(hcr.Windows))
	}
}

func TestGetHourlyCapacity_UnknownProvider(t *testing.T) {
	cfg := &config.Config{Budget: config.BudgetConfig{MaxPercent: 80}}
	mgr := NewManager(cfg, nil, nil, nil)

	_, err := mgr.GetHourlyCapacity(context.Background(), "unknown")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestGetHourlyCapacity_NilProvider(t *testing.T) {
	cfg := &config.Config{Budget: config.BudgetConfig{MaxPercent: 80}}
	mgr := NewManager(cfg, nil, nil, nil) // all providers nil

	_, err := mgr.GetHourlyCapacity(context.Background(), "claude")
	if err == nil {
		t.Error("expected error when claude provider is nil")
	}

	_, err = mgr.GetHourlyCapacity(context.Background(), "codex")
	if err == nil {
		t.Error("expected error when codex provider is nil")
	}

	_, err = mgr.GetHourlyCapacity(context.Background(), "copilot")
	if err == nil {
		t.Error("expected error when copilot provider is nil")
	}
}

func TestCheckProviders_WindowsPropagated(t *testing.T) {
	cfg := &config.Config{Budget: config.BudgetConfig{MaxPercent: 80}}
	windows := []WindowCapacity{
		{Name: "five_hour", UsedPct: 50, ResetIn: 2 * time.Hour, Capacity: 0.6},
	}
	claude := &mockClaudeProviderWithWindows{result: HourlyCapacityResult{
		Capacity: 0.6, BottleneckWindow: "five_hour", BottleneckUsedPct: 50,
		Windows: windows, Source: "api",
	}}
	mgr := NewManager(cfg, claude, nil, nil)

	results, _ := mgr.CheckProviders([]string{"claude"}, false)
	if len(results[0].Allowance.Windows) != 1 {
		t.Errorf("Windows not propagated: got %d, want 1", len(results[0].Allowance.Windows))
	}
	if results[0].Allowance.Windows[0].Name != "five_hour" {
		t.Errorf("Window name = %q, want five_hour", results[0].Allowance.Windows[0].Name)
	}
}

func TestCheckProviders_HourlyCapacityInAllowance(t *testing.T) {
	cfg := &config.Config{Budget: config.BudgetConfig{MaxPercent: 80}}
	claude := &mockClaudeProviderWithWindows{result: HourlyCapacityResult{
		Capacity: 0.42, BottleneckWindow: "seven_day", BottleneckUsedPct: 55,
		Source: "api",
	}}
	mgr := NewManager(cfg, claude, nil, nil)

	results, _ := mgr.CheckProviders([]string{"claude"}, false)
	a := results[0].Allowance
	if a.HourlyCapacity != 0.42 {
		t.Errorf("HourlyCapacity = %f, want 0.42", a.HourlyCapacity)
	}
	if a.BottleneckWindow != "seven_day" {
		t.Errorf("BottleneckWindow = %q, want seven_day", a.BottleneckWindow)
	}
	if a.BottleneckUsedPct != 55 {
		t.Errorf("BottleneckUsedPct = %f, want 55", a.BottleneckUsedPct)
	}
}

func TestCheckProviders_ExactlyZeroCapacity_NotOK(t *testing.T) {
	cfg := &config.Config{Budget: config.BudgetConfig{MaxPercent: 80}}
	claude := &mockClaudeProviderWithWindows{result: HourlyCapacityResult{
		Capacity: 0.0, BottleneckWindow: "five_hour", BottleneckUsedPct: 85,
		Source: "api",
	}}
	mgr := NewManager(cfg, claude, nil, nil)

	results, _ := mgr.CheckProviders([]string{"claude"}, false)
	if results[0].OK {
		t.Error("expected OK=false when Capacity=0")
	}
}

func TestCheckProviders_TinyCapacity_IsOK(t *testing.T) {
	cfg := &config.Config{Budget: config.BudgetConfig{MaxPercent: 80}}
	claude := &mockClaudeProviderWithWindows{result: HourlyCapacityResult{
		Capacity: 0.001, BottleneckWindow: "five_hour", BottleneckUsedPct: 79,
		Source: "api",
	}}
	mgr := NewManager(cfg, claude, nil, nil)

	results, _ := mgr.CheckProviders([]string{"claude"}, false)
	if !results[0].OK {
		t.Error("expected OK=true when Capacity > 0 (even tiny)")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
