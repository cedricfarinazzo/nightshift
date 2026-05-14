package budget

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/usage"
)

// --- helpers ---

func makeConfig() *config.Config {
	return &config.Config{
		Budget: config.BudgetConfig{
			Mode:         "weekly",
			MaxPercent:   80,
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

// --- active mode: anthropic provider ---

func TestAnthropicActiveProvider_APISuccess(t *testing.T) {
	srv := anthropicTestServer(0.6) // 60% utilization
	defer srv.Close()

	apiClient := usage.NewAnthropicClient(
		usage.WithBaseURL(srv.URL),
		usage.WithHTTPClient(srv.Client()),
		usage.WithCredentialStore(&staticCred{token: "test-token"}),
	)
	p := &anthropicActiveProvider{client: apiClient}

	hcr, err := p.GetHourlyCapacity(context.Background(), 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hcr.BottleneckUsedPct != 60.0 {
		t.Errorf("want BottleneckUsedPct=60.0, got %f", hcr.BottleneckUsedPct)
	}
	if hcr.Source != "api" {
		t.Errorf("want source=api, got %s", hcr.Source)
	}
}

func TestAnthropicActiveProvider_APIError_ReturnsZero(t *testing.T) {
	srv := anthropicErrorServer()
	defer srv.Close()

	apiClient := usage.NewAnthropicClient(
		usage.WithBaseURL(srv.URL),
		usage.WithHTTPClient(srv.Client()),
		usage.WithCredentialStore(&staticCred{token: "test-token"}),
	)
	p := &anthropicActiveProvider{client: apiClient}

	hcr, err := p.GetHourlyCapacity(context.Background(), 80)
	if err == nil {
		t.Fatalf("expected error on API failure, got nil")
	}
	if hcr.Capacity != 0.0 {
		t.Errorf("want Capacity=0.0 on API error, got %f", hcr.Capacity)
	}
	if hcr.Source != "none" {
		t.Errorf("want source=none, got %s", hcr.Source)
	}
}

func TestAnthropicActiveProvider_NilClient_ReturnsZero(t *testing.T) {
	p := &anthropicActiveProvider{client: nil}

	hcr, err := p.GetHourlyCapacity(context.Background(), 80)
	if err == nil {
		t.Fatalf("expected error on nil client, got nil")
	}
	if hcr.Capacity != 0.0 {
		t.Errorf("want Capacity=0.0, got %f", hcr.Capacity)
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

	ghExec := func(args ...string) ([]byte, error) {
		return []byte("fake-gh-token\n"), nil
	}
	apiClient := newCopilotClientForTest(t, srv.URL, ghExec)
	p := &copilotActiveProvider{client: apiClient}

	hcr, err := p.GetHourlyCapacity(context.Background(), 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hcr.BottleneckUsedPct != 60.0 {
		t.Errorf("want BottleneckUsedPct=60.0, got %f", hcr.BottleneckUsedPct)
	}
	if hcr.Source != "api" {
		t.Errorf("want source=api, got %s", hcr.Source)
	}
}

func TestCopilotActiveProvider_APIError_ReturnsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ghExec := func(args ...string) ([]byte, error) {
		return []byte("fake-gh-token\n"), nil
	}
	apiClient := newCopilotClientForTest(t, srv.URL, ghExec)
	p := &copilotActiveProvider{client: apiClient}

	hcr, err := p.GetHourlyCapacity(context.Background(), 80)
	if err == nil {
		t.Fatalf("expected error on API failure, got nil")
	}
	if hcr.Capacity != 0.0 {
		t.Errorf("want Capacity=0.0 on API error, got %f", hcr.Capacity)
	}
	if hcr.Source != "none" {
		t.Errorf("want source=none, got %s", hcr.Source)
	}
}

// --- NewManagerWithTracking ---

func TestNewManagerWithTracking_Constructs(t *testing.T) {
	cfg := makeConfig()
	// Construction succeeds; providers may or may not have credentials depending on environment.
	mgr := NewManagerWithTracking(cfg)
	if mgr == nil {
		t.Fatal("NewManagerWithTracking returned nil manager")
	}
	// GetHourlyCapacity should not panic; returns error if credentials unavailable, data if available.
	_, _ = mgr.GetHourlyCapacity(context.Background(), "claude")
}

func TestActiveMode_APISucceeds_ReturnsAPIValue(t *testing.T) {
	srv := anthropicTestServer(0.25)
	defer srv.Close()

	apiClient := usage.NewAnthropicClient(
		usage.WithBaseURL(srv.URL),
		usage.WithHTTPClient(srv.Client()),
		usage.WithCredentialStore(&staticCred{token: "test-token"}),
	)
	claudeP := &anthropicActiveProvider{client: apiClient}
	codexP := &codexActiveProvider{client: nil}

	cfg := makeConfig()
	mgr := NewManager(cfg, claudeP, codexP, nil)

	hcr, err := mgr.GetHourlyCapacity(context.Background(), "claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hcr.BottleneckUsedPct != 25.0 {
		t.Errorf("want BottleneckUsedPct=25.0 (api), got %f", hcr.BottleneckUsedPct)
	}

	_, err = mgr.GetHourlyCapacity(context.Background(), "codex")
	if err == nil {
		t.Fatalf("expected error on nil client, got nil")
	}
}

// --- thread safety ---

func TestAnthropicActiveProvider_Concurrent(t *testing.T) {
	srv := anthropicTestServer(0.5)
	defer srv.Close()

	apiClient := usage.NewAnthropicClient(
		usage.WithBaseURL(srv.URL),
		usage.WithHTTPClient(srv.Client()),
		usage.WithCredentialStore(&staticCred{token: "test-token"}),
	)
	p := &anthropicActiveProvider{client: apiClient}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.GetHourlyCapacity(context.Background(), 80)
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
