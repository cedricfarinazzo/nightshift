package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

var httpClient = http.DefaultClient

// FetchAnthropicModels fetches available model IDs from the Anthropic API.
// Returns an error when the API is unreachable or the key is invalid; callers
// should fall back to a static list on error.
func FetchAnthropicModels(ctx context.Context, apiKey string) ([]string, error) {
	var ids []string
	afterID := ""

	for {
		baseURL := "https://api.anthropic.com/v1/models"
		fetchURL := baseURL
		if afterID != "" {
			q := url.Values{}
			q.Set("after_id", afterID)
			fetchURL = baseURL + "?" + q.Encode()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("X-Api-Key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch anthropic models: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("anthropic models API status %d", resp.StatusCode)
		}

		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("decode anthropic models: %w", err)
		}
		_ = resp.Body.Close()

		for _, m := range page.Data {
			if m.ID != "" {
				ids = append(ids, m.ID)
			}
		}

		if !page.HasMore {
			break
		}
		afterID = page.LastID
	}

	sort.Strings(ids)
	return ids, nil
}

// FetchOpenAIModels fetches available model IDs from the OpenAI API, filtered
// to gpt- and codex- prefixes. Returns an error on failure; callers should
// fall back to a static list.
func FetchOpenAIModels(ctx context.Context, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch openai models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai models API status %d", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode openai models: %w", err)
	}

	var ids []string
	for _, m := range body.Data {
		if strings.HasPrefix(m.ID, "gpt-") || strings.HasPrefix(m.ID, "codex-") {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// CopilotModels returns the hardcoded list of Copilot CLI model IDs.
// No programmatic API exists for Copilot model listing.
// Source: https://docs.github.com/en/copilot/reference/ai-models/supported-models (2026-05-10)
func CopilotModels() []string {
	return []string{
		"claude-haiku-4.5",
		"claude-opus-4.5",
		"claude-opus-4.6",
		"claude-opus-4.7",
		"claude-sonnet-4.5",
		"claude-sonnet-4.6",
		"gemini-2.5-pro",
		"gemini-3-flash",
		"gemini-3.1-pro",
		"gpt-4.1",
		"gpt-5-mini",
		"gpt-5.2",
		"gpt-5.2-codex",
		"gpt-5.3-codex",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.4-nano",
		"gpt-5.5",
		"grok-code-fast-1",
	}
}
