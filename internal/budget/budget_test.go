package budget

import (
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/config"
)

// mockClaudeProvider implements ClaudeUsageProvider for testing.
type mockClaudeProvider struct {
	usedPercent float64
	err         error
	source      string
}

func (m *mockClaudeProvider) Name() string { return "claude" }
func (m *mockClaudeProvider) GetUsedPercent(mode string) (float64, error) {
	return m.usedPercent, m.err
}
func (m *mockClaudeProvider) LastUsedPercentSource() string {
	return m.source
}

// mockCodexProvider implements CodexUsageProvider for testing.
type mockCodexProvider struct {
	usedPercent float64
	resetTime   time.Time
	err         error
}

func (m *mockCodexProvider) Name() string { return "codex" }
func (m *mockCodexProvider) GetUsedPercent(mode string) (float64, error) {
	return m.usedPercent, m.err
}
func (m *mockCodexProvider) GetResetTime(mode string) (time.Time, error) {
	return m.resetTime, m.err
}

// mockCopilotProvider implements CopilotUsageProvider for testing.
type mockCopilotProvider struct {
	usedPercent float64
	resetTime   time.Time
	err         error
}

func (m *mockCopilotProvider) Name() string { return "copilot" }
func (m *mockCopilotProvider) GetUsedPercent(mode string) (float64, error) {
	return m.usedPercent, m.err
}
func (m *mockCopilotProvider) GetResetTime(mode string) (time.Time, error) {
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
	if r.Allowance.UsedPercent != 50 {
		t.Errorf("UsedPercent = %f, want 50", r.Allowance.UsedPercent)
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
	// Exactly at limit is NOT OK (< maxPercent required).
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
		Budget: config.BudgetConfig{Mode: "daily"},
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
	if !contains(summary, "25.0%") {
		t.Error("summary should contain used percent")
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
	if results[0].Allowance.UsedPercentSource != "api" {
		t.Errorf("UsedPercentSource = %q, want %q", results[0].Allowance.UsedPercentSource, "api")
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
