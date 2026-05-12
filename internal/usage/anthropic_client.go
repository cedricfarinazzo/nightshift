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
	versionedUserAgent      = "nightshift/0.3.4"
)

// Sentinel errors for HTTP status mapping.
var (
	ErrUnauthorized   = errors.New("anthropic: unauthorized (401)")
	ErrForbidden      = errors.New("anthropic: forbidden (403)")
	ErrRateLimited    = errors.New("anthropic: rate limited (429)")
	ErrServerError    = errors.New("anthropic: server error (5xx)")
	ErrBodyTruncation = errors.New("anthropic: response body truncated (exceeds limit)")
)

// AnthropicClient fetches usage quotas from the Anthropic API.
type AnthropicClient struct {
	baseURL    string
	httpClient *http.Client
	store      CredentialStore
	userAgent  string
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

// NewAnthropicClient creates a client with optional configuration.
func NewAnthropicClient(opts ...Option) *AnthropicClient {
	c := &AnthropicClient{
		baseURL:    defaultAnthropicBaseURL,
		httpClient: &http.Client{Timeout: requestTimeout},
		userAgent:  versionedUserAgent,
	}
	c.store = NewTokenDiscovery(&ExecKeychainRunner{})

	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FetchQuotas fetches current quota utilization from the Anthropic usage API.
func (c *AnthropicClient) FetchQuotas(ctx context.Context) (AnthropicQuotaResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	accessToken, err := c.store.ReadToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading token: %w", err)
	}

	return c.doRequest(ctx, accessToken)
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
			if t, err := time.Parse(time.RFC3339, entry.ResetsAt); err == nil {
				parsed.ResetsAt = t
			}
		}
		result[key] = parsed
	}

	return result, nil
}

func truncateBody(b []byte) string {
	if len(b) > 256 {
		return string(b[:256]) + "..."
	}
	return string(b)
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
