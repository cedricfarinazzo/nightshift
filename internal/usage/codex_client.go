package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	codexPrimaryURL  = "https://chatgpt.com/backend-api/wham/usage"
	codexFallbackURL = "https://chatgpt.com/api/codex/usage"
	codexTimeout     = 10 * time.Second
)

// CodexClient fetches real-time rate limit usage from the Codex/ChatGPT API.
type CodexClient struct {
	httpClient  *http.Client
	primaryURL  string
	fallbackURL string
	creds       *CodexCredentials
	userAgent   string
	// oauthURL allows tests to override the token refresh endpoint.
	oauthURL string
}

// NewCodexClient creates a client by auto-discovering credentials.
// userAgent defaults to "nightshift/unknown" if empty; callers should inject actual version.
func NewCodexClient(userAgent string) (*CodexClient, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	return NewCodexClientWithCreds(creds, userAgent)
}

// NewCodexClientWithCreds creates a client with pre-loaded credentials (useful in tests).
// Returns error if creds is nil. userAgent defaults to "nightshift/unknown" if empty.
func NewCodexClientWithCreds(creds *CodexCredentials, userAgent string) (*CodexClient, error) {
	if creds == nil {
		return nil, fmt.Errorf("codex: credentials required")
	}
	if userAgent == "" {
		userAgent = "nightshift/unknown"
	}
	return &CodexClient{
		httpClient:  &http.Client{Timeout: codexTimeout},
		primaryURL:  codexPrimaryURL,
		fallbackURL: codexFallbackURL,
		creds:       creds,
		userAgent:   userAgent,
		oauthURL:    codexOAuthURL,
	}, nil
}

// FetchUsage retrieves current rate limit utilization from the Codex usage API.
// It auto-refreshes expired tokens and falls back to the alternate URL on 404.
func (c *CodexClient) FetchUsage(ctx context.Context) (*CodexUsageResponse, error) {
	if c.creds.IsExpired() {
		if err := refreshTokenWithURL(ctx, c.creds, c.oauthURL); err != nil {
			return nil, ErrTokenExpired
		}
	}

	resp, err := c.fetchURL(ctx, c.primaryURL)
	if err != nil {
		return nil, err
	}
	if resp != nil {
		return resp, nil
	}

	// Primary returned 404 — try fallback.
	resp, err = c.fetchURL(ctx, c.fallbackURL)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, ErrUsageNotFound
	}
	return resp, nil
}

// fetchURL performs a single GET. Returns (nil, nil) on 404 so the caller can try the fallback.
func (c *CodexClient) fetchURL(ctx context.Context, rawURL string) (*CodexUsageResponse, error) {
	result, err := c.doRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// doRequest sends GET rawURL with auth headers. On 401 it attempts one refresh and retries.
// Returns (nil, nil) on 404.
func (c *CodexClient) doRequest(ctx context.Context, rawURL string) (*CodexUsageResponse, error) {
	resp, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var out CodexUsageResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("codex: decode response: %w", err)
		}
		return &out, nil

	case http.StatusNotFound:
		return nil, nil // caller tries fallback

	case http.StatusUnauthorized:
		// Drain and close current body before retry.
		resp.Body.Close()
		if err := refreshTokenWithURL(ctx, c.creds, c.oauthURL); err != nil {
			return nil, ErrTokenExpired
		}
		retryResp, err := c.get(ctx, rawURL)
		if err != nil {
			return nil, err
		}
		defer retryResp.Body.Close()
		if retryResp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("codex: usage API status %d after token refresh", retryResp.StatusCode)
		}
		var out CodexUsageResponse
		if err := json.NewDecoder(retryResp.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("codex: decode response after refresh: %w", err)
		}
		return &out, nil

	default:
		return nil, fmt.Errorf("codex: usage API status %d", resp.StatusCode)
	}
}

func (c *CodexClient) get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("codex: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	req.Header.Set("User-Agent", c.userAgent)
	if c.creds.UserID != "" {
		req.Header.Set("X-Account-Id", c.creds.UserID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex: request failed: %w", err)
	}
	return resp, nil
}
