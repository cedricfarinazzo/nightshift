package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	anthropicUsagePath      = "/api/oauth/usage"
	anthropicBetaHeader     = "oauth-2025-04-20"
	bodyLimitBytes          = 64 * 1024 // 64KB
	requestTimeout          = 30 * time.Second
	// expiryBuffer prevents edge-race on near-expiry tokens.
	expiryBuffer = 30 * time.Second
	// versionedUserAgent is the default User-Agent header value.
	versionedUserAgent = "nightshift/0.3.4"
)

// Sentinel errors for HTTP status mapping.
var (
	ErrUnauthorized    = errors.New("anthropic: unauthorized (401)")
	ErrForbidden       = errors.New("anthropic: forbidden (403)")
	ErrRateLimited     = errors.New("anthropic: rate limited (429)")
	ErrServerError     = errors.New("anthropic: server error (5xx)")
	ErrBodyTooLarge    = errors.New("anthropic: response body exceeds 64KB limit")
	ErrBodyTruncation  = errors.New("anthropic: response body truncated (exceeds limit)")
)

// AnthropicClient fetches usage quotas from the Anthropic API.
type AnthropicClient struct {
	baseURL       string
	oauthBaseURL  string
	httpClient    *http.Client
	store         CredentialStore
	userAgent     string
}

// Option configures an AnthropicClient.
type Option func(*AnthropicClient)

// WithBaseURL overrides the API base URL (useful for tests).
func WithBaseURL(u string) Option {
	return func(c *AnthropicClient) {
		c.baseURL = u
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *AnthropicClient) {
		c.httpClient = hc
	}
}

// WithCredentialStore sets a custom credential store.
func WithCredentialStore(s CredentialStore) Option {
	return func(c *AnthropicClient) {
		c.store = s
	}
}

// WithUserAgent sets the User-Agent header value.
func WithUserAgent(ua string) Option {
	return func(c *AnthropicClient) {
		c.userAgent = ua
	}
}

// WithOAuthBaseURL overrides the OAuth token endpoint base URL (useful for tests).
func WithOAuthBaseURL(u string) Option {
	return func(c *AnthropicClient) {
		c.oauthBaseURL = u
	}
}

// NewAnthropicClient creates a client with optional configuration.
// By default it uses NewTokenDiscovery with an ExecKeychainRunner.
func NewAnthropicClient(opts ...Option) *AnthropicClient {
	c := &AnthropicClient{
		baseURL:      defaultAnthropicBaseURL,
		oauthBaseURL: "https://console.anthropic.com",
		httpClient:   &http.Client{Timeout: requestTimeout},
		userAgent:    versionedUserAgent,
	}
	c.store = NewTokenDiscovery(&ExecKeychainRunner{})

	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FetchQuotas fetches current quota utilization from the Anthropic usage API.
// It auto-discovers the OAuth token and refreshes it if expired.
func (c *AnthropicClient) FetchQuotas(ctx context.Context) (AnthropicQuotaResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	accessToken, err := c.resolveToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving token: %w", err)
	}

	resp, doErr := c.doRequest(ctx, accessToken)
	if doErr != nil {
		if errors.Is(doErr, ErrUnauthorized) {
			// One retry after forced refresh.
			accessToken, err = c.forceRefresh(ctx)
			if err != nil {
				return nil, fmt.Errorf("token refresh after 401: %w", err)
			}
			resp, doErr = c.doRequest(ctx, accessToken)
			if doErr != nil {
				return nil, doErr
			}
		} else {
			return nil, doErr
		}
	}

	return resp, nil
}

// resolveToken returns a valid access token, refreshing proactively if near expiry.
func (c *AnthropicClient) resolveToken(ctx context.Context) (string, error) {
	access, refresh, expiresAt, err := c.store.ReadToken(ctx)
	if err != nil {
		return "", fmt.Errorf("reading token: %w", err)
	}

	if !expiresAt.IsZero() && time.Now().Add(expiryBuffer).After(expiresAt) {
		if refresh != "" {
			newAccess, refreshErr := RefreshToken(ctx, c.httpClient, refresh, c.store, c.oauthBaseURL)
			if refreshErr == nil {
				return newAccess, nil
			}
			// Fall through and use existing token.
		}
	}

	return access, nil
}

// forceRefresh obtains a new access token using the stored refresh token.
func (c *AnthropicClient) forceRefresh(ctx context.Context) (string, error) {
	_, refresh, _, err := c.store.ReadToken(ctx)
	if err != nil {
		return "", fmt.Errorf("reading refresh token: %w", err)
	}
	if refresh == "" {
		return "", fmt.Errorf("no refresh token available")
	}
	return RefreshToken(ctx, c.httpClient, refresh, c.store, c.oauthBaseURL)
}

// doRequest performs the HTTP GET and parses the response.
func (c *AnthropicClient) doRequest(ctx context.Context, accessToken string) (AnthropicQuotaResponse, error) {
	reqURL := c.baseURL + anthropicUsagePath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	// Token is redacted from logs — never include in log lines.
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", anthropicBetaHeader)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// Check for body size before reading to detect truncation.
	lr := io.LimitReader(resp.Body, bodyLimitBytes+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	if len(body) > bodyLimitBytes {
		return nil, fmt.Errorf("%w: got %d bytes", ErrBodyTruncation, len(body))
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		return nil, ErrForbidden
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: %s", ErrServerError, truncateBody(body))
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncateBody(body))
	}

	return parseQuotaResponse(body)
}

// parseQuotaResponse unmarshals the API response into AnthropicQuotaResponse.
func parseQuotaResponse(body []byte) (AnthropicQuotaResponse, error) {
	var raw map[string]anthropicQuotaEntryRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing quota response: %w", err)
	}

	result := make(AnthropicQuotaResponse, len(raw))
	for key, entry := range raw {
		parsed := AnthropicQuotaEntry{
			Utilization:  entry.Utilization,
			IsEnabled:    entry.IsEnabled,
			MonthlyLimit: entry.MonthlyLimit,
			UsedCredits:  entry.UsedCredits,
			ResetsAtRaw:  entry.ResetsAt,
		}
		if entry.ResetsAt != "" {
			t, err := time.Parse(time.RFC3339, entry.ResetsAt)
			if err != nil {
				// Preserve raw value but don't fail; callers can inspect ResetsAtRaw.
			} else {
				parsed.ResetsAt = t
			}
		}
		result[key] = parsed
	}

	return result, nil
}

// ExecKeychainRunner executes keychain/keyring commands via os/exec.
type ExecKeychainRunner struct{}

// Run executes the given command and returns stdout/stderr/exitCode.
func (r *ExecKeychainRunner) Run(ctx context.Context, name string, args []string, _ string, _ string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	return stdout.String(), stderr.String(), exitCode, err
}
