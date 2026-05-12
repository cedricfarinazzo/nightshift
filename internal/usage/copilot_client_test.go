package usage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchQuotas_PremiumPlan(t *testing.T) {
	raw := copilotAPIResponse{
		Login:          "octocat",
		CopilotPlan:    "pro",
		AccessTypeSKU:  "copilot_pro",
		QuotaResetDate: "2026-06-01",
		QuotaSnapshots: map[string]CopilotQuotaSnapshot{
			"premium_interactions": {
				Entitlement:      300,
				Remaining:        250,
				PercentRemaining: 83.33,
				OveragePermitted: true,
				Unlimited:        false,
			},
			"chat": {
				Entitlement: 0,
				Unlimited:   true,
			},
		},
	}
	srv := jsonServer(t, raw, http.StatusOK)
	defer srv.Close()

	c := clientWithServer(t, srv)
	resp, err := c.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Login != "octocat" {
		t.Errorf("login = %q, want %q", resp.Login, "octocat")
	}
	prem, ok := resp.Quotas["premium_interactions"]
	if !ok {
		t.Fatal("missing premium_interactions quota")
	}
	if prem.Remaining != 250 {
		t.Errorf("remaining = %d, want 250", prem.Remaining)
	}
	if prem.PercentRemaining != 83.33 {
		t.Errorf("percent_remaining = %v, want 83.33", prem.PercentRemaining)
	}
	chat := resp.Quotas["chat"]
	if !chat.Unlimited {
		t.Error("chat quota should be unlimited")
	}
}

func TestFetchQuotas_FreePlan(t *testing.T) {
	raw := copilotAPIResponse{
		Login:       "freeuser",
		CopilotPlan: "free",
		LimitedUserQuotas: map[string]int{
			"completions": 150,
			"chat":        40,
		},
		MonthlyQuotas: map[string]int{
			"completions": 2000,
			"chat":        50,
		},
	}
	srv := jsonServer(t, raw, http.StatusOK)
	defer srv.Close()

	c := clientWithServer(t, srv)
	resp, err := c.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	comp := resp.Quotas["completions"]
	if comp.Remaining != 150 {
		t.Errorf("completions remaining = %d, want 150", comp.Remaining)
	}
	if comp.Entitlement != 2000 {
		t.Errorf("completions entitlement = %d, want 2000", comp.Entitlement)
	}
	wantPct := float64(150) / float64(2000) * 100
	if comp.PercentRemaining != wantPct {
		t.Errorf("percent_remaining = %v, want %v", comp.PercentRemaining, wantPct)
	}
}

func TestFetchQuotas_Unauthorized(t *testing.T) {
	srv := statusServer(http.StatusUnauthorized)
	defer srv.Close()

	c := clientWithServer(t, srv)
	_, err := c.FetchQuotas(context.Background())
	if !errors.Is(err, ErrCopilotUnauthorized) {
		t.Errorf("err = %v, want ErrCopilotUnauthorized", err)
	}
}

func TestFetchQuotas_NotFound(t *testing.T) {
	srv := statusServer(http.StatusNotFound)
	defer srv.Close()

	c := clientWithServer(t, srv)
	_, err := c.FetchQuotas(context.Background())
	if !errors.Is(err, ErrCopilotNotFound) {
		t.Errorf("err = %v, want ErrCopilotNotFound", err)
	}
}

func TestFetchQuotas_ServerError(t *testing.T) {
	srv := statusServer(http.StatusInternalServerError)
	defer srv.Close()

	c := clientWithServer(t, srv)
	_, err := c.FetchQuotas(context.Background())
	if !errors.Is(err, ErrCopilotUnavailable) {
		t.Errorf("err = %v, want ErrCopilotUnavailable", err)
	}
}

func TestTokenDiscovery_GHExecFirst(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	called := false
	c, err := newCopilotClientWithExec(func(args ...string) ([]byte, error) {
		called = true
		return []byte("gh-token\n"), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("ghExec not called")
	}
	if c.token != "gh-token" {
		t.Errorf("token = %q, want %q", c.token, "gh-token")
	}
}

func TestTokenDiscovery_FallsBackToGITHUB_TOKEN(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")
	t.Setenv("GH_TOKEN", "")

	c, err := newCopilotClientWithExec(func(args ...string) ([]byte, error) {
		return nil, errors.New("gh not found")
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token != "env-token" {
		t.Errorf("token = %q, want env-token", c.token)
	}
}

func TestTokenDiscovery_FallsBackToGH_TOKEN(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "gh-env-token")

	c, err := newCopilotClientWithExec(func(args ...string) ([]byte, error) {
		return nil, errors.New("gh not found")
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token != "gh-env-token" {
		t.Errorf("token = %q, want gh-env-token", c.token)
	}
}

func TestTokenDiscovery_NoTokenError(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	_, err := newCopilotClientWithExec(func(args ...string) ([]byte, error) {
		return nil, errors.New("gh not found")
	})
	if err == nil {
		t.Error("expected error when no token available")
	}
}

func TestDisplayName(t *testing.T) {
	cases := []struct{ key, want string }{
		{"premium_interactions", "Premium Requests"},
		{"chat", "Chat"},
		{"completions", "Completions"},
		{"code_review", "Code Review"},
		{"unknown_key", "Unknown Key"},
	}
	for _, tc := range cases {
		if got := DisplayName(tc.key); got != tc.want {
			t.Errorf("DisplayName(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

// --- helpers ---

func jsonServer(t *testing.T, v interface{}, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
}

func statusServer(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
}

// clientWithServer builds a CopilotClient that hits srv instead of the real API.
func clientWithServer(t *testing.T, srv *httptest.Server) *CopilotClient {
	t.Helper()
	c, err := newCopilotClientWithExec(func(args ...string) ([]byte, error) {
		return []byte("test-token\n"), nil
	})
	if err != nil {
		t.Fatalf("building client: %v", err)
	}
	// Override HTTP client to target test server.
	c.httpClient = &http.Client{}
	c.baseURL = srv.URL + "/copilot_internal/user"
	return c
}

// rewriteTransport rewrites scheme and host while preserving path and query.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	// Parse target to extract scheme and host
	parsed, err := http.NewRequest(req.Method, rt.target, req.Body)
	if err != nil {
		return nil, err
	}
	// Rewrite only scheme and host; preserve path and query
	req.URL.Scheme = parsed.URL.Scheme
	req.URL.Host = parsed.URL.Host
	return rt.base.RoundTrip(req)
}
