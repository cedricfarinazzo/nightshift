package usage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeJWT builds a minimal unsigned JWT with the given claims.
func makeJWT(claims map[string]interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	body := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + body + ".fakesig"
}

func sampleUsageJSON(creditsBalance interface{}) string {
	b, _ := json.Marshal(map[string]interface{}{
		"plan_type": "pro",
		"rate_limit": map[string]interface{}{
			"primary_window": map[string]interface{}{
				"used_percent":          42.5,
				"reset_at":              time.Now().Add(time.Hour).Format(time.RFC3339),
				"limit_window_seconds":  18000,
			},
			"secondary_window": map[string]interface{}{
				"used_percent":          10.0,
				"reset_at":              time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339),
				"limit_window_seconds":  604800,
			},
		},
		"credits": map[string]interface{}{
			"balance": creditsBalance,
		},
	})
	return string(b)
}

func newClient(primary, fallback, oauth string, creds *CodexCredentials) *CodexClient {
	c := NewCodexClientWithCreds(creds)
	c.primaryURL = primary
	c.fallbackURL = fallback
	c.oauthURL = oauth
	return c
}

// ---- FetchUsage tests ----

func TestFetchUsage_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(sampleUsageJSON(1234.56)))
	}))
	defer srv.Close()

	creds := &CodexCredentials{AccessToken: "tok"}
	c := newClient(srv.URL, srv.URL+"/fallback", "", creds)

	resp, err := c.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.PlanType != "pro" {
		t.Errorf("plan_type = %q, want pro", resp.PlanType)
	}
	if resp.RateLimit == nil || resp.RateLimit.PrimaryWindow == nil {
		t.Fatal("expected primary window")
	}
	if resp.RateLimit.PrimaryWindow.UsedPercent != 42.5 {
		t.Errorf("used_percent = %v, want 42.5", resp.RateLimit.PrimaryWindow.UsedPercent)
	}
	if resp.Credits == nil {
		t.Fatal("expected credits")
	}
	if resp.Credits.Balance != 1234.56 {
		t.Errorf("credits.balance = %v, want 1234.56", resp.Credits.Balance)
	}
}

func TestFetchUsage_fallback404(t *testing.T) {
	fallbackCalled := false
	fallbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(sampleUsageJSON(0)))
	}))
	defer fallbackSrv.Close()

	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer primarySrv.Close()

	creds := &CodexCredentials{AccessToken: "tok"}
	c := newClient(primarySrv.URL, fallbackSrv.URL, "", creds)

	resp, err := c.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fallbackCalled {
		t.Error("fallback URL not called")
	}
	if resp == nil {
		t.Fatal("expected response from fallback")
	}
}

func TestFetchUsage_fallbackAlsoFails(t *testing.T) {
	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv404.Close()

	creds := &CodexCredentials{AccessToken: "tok"}
	c := newClient(srv404.URL, srv404.URL, "", creds)

	_, err := c.FetchUsage(context.Background())
	if err != ErrUsageNotFound {
		t.Errorf("want ErrUsageNotFound, got %v", err)
	}
}

func TestFetchUsage_tokenRefreshOn401(t *testing.T) {
	refreshed := false
	oauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshed = true
		json.NewEncoder(w).Encode(oauthTokenResponse{
			AccessToken:  "newtoken",
			RefreshToken: "newrefresh",
			ExpiresIn:    3600,
		})
	}))
	defer oauthSrv.Close()

	calls := 0
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(sampleUsageJSON(0)))
	}))
	defer apiSrv.Close()

	creds := &CodexCredentials{AccessToken: "oldtok", RefreshToken: "oldrefresh"}
	c := newClient(apiSrv.URL, apiSrv.URL+"/fallback", oauthSrv.URL, creds)

	resp, err := c.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !refreshed {
		t.Error("token refresh not called")
	}
	if resp == nil {
		t.Fatal("expected response after refresh")
	}
}

func TestFetchUsage_creditsStringFloat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// balance as quoted string
		w.Write([]byte(sampleUsageJSON("999.99")))
	}))
	defer srv.Close()

	creds := &CodexCredentials{AccessToken: "tok"}
	c := newClient(srv.URL, srv.URL, "", creds)

	resp, err := c.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Credits == nil {
		t.Fatal("expected credits")
	}
	if resp.Credits.Balance != 999.99 {
		t.Errorf("balance = %v, want 999.99", resp.Credits.Balance)
	}
}

func TestFetchUsage_creditsNumericFloat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(sampleUsageJSON(500.0)))
	}))
	defer srv.Close()

	creds := &CodexCredentials{AccessToken: "tok"}
	c := newClient(srv.URL, srv.URL, "", creds)

	resp, err := c.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Credits.Balance != 500.0 {
		t.Errorf("balance = %v, want 500.0", resp.Credits.Balance)
	}
}

// ---- Credential tests ----

func TestLoadCredentials_envVar(t *testing.T) {
	t.Setenv("CODEX_TOKEN", "env-token")
	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.AccessToken != "env-token" {
		t.Errorf("access_token = %q, want env-token", creds.AccessToken)
	}
}

func TestLoadCredentials_authJson(t *testing.T) {
	dir := t.TempDir()
	exp := time.Now().Add(time.Hour).Unix()
	jwt := makeJWT(map[string]interface{}{
		"exp":             exp,
		"chatgpt_user_id": "usr_abc123",
	})

	af := authFileShape{}
	af.Tokens.AccessToken = "file-token"
	af.Tokens.RefreshToken = "file-refresh"
	af.Tokens.IDToken = jwt
	data, _ := json.Marshal(af)
	os.WriteFile(filepath.Join(dir, "auth.json"), data, 0600)

	t.Setenv("CODEX_HOME", dir)
	t.Setenv("CODEX_TOKEN", "") // ensure env var path not taken

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.AccessToken != "file-token" {
		t.Errorf("access_token = %q", creds.AccessToken)
	}
	if creds.UserID != "usr_abc123" {
		t.Errorf("user_id = %q, want usr_abc123", creds.UserID)
	}
	if creds.ExpiresAt.Unix() != exp {
		t.Errorf("expires_at = %v, want %v", creds.ExpiresAt.Unix(), exp)
	}
}

func TestLoadCredentials_notFound(t *testing.T) {
	t.Setenv("CODEX_TOKEN", "")
	t.Setenv("CODEX_HOME", "/nonexistent/path/that/cannot/exist/xyz")

	_, err := LoadCredentials()
	if err != ErrNoCredentials {
		t.Errorf("want ErrNoCredentials, got %v", err)
	}
}

// ---- Refresh token tests ----

func TestRefreshToken_rotation(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")

	initial := authFileShape{}
	initial.Tokens.AccessToken = "old-access"
	initial.Tokens.RefreshToken = "old-refresh"
	data, _ := json.Marshal(initial)
	os.WriteFile(authPath, data, 0600)

	oauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.FormValue("refresh_token") != "old-refresh" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(oauthTokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		})
	}))
	defer oauthSrv.Close()

	creds := &CodexCredentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		AuthFilePath: authPath,
	}

	if err := refreshTokenWithURL(context.Background(), creds, oauthSrv.URL); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	if creds.AccessToken != "new-access" {
		t.Errorf("in-memory access_token = %q, want new-access", creds.AccessToken)
	}
	if creds.RefreshToken != "new-refresh" {
		t.Errorf("in-memory refresh_token = %q, want new-refresh", creds.RefreshToken)
	}

	// Verify file written.
	written, _ := os.ReadFile(authPath)
	var af authFileShape
	json.Unmarshal(written, &af)
	if af.Tokens.AccessToken != "new-access" {
		t.Errorf("file access_token = %q, want new-access", af.Tokens.AccessToken)
	}
	if af.Tokens.RefreshToken != "new-refresh" {
		t.Errorf("file refresh_token = %q, want new-refresh", af.Tokens.RefreshToken)
	}
}

// ---- JWT parsing tests ----

func TestJWTParsing_expiry(t *testing.T) {
	exp := time.Now().Add(2 * time.Hour).Unix()
	jwt := makeJWT(map[string]interface{}{
		"exp":             exp,
		"chatgpt_user_id": "user_xyz",
	})

	userID, expTime, err := parseIDToken(jwt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "user_xyz" {
		t.Errorf("userID = %q, want user_xyz", userID)
	}
	if expTime.Unix() != exp {
		t.Errorf("exp = %v, want %v", expTime.Unix(), exp)
	}
}

func TestJWTParsing_subFallback(t *testing.T) {
	jwt := makeJWT(map[string]interface{}{
		"sub": "sub_fallback",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	userID, _, err := parseIDToken(jwt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "sub_fallback" {
		t.Errorf("userID = %q, want sub_fallback", userID)
	}
}
