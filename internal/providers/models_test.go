package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchAnthropicModels_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") == "" {
			t.Error("missing X-Api-Key header")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":     []map[string]any{{"id": "claude-opus-4-7"}, {"id": "claude-sonnet-4-6"}},
			"has_more": false,
			"last_id":  "",
		})
	}))
	defer srv.Close()

	// Patch default client base URL by injecting a test transport.
	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: rewriteHostTransport{target: srv.URL, inner: http.DefaultTransport},
	}
	defer func() { http.DefaultClient = origClient }()

	ids, err := FetchAnthropicModels(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 models, got %d", len(ids))
	}
}

func TestFetchAnthropicModels_Paginated(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if page == 0 {
			page++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":     []map[string]any{{"id": "model-a"}},
				"has_more": true,
				"last_id":  "model-a",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":     []map[string]any{{"id": "model-b"}},
			"has_more": false,
			"last_id":  "",
		})
	}))
	defer srv.Close()

	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: rewriteHostTransport{target: srv.URL, inner: http.DefaultTransport},
	}
	defer func() { http.DefaultClient = origClient }()

	ids, err := FetchAnthropicModels(context.Background(), "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 paginated models, got %d", len(ids))
	}
}

func TestFetchAnthropicModels_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: rewriteHostTransport{target: srv.URL, inner: http.DefaultTransport},
	}
	defer func() { http.DefaultClient = origClient }()

	_, err := FetchAnthropicModels(context.Background(), "bad-key")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestFetchOpenAIModels_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4.1"},
				{"id": "gpt-5-mini"},
				{"id": "codex-mini"},
				{"id": "whisper-1"}, // should be filtered out
			},
		})
	}))
	defer srv.Close()

	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: rewriteHostTransport{target: srv.URL, inner: http.DefaultTransport},
	}
	defer func() { http.DefaultClient = origClient }()

	ids, err := FetchOpenAIModels(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("want 3 filtered models, got %d: %v", len(ids), ids)
	}
	for _, id := range ids {
		if id == "whisper-1" {
			t.Error("whisper-1 should have been filtered out")
		}
	}
}

func TestFetchOpenAIModels_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: rewriteHostTransport{target: srv.URL, inner: http.DefaultTransport},
	}
	defer func() { http.DefaultClient = origClient }()

	_, err := FetchOpenAIModels(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestCopilotModels(t *testing.T) {
	models := CopilotModels()
	if len(models) == 0 {
		t.Fatal("expected non-empty copilot model list")
	}
	// spot-check known models
	found := map[string]bool{}
	for _, m := range models {
		found[m] = true
	}
	for _, want := range []string{"claude-sonnet-4.6", "gpt-4.1", "gemini-2.5-pro"} {
		if !found[want] {
			t.Errorf("expected %q in copilot model list", want)
		}
	}
}

// rewriteHostTransport redirects all requests to the given target server URL.
type rewriteHostTransport struct {
	target string
	inner  http.RoundTripper
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = req.URL.Host // keep host for path matching but swap scheme+host
	// Replace host with test server
	u := *req.URL
	u.Scheme = "http"
	// extract host from target
	targetURL := t.target
	if len(targetURL) > 7 {
		u.Host = targetURL[7:] // strip "http://"
	}
	req.URL = &u
	return t.inner.RoundTrip(req)
}
