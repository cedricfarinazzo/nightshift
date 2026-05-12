package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	codexOAuthURL  = "https://auth.openai.com/oauth/token"
	codexClientID  = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexOAuthScope = "openid profile email offline_access"
)

// oauthTokenResponse is the response from the token refresh endpoint.
type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// RefreshToken refreshes the OAuth access token using the stored refresh token.
// OpenAI uses one-time-use refresh tokens (rotation) — the new refresh token is
// written back to auth.json atomically before returning.
func RefreshToken(ctx context.Context, creds *CodexCredentials) error {
	return refreshTokenWithURL(ctx, creds, codexOAuthURL)
}

func refreshTokenWithURL(ctx context.Context, creds *CodexCredentials, oauthURL string) error {
	if creds.RefreshToken == "" {
		return fmt.Errorf("codex: no refresh token available")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", creds.RefreshToken)
	form.Set("client_id", codexClientID)
	form.Set("scope", codexOAuthScope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("codex: build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Use timeout-bounded client for refresh requests (avoid indefinite hang).
	httpClient := &http.Client{Timeout: codexTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("codex: refresh token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("codex: refresh token status %d", resp.StatusCode)
	}

	var tok oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("codex: decode refresh response: %w", err)
	}

	creds.AccessToken = tok.AccessToken
	creds.RefreshToken = tok.RefreshToken
	creds.IDToken = tok.IDToken
	if tok.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}

	if tok.IDToken != "" {
		if userID, exp, err := parseIDToken(tok.IDToken); err == nil {
			creds.UserID = userID
			if !exp.IsZero() {
				creds.ExpiresAt = exp
			}
		}
	}

	if creds.AuthFilePath != "" {
		if err := writeCredentials(creds.AuthFilePath, creds); err != nil {
			return fmt.Errorf("codex: failed to write rotated token to %s: %w (credentials in memory updated but not persisted; next refresh will fail)", creds.AuthFilePath, err)
		}
	}

	return nil
}

// writeCredentials atomically writes updated tokens to auth.json.
func writeCredentials(path string, creds *CodexCredentials) error {
	af := authFileShape{}
	af.Tokens.AccessToken = creds.AccessToken
	af.Tokens.RefreshToken = creds.RefreshToken
	af.Tokens.IDToken = creds.IDToken

	data, err := json.MarshalIndent(af, "", "  ")
	if err != nil {
		return fmt.Errorf("codex: marshal auth.json: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".auth_tmp_*")
	if err != nil {
		return fmt.Errorf("codex: create temp auth file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("codex: write temp auth file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("codex: close temp auth file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("codex: rename auth file: %w", err)
	}

	return nil
}
