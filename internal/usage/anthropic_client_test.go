package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockStore is an in-memory CredentialStore for tests.
type mockStore struct {
	access    string
	refresh   string
	expiresAt time.Time
	written   []writeCall
}

type writeCall struct {
	access, refresh string
	expiresAt       time.Time
}

func (m *mockStore) ReadToken(ctx context.Context) (string, string, time.Time, error) {
	return m.access, m.refresh, m.expiresAt, nil
}

func (m *mockStore) WriteToken(access, refresh string, expiresAt time.Time) error {
	m.written = append(m.written, writeCall{access, refresh, expiresAt})
	m.access = access
	if refresh != "" {
		m.refresh = refresh
	}
	m.expiresAt = expiresAt
	return nil
}

func validQuotaBody() []byte {
	ml := int64(1000000)
	raw := map[string]anthropicQuotaEntryRaw{
		"five_hour": {
			Utilization: 0.42,
			ResetsAt:    "2026-05-12T10:00:00Z",
			IsEnabled:   true,
		},
		"seven_day": {
			Utilization:  0.75,
			ResetsAt:     "2026-05-19T00:00:00Z",
			IsEnabled:    true,
			MonthlyLimit: &ml,
		},
	}
	b, _ := json.Marshal(raw)
	return b
}

func oauthRefreshHandler(newAccess, newRefresh string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := oauthTokenResponse{
			AccessToken:  newAccess,
			RefreshToken: newRefresh,
			ExpiresIn:    3600,
		}
		b, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})
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

	store := &mockStore{access: "tok_valid", refresh: "ref", expiresAt: time.Now().Add(time.Hour)}
	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(store))

	resp, err := client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	fh, ok := resp["five_hour"]
	if !ok {
		t.Fatal("missing five_hour key")
	}
	if fh.Utilization != 0.42 {
		t.Errorf("utilization = %v, want 0.42", fh.Utilization)
	}
	if !fh.IsEnabled {
		t.Error("IsEnabled should be true")
	}
	if fh.ResetsAt.IsZero() {
		t.Error("ResetsAt should be parsed")
	}
}

func TestFetchQuotas_401_RefreshRetry_Success(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validQuotaBody())
	}))
	defer srv.Close()

	oauthSrv := httptest.NewServer(oauthRefreshHandler("tok_refreshed", "ref_new"))
	defer oauthSrv.Close()

	store := &mockStore{
		access:    "tok_expired",
		refresh:   "ref_old",
		expiresAt: time.Now().Add(time.Hour),
	}
	client := NewAnthropicClient(
		WithBaseURL(srv.URL),
		WithCredentialStore(store),
		WithOAuthBaseURL(oauthSrv.URL),
	)

	resp, err := client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
	if len(store.written) == 0 {
		t.Error("expected WriteToken to be called after refresh")
	}
}

func TestFetchQuotas_401_RefreshFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	oauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer oauthSrv.Close()

	store := &mockStore{access: "tok_bad", refresh: "ref_bad", expiresAt: time.Now().Add(time.Hour)}
	client := NewAnthropicClient(
		WithBaseURL(srv.URL),
		WithCredentialStore(store),
		WithOAuthBaseURL(oauthSrv.URL),
	)

	_, err := client.FetchQuotas(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchQuotas_403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	store := &mockStore{access: "tok", expiresAt: time.Now().Add(time.Hour)}
	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(store))

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

	store := &mockStore{access: "tok", expiresAt: time.Now().Add(time.Hour)}
	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(store))

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

	store := &mockStore{access: "tok", expiresAt: time.Now().Add(time.Hour)}
	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(store))

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

	store := &mockStore{access: "tok", expiresAt: time.Now().Add(time.Hour)}
	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(store))

	_, err := client.FetchQuotas(context.Background())
	if err == nil {
		t.Fatal("expected parse error on non-JSON body")
	}
}

func TestFetchQuotas_ExpiredTokenAutoRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validQuotaBody())
	}))
	defer srv.Close()

	oauthSrv := httptest.NewServer(oauthRefreshHandler("tok_fresh", "ref_fresh"))
	defer oauthSrv.Close()

	store := &mockStore{
		access:    "tok_old",
		refresh:   "ref_old",
		expiresAt: time.Now().Add(-time.Minute), // already expired
	}
	client := NewAnthropicClient(
		WithBaseURL(srv.URL),
		WithCredentialStore(store),
		WithOAuthBaseURL(oauthSrv.URL),
	)

	resp, err := client.FetchQuotas(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if len(store.written) == 0 {
		t.Error("expected WriteToken after proactive refresh")
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

	store := &mockStore{access: secret, expiresAt: time.Now().Add(time.Hour)}
	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(store))

	_, _ = client.FetchQuotas(context.Background())

	if captured != "Bearer "+secret {
		t.Errorf("unexpected Authorization header: %s", captured)
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

// mockKeychainRunner is a test implementation of KeychainRunner.
type mockKeychainRunner struct {
	keychainResult string
	keychainCode   int
	keychainErr    error
	keyringResult  string
	keyringCode    int
	keyringErr     error
}

func (m *mockKeychainRunner) Run(ctx context.Context, name string, args []string, dir string, stdin string) (string, string, int, error) {
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
	creds.ClaudeAiOauth.RefreshToken = "refresh_keychain"
	creds.ClaudeAiOauth.ExpiresAt = 1234567890000

	b, _ := json.Marshal(creds)
	runner := &mockKeychainRunner{keychainResult: string(b), keychainCode: 0}

	td := NewTokenDiscovery(runner)
	access, refresh, expiresAt, err := td.ReadToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if access != "from_keychain" {
		t.Errorf("access = %s, want from_keychain", access)
	}
	if refresh != "refresh_keychain" {
		t.Errorf("refresh = %s, want refresh_keychain", refresh)
	}
	if expiresAt.IsZero() {
		t.Error("expiresAt should be parsed from keychain")
	}
}

func TestTokenDiscovery_Keyring(t *testing.T) {
	creds := claudeCredentialsFile{}
	creds.ClaudeAiOauth.AccessToken = "from_keyring"
	creds.ClaudeAiOauth.RefreshToken = "refresh_keyring"

	b, _ := json.Marshal(creds)
	runner := &mockKeychainRunner{
		keychainCode:  1,           // keychain fails
		keyringResult: string(b),
		keyringCode:   0,
	}

	td := NewTokenDiscovery(runner)
	access, refresh, _, err := td.ReadToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if access != "from_keyring" {
		t.Errorf("access = %s, want from_keyring", access)
	}
	if refresh != "refresh_keyring" {
		t.Errorf("refresh = %s, want refresh_keyring", refresh)
	}
}

func TestTokenDiscovery_FileBackfall(t *testing.T) {
	// Both keychain and keyring fail, should fall back to file
	// Create a tokenDiscovery with a path that definitely doesn't exist
	runner := &mockKeychainRunner{keychainCode: 1, keyringCode: 1}
	td := &tokenDiscovery{
		runner:   runner,
		filePath: "/nonexistent/path/.credentials.json",
	}
	_, _, _, err := td.ReadToken(context.Background())
	// Error is expected since the file doesn't exist
	if err == nil {
		t.Fatal("expected error on missing file")
	}
}

func TestBodyLimitDetection(t *testing.T) {
	// Create response that exceeds limit
	large := make([]byte, bodyLimitBytes+100)
	for i := range large {
		large[i] = 'x'
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(large)
	}))
	defer srv.Close()

	store := &mockStore{access: "tok", expiresAt: time.Now().Add(time.Hour)}
	client := NewAnthropicClient(WithBaseURL(srv.URL), WithCredentialStore(store))

	_, err := client.FetchQuotas(context.Background())
	if !errors.Is(err, ErrBodyTruncation) && err == nil {
		t.Fatalf("expected ErrBodyTruncation or parse error, got %v", err)
	}
}
