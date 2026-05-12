package budget

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/usage"
)

// --- fake passive providers ---

type fakeClaudeProvider struct {
	pct float64
	err error
	src string
}

func (f *fakeClaudeProvider) Name() string { return "claude" }
func (f *fakeClaudeProvider) GetUsedPercent(_ string, _ int64) (float64, error) {
	return f.pct, f.err
}
func (f *fakeClaudeProvider) LastUsedPercentSource() string { return f.src }

type fakeCodexProvider struct {
	pct   float64
	err   error
	reset time.Time
}

func (f *fakeCodexProvider) Name() string { return "codex" }
func (f *fakeCodexProvider) GetUsedPercent(_ string, _ int64) (float64, error) {
	return f.pct, f.err
}
func (f *fakeCodexProvider) GetResetTime(_ string) (time.Time, error) { return f.reset, nil }

type fakeCopilotProvider struct {
	pct   float64
	err   error
	reset time.Time
}

func (f *fakeCopilotProvider) Name() string { return "copilot" }
func (f *fakeCopilotProvider) GetUsedPercent(_ string, _ int64) (float64, error) {
	return f.pct, f.err
}
func (f *fakeCopilotProvider) GetResetTime(_ string) (time.Time, error) { return f.reset, nil }

// --- helpers ---

func makeConfig(tracking string) *config.Config {
	return &config.Config{
		Budget: config.BudgetConfig{
			Mode:         "weekly",
			MaxPercent:   80,
			WeeklyTokens: 1_000_000,
			Tracking:     tracking,
		},
	}
}

// anthropicTestServer returns a server that serves a minimal AnthropicQuotaResponse.
func anthropicTestServer(utilization float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type entry struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
			IsEnabled   bool    `json:"is_enabled"`
		}
		payload := map[string]entry{
			"seven_day": {Utilization: utilization, IsEnabled: true, ResetsAt: "2099-01-01T00:00:00Z"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
}

func anthropicErrorServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

// --- passive mode ---

func TestPassiveMode_NeverCallsAPI(t *testing.T) {
	apiCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	passive := &fakeClaudeProvider{pct: 42.0}
	cfg := makeConfig("passive")
	mgr := NewManagerWithTracking(cfg, passive, nil, nil)

	pct, err := mgr.GetUsedPercent("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 42.0 {
		t.Errorf("want 42.0, got %f", pct)
	}
	if apiCalled {
		t.Error("API should not be called in passive mode")
	}
}

func TestEmptyTrackingMode_BehavesLikePassive(t *testing.T) {
	passive := &fakeClaudeProvider{pct: 55.0}
	cfg := makeConfig("")
	mgr := NewManagerWithTracking(cfg, passive, nil, nil)

	pct, err := mgr.GetUsedPercent("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 55.0 {
		t.Errorf("want 55.0, got %f", pct)
	}
}

// --- active mode: anthropic adapter ---

func TestAnthropicActiveProvider_APISuccess(t *testing.T) {
	srv := anthropicTestServer(0.6) // 60% utilization
	defer srv.Close()

	passive := &fakeClaudeProvider{pct: 99.0}
	apiClient := usage.NewAnthropicClient(
		usage.WithBaseURL(srv.URL),
		usage.WithHTTPClient(srv.Client()),
		// Use a static credential store so no real credentials are needed.
		usage.WithCredentialStore(&staticCred{token: "test-token"}),
	)
	p := &anthropicActiveProvider{client: apiClient, passive: passive}

	pct, err := p.GetUsedPercent("weekly", 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 60.0 {
		t.Errorf("want 60.0, got %f", pct)
	}
	if p.LastUsedPercentSource() != "api" {
		t.Errorf("want source=api, got %s", p.LastUsedPercentSource())
	}
}

func TestAnthropicActiveProvider_APIError_FallsBackToPassive(t *testing.T) {
	srv := anthropicErrorServer()
	defer srv.Close()

	passive := &fakeClaudeProvider{pct: 33.0}
	apiClient := usage.NewAnthropicClient(
		usage.WithBaseURL(srv.URL),
		usage.WithHTTPClient(srv.Client()),
		usage.WithCredentialStore(&staticCred{token: "test-token"}),
	)
	p := &anthropicActiveProvider{client: apiClient, passive: passive}

	pct, err := p.GetUsedPercent("weekly", 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 33.0 {
		t.Errorf("want 33.0 (passive fallback), got %f", pct)
	}
	if p.LastUsedPercentSource() != "file" {
		t.Errorf("want source=file, got %s", p.LastUsedPercentSource())
	}
}

func TestAnthropicActiveProvider_NilClient_FallsBackToPassive(t *testing.T) {
	passive := &fakeClaudeProvider{pct: 20.0}
	p := &anthropicActiveProvider{client: nil, passive: passive}

	pct, err := p.GetUsedPercent("weekly", 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 20.0 {
		t.Errorf("want 20.0, got %f", pct)
	}
}

// --- copilot adapter ---

func copilotTestServer(percentRemaining float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]interface{}{
			"login":            "testuser",
			"copilot_plan":     "copilot_enterprise",
			"quota_reset_date": "2099-06-01",
			"quota_snapshots": map[string]interface{}{
				"premium_interactions": map[string]interface{}{
					"entitlement":       300,
					"remaining":         150,
					"quota_remaining":   150.0,
					"percent_remaining": percentRemaining,
					"overage_count":     0,
					"overage_permitted": false,
					"unlimited":         false,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
}

func TestCopilotActiveProvider_APISuccess(t *testing.T) {
	srv := copilotTestServer(40.0) // 40% remaining → 60% used
	defer srv.Close()

	passive := &fakeCopilotProvider{pct: 99.0}
	ghExec := func(args ...string) ([]byte, error) {
		return []byte("fake-gh-token\n"), nil
	}
	apiClient := newCopilotClientForTest(t, srv.URL, ghExec)
	p := &copilotActiveProvider{client: apiClient, passive: passive}

	pct, err := p.GetUsedPercent("weekly", 300)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 60.0 {
		t.Errorf("want 60.0, got %f", pct)
	}
	if p.LastUsedPercentSource() != "api" {
		t.Errorf("want source=api, got %s", p.LastUsedPercentSource())
	}
}

func TestCopilotActiveProvider_APIError_FallsBackToPassive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	passive := &fakeCopilotProvider{pct: 10.0}
	ghExec := func(args ...string) ([]byte, error) {
		return []byte("fake-gh-token\n"), nil
	}
	apiClient := newCopilotClientForTest(t, srv.URL, ghExec)
	p := &copilotActiveProvider{client: apiClient, passive: passive}

	pct, err := p.GetUsedPercent("weekly", 300)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 10.0 {
		t.Errorf("want 10.0 (passive fallback), got %f", pct)
	}
	if p.LastUsedPercentSource() != "file" {
		t.Errorf("want source=file, got %s", p.LastUsedPercentSource())
	}
}

// --- hybrid mode ---

func TestHybridMode_MixedCredentials(t *testing.T) {
	// Anthropic API succeeds; codex/copilot no credentials → passive used.
	srv := anthropicTestServer(0.25)
	defer srv.Close()

	passiveClaude := &fakeClaudeProvider{pct: 99.0}
	passiveCodex := &fakeCodexProvider{pct: 50.0}

	apiClient := usage.NewAnthropicClient(
		usage.WithBaseURL(srv.URL),
		usage.WithHTTPClient(srv.Client()),
		usage.WithCredentialStore(&staticCred{token: "test-token"}),
	)
	claudeP := &anthropicActiveProvider{client: apiClient, passive: passiveClaude}
	codexP := &codexActiveProvider{client: nil, passive: passiveCodex}

	cfg := makeConfig("hybrid")
	mgr := NewManager(cfg, claudeP, codexP, nil)

	pct, err := mgr.GetUsedPercent("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 25.0 {
		t.Errorf("want 25.0 (api), got %f", pct)
	}

	pct, err = mgr.GetUsedPercent("codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pct != 50.0 {
		t.Errorf("want 50.0 (passive), got %f", pct)
	}
}

// --- thread safety ---

func TestAnthropicActiveProvider_Concurrent(t *testing.T) {
	srv := anthropicTestServer(0.5)
	defer srv.Close()

	passive := &fakeClaudeProvider{pct: 99.0}
	apiClient := usage.NewAnthropicClient(
		usage.WithBaseURL(srv.URL),
		usage.WithHTTPClient(srv.Client()),
		usage.WithCredentialStore(&staticCred{token: "test-token"}),
	)
	p := &anthropicActiveProvider{client: apiClient, passive: passive}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.GetUsedPercent("weekly", 1_000_000)
			_ = p.LastUsedPercentSource()
		}()
	}
	wg.Wait()
}

// --- helpers for tests ---

// staticCred is a CredentialStore that always returns a fixed token.
type staticCred struct{ token string }

func (s *staticCred) ReadToken(_ context.Context) (string, error) { return s.token, nil }

// newCopilotClientForTest creates a CopilotClient with a custom baseURL and ghExec.
// Uses package-internal constructor via test helper exposed below.
func newCopilotClientForTest(t *testing.T, baseURL string, ghExec func(...string) ([]byte, error)) *usage.CopilotClient {
	t.Helper()
	c, err := usage.NewCopilotClientWithBaseURL(baseURL, usage.WithCopilotGHExec(ghExec))
	if err != nil {
		t.Fatalf("NewCopilotClientWithBaseURL: %v", err)
	}
	return c
}
