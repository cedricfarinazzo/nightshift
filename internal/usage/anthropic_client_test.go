package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockStore is an in-memory CredentialStore for tests.
type mockStore struct {
	access string
	err    error
}

func (m *mockStore) ReadToken(_ context.Context) (string, error) {
	return m.access, m.err
}

func validQuotaBody() []byte {
	ml := int64(1000000)
	raw := map[string]anthropicQuotaEntryRaw{
		"five_hour": {
			Utilization: 42.0,
			ResetsAt:    "2026-05-12T10:00:00Z",
			IsEnabled:   true,
		},
		"seven_day": {
			Utilization:  75.0,
			ResetsAt:     "2026-05-19T00:00:00Z",
			IsEnabled:    true,
			MonthlyLimit: &ml,
		},
	}
	b, _ := json.Marshal(raw)
	return b
}

func TestFetchQuotas_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		if r.Header.Get("anthropic-beta") != anthropicBetaHeader {
			t.Errorf("wrong beta header: %s", r.Header.Get("anthropic-beta"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validQuotaBody())
	}))
	defer srv.Close()

	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(&mockStore{access: "tok_valid"}))

	resp, err := client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fh, ok := resp["five_hour"]
	if !ok {
		t.Fatal("missing five_hour key")
	}
	if fh.Utilization != 0.42 {
		t.Errorf("utilization = %v, want 0.42 (42.0/100)", fh.Utilization)
	}
	if !fh.IsEnabled {
		t.Error("IsEnabled should be true")
	}
	if fh.ResetsAt.IsZero() {
		t.Error("ResetsAt should be parsed")
	}
}

func TestFetchQuotas_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(&mockStore{access: "tok_bad"}))

	_, err := client.FetchQuotas(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestFetchQuotas_403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(&mockStore{access: "tok"}))

	_, err := client.FetchQuotas(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestFetchQuotas_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(&mockStore{access: "tok"}))

	_, err := client.FetchQuotas(context.Background())
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestFetchQuotas_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(&mockStore{access: "tok"}))

	_, err := client.FetchQuotas(context.Background())
	if !errors.Is(err, ErrServerError) {
		t.Errorf("expected ErrServerError, got %v", err)
	}
}

func TestFetchQuotas_BodyTruncation(t *testing.T) {
	large := make([]byte, bodyLimitBytes+1024)
	for i := range large {
		large[i] = 'x'
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(large)
	}))
	defer srv.Close()

	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(&mockStore{access: "tok"}))

	_, err := client.FetchQuotas(context.Background())
	if !errors.Is(err, ErrBodyTruncation) {
		t.Errorf("expected ErrBodyTruncation, got %v", err)
	}
}

func TestFetchQuotas_TokenSentToServer(t *testing.T) {
	const secret = "super_secret_token_xyz"
	captured := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validQuotaBody())
	}))
	defer srv.Close()

	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(&mockStore{access: secret}))
	_, _ = client.FetchQuotas(context.Background())

	if captured != "Bearer "+secret {
		t.Errorf("unexpected Authorization header: %s", captured)
	}
}

func TestFetchQuotas_StoreError(t *testing.T) {
	client := NewAnthropicClient(WithCredentialStore(&mockStore{err: fmt.Errorf("no credentials")}))

	_, err := client.FetchQuotas(context.Background())
	if err == nil {
		t.Fatal("expected error when store fails")
	}
}

func TestParseQuotaResponse_ResetsAt(t *testing.T) {
	raw := map[string]anthropicQuotaEntryRaw{
		"five_hour": {
			Utilization: 0.1,
			ResetsAt:    "2026-05-12T15:30:00Z",
			IsEnabled:   true,
		},
	}
	b, _ := json.Marshal(raw)
	resp, err := parseQuotaResponse(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry := resp["five_hour"]
	want := time.Date(2026, 5, 12, 15, 30, 0, 0, time.UTC)
	if !entry.ResetsAt.Equal(want) {
		t.Errorf("ResetsAt = %v, want %v", entry.ResetsAt, want)
	}
}

func TestParseQuotaResponse_InvalidJSON(t *testing.T) {
	_, err := parseQuotaResponse([]byte("not json"))
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestCache_DefaultTTL(t *testing.T) {
	c := NewAnthropicClient()
	if c.cacheTTL != defaultCacheTTL {
		t.Errorf("cacheTTL = %v, want %v", c.cacheTTL, defaultCacheTTL)
	}
}

func TestWithCacheTTL(t *testing.T) {
	c := NewAnthropicClient(WithCacheTTL(10 * time.Minute))
	if c.cacheTTL != 10*time.Minute {
		t.Errorf("cacheTTL = %v, want 10m", c.cacheTTL)
	}
}

func TestCache_Hit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validQuotaBody())
	}))
	defer srv.Close()

	client := NewAnthropicClient(
		WithBaseURL(srv.URL),
		WithCredentialStore(&mockStore{access: "tok_cache"}),
		WithCacheTTL(5*time.Minute),
	)

	_, err := client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err = client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("server called %d times, want 1", calls.Load())
	}
}

func TestCache_Miss_AfterTTL(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validQuotaBody())
	}))
	defer srv.Close()

	now := time.Now()
	client := NewAnthropicClient(
		WithBaseURL(srv.URL),
		WithCredentialStore(&mockStore{access: "tok_ttl"}),
		WithCacheTTL(1*time.Minute),
		withClockFn(func() time.Time { return now }),
	)

	_, _ = client.FetchQuotas(context.Background())

	// Advance clock past TTL.
	now = now.Add(2 * time.Minute)

	_, err := client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("server called %d times, want 2", calls.Load())
	}
}

func TestCache_429_FallbackToCached(t *testing.T) {
	serve429 := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serve429 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validQuotaBody())
	}))
	defer srv.Close()

	client := NewAnthropicClient(
		WithBaseURL(srv.URL),
		WithCredentialStore(&mockStore{access: "tok_429"}),
		WithCacheTTL(5*time.Minute),
	)

	// Prime cache.
	resp, err := client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Now 429.
	serve429 = true
	resp2, err := client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("429 should return cached, got error: %v", err)
	}
	if len(resp2) != len(resp) {
		t.Errorf("cached response length mismatch")
	}
}

func TestCache_429_NoCacheEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := NewAnthropicClient(
		WithBaseURL(srv.URL),
		WithCredentialStore(&mockStore{access: "tok_429_empty"}),
	)

	_, err := client.FetchQuotas(context.Background())
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestCache_Invalidate(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validQuotaBody())
	}))
	defer srv.Close()

	client := NewAnthropicClient(
		WithBaseURL(srv.URL),
		WithCredentialStore(&mockStore{access: "tok_inv"}),
		WithCacheTTL(5*time.Minute),
	)

	_, _ = client.FetchQuotas(context.Background())

	if err := client.Invalidate(context.Background()); err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	_, err := client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("after invalidate: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("server called %d times, want 2", calls.Load())
	}
}

func TestCache_Concurrent(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validQuotaBody())
	}))
	defer srv.Close()

	client := NewAnthropicClient(
		WithBaseURL(srv.URL),
		WithCredentialStore(&mockStore{access: "tok_concurrent"}),
		WithCacheTTL(5*time.Minute),
	)

	var wg sync.WaitGroup
	const goroutines = 20
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = client.FetchQuotas(context.Background())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	// Verify that concurrent calls to cold cache are deduplicated via singleflight.
	// All 20 goroutines should result in exactly 1 HTTP request, not 20.
	if calls.Load() != 1 {
		t.Errorf("server called %d times, want 1 (singleflight deduplication)", calls.Load())
	}
}

// mockKeychainRunner is a test implementation of KeychainRunner.
type mockKeychainRunner struct {
	keychainResult string
	keychainCode   int
	keychainErr    error
	keyringResult  string
	keyringCode    int
	keyringErr     error
}

func (m *mockKeychainRunner) Run(_ context.Context, name string, _ []string, _ string, _ string) (string, string, int, error) {
	if name == "security" {
		return m.keychainResult, "", m.keychainCode, m.keychainErr
	}
	if name == "secret-tool" {
		return m.keyringResult, "", m.keyringCode, m.keyringErr
	}
	return "", "", 1, fmt.Errorf("unknown command: %s", name)
}

func TestTokenDiscovery_Keychain(t *testing.T) {
	creds := claudeCredentialsFile{}
	creds.ClaudeAiOauth.AccessToken = "from_keychain"

	b, _ := json.Marshal(creds)
	runner := &mockKeychainRunner{keychainResult: string(b), keychainCode: 0}

	td := NewTokenDiscovery(runner)
	access, err := td.ReadToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if access != "from_keychain" {
		t.Errorf("access = %s, want from_keychain", access)
	}
}

func TestTokenDiscovery_Keyring(t *testing.T) {
	creds := claudeCredentialsFile{}
	creds.ClaudeAiOauth.AccessToken = "from_keyring"

	b, _ := json.Marshal(creds)
	runner := &mockKeychainRunner{
		keychainCode:  1,
		keyringResult: string(b),
		keyringCode:   0,
	}

	td := NewTokenDiscovery(runner)
	access, err := td.ReadToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if access != "from_keyring" {
		t.Errorf("access = %s, want from_keyring", access)
	}
}

func TestTokenDiscovery_FileFallback(t *testing.T) {
	runner := &mockKeychainRunner{keychainCode: 1, keyringCode: 1}
	td := &tokenDiscovery{
		runner:   runner,
		filePath: "/nonexistent/path/.credentials.json",
	}
	_, err := td.ReadToken(context.Background())
	if err == nil {
		t.Fatal("expected error on missing file")
	}
}
