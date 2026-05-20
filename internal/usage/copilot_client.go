package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/security"
)

// Version is injected by the caller at startup to populate User-Agent headers.
// Defaults to "unknown" if not set.
var Version = "unknown"

const (
	defaultCopilotAPIURL  = "https://api.github.com/copilot_internal/user"
	copilotTimeout        = 30 * time.Second
	tokenDiscoveryTimeout = 5 * time.Second
)

// Sentinel errors for Copilot API responses.
var (
	ErrCopilotUnauthorized = errors.New("copilot: unauthorized (invalid or missing token)")
	ErrCopilotNotFound     = errors.New("copilot: resource not found")
	ErrCopilotUnavailable  = errors.New("copilot: service unavailable")
)

// CopilotClient fetches Copilot quota data from the GitHub internal API.
type CopilotClient struct {
	token      string
	httpClient *http.Client
	baseURL    string
	userAgent  string
	// ghExec runs gh with the given args and returns stdout. Injectable for tests.
	ghExec func(args ...string) ([]byte, error)
}

// NewCopilotClient constructs a CopilotClient, discovering the GitHub token via:
//  1. gh auth token
//  2. $GITHUB_TOKEN
//  3. $GH_TOKEN
func NewCopilotClient() (*CopilotClient, error) {
	c := &CopilotClient{
		httpClient: &http.Client{Timeout: copilotTimeout},
		baseURL:    defaultCopilotAPIURL,
		userAgent:  "nightshift/" + Version,
		ghExec:     defaultGHExec,
	}
	token, err := c.discoverToken(context.Background())
	if err != nil {
		return nil, err
	}
	c.token = token
	return c, nil
}

// newCopilotClientWithExec is used in tests to inject a fake ghExec and custom baseURL.
func newCopilotClientWithExec(ghExec func(args ...string) ([]byte, error)) (*CopilotClient, error) {
	c := &CopilotClient{
		httpClient: &http.Client{Timeout: copilotTimeout},
		baseURL:    defaultCopilotAPIURL,
		userAgent:  "nightshift/" + Version,
		ghExec:     ghExec,
	}
	token, err := c.discoverToken(context.Background())
	if err != nil {
		return nil, err
	}
	c.token = token
	return c, nil
}

// CopilotOption configures a CopilotClient.
type CopilotOption func(*CopilotClient)

// WithCopilotGHExec overrides the gh executor (useful for tests).
func WithCopilotGHExec(fn func(args ...string) ([]byte, error)) CopilotOption {
	return func(c *CopilotClient) { c.ghExec = fn }
}

// NewCopilotClientWithBaseURL constructs a client with a custom API base URL (useful for GitHub Enterprise or tests).
func NewCopilotClientWithBaseURL(baseURL string, opts ...CopilotOption) (*CopilotClient, error) {
	c := &CopilotClient{
		httpClient: &http.Client{Timeout: copilotTimeout},
		baseURL:    baseURL,
		userAgent:  "nightshift/" + Version,
		ghExec:     defaultGHExec,
	}
	for _, opt := range opts {
		opt(c)
	}
	token, err := c.discoverToken(context.Background())
	if err != nil {
		return nil, err
	}
	c.token = token
	return c, nil
}

func defaultGHExec(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tokenDiscoveryTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "gh", args...).Output()
}

func (c *CopilotClient) discoverToken(ctx context.Context) (string, error) {
	// 1. gh auth token (defaultGHExec handles timeout internally)
	out, err := c.ghExec("auth", "token")
	if err == nil {
		if tok := strings.TrimSpace(string(out)); tok != "" {
			return tok, nil
		}
	}

	// 2. $GITHUB_TOKEN with fallback to $GH_TOKEN
	if tok := security.GetGitHubToken(); tok != "" {
		return tok, nil
	}

	return "", fmt.Errorf("copilot: no GitHub token found (tried gh auth token, $GITHUB_TOKEN, $GH_TOKEN)")
}

// FetchQuotas calls the Copilot internal user API and returns normalized quota data.
func (c *CopilotClient) FetchQuotas(ctx context.Context) (*CopilotUserResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("copilot: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, ErrCopilotUnauthorized
	case http.StatusNotFound:
		return nil, ErrCopilotNotFound
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrCopilotUnavailable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("copilot: unexpected status %d: %s", resp.StatusCode, body)
	}

	var raw copilotAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("copilot: decoding response: %w", err)
	}

	return normalize(raw), nil
}
