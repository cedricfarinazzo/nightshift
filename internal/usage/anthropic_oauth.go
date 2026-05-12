package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	claudeOAuthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	defaultOAuthBaseURL = "https://console.anthropic.com"
)

// oauthBaseURL can be overridden in tests.
var oauthBaseURL = defaultOAuthBaseURL

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	TokenType    string `json:"token_type"`
}

// RefreshToken exchanges a refresh token for a new access token and writes it back to store.
func RefreshToken(ctx context.Context, httpClient *http.Client, refreshToken string, store CredentialStore) (string, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	endpoint := oauthBaseURL + "/v1/oauth/token"

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", claudeOAuthClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("building token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("reading token refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, truncateBody(body))
	}

	var tokenResp oauthTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing token refresh response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token refresh: empty access_token in response")
	}

	expiresAt := time.Time{}
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	newRefresh := tokenResp.RefreshToken
	if newRefresh == "" {
		newRefresh = refreshToken // keep existing if not rotated
	}

	if err := store.WriteToken(tokenResp.AccessToken, newRefresh, expiresAt); err != nil {
		// Log-worthy but non-fatal: we have the token, just couldn't persist it.
		_ = err
	}

	return tokenResp.AccessToken, nil
}

func truncateBody(b []byte) string {
	if len(b) > 256 {
		return string(b[:256]) + "..."
	}
	return string(b)
}
