package budget

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/marcus/nightshift/internal/config"

	"github.com/marcus/nightshift/internal/usage"
	"github.com/rs/zerolog/log"
)

// Quota represents utilization for a single time window.
type Quota struct {
	Window      string
	Utilization float64 // 0.0–1.0
	ResetsAt    time.Time
}

// ProviderUsage is unified usage data from any provider.
type ProviderUsage struct {
	Provider  string
	Quotas    []Quota
	Credits   *float64
	ResetTime *time.Time
	Source    string // "api", "none"
	FetchedAt time.Time
}

// anthropicActiveProvider fetches usage from the Anthropic API.
type anthropicActiveProvider struct {
	client *usage.AnthropicClient
	mu     sync.Mutex
	src    string
}

func (p *anthropicActiveProvider) Name() string { return "claude" }

func (p *anthropicActiveProvider) GetUsedPercent(mode string) (float64, error) {
	if p.client == nil {
		p.setSource("none")
		return 0, fmt.Errorf("anthropic client not initialized")
	}
	resp, err := p.client.FetchQuotas(context.Background())
	if err != nil {
		log.Warn().Err(err).Str("provider", "claude").Msg("budget: api fetch failed")
		p.setSource("none")
		return 0, fmt.Errorf("anthropic api fetch failed: %w", err)
	}
	if entry, ok := resp["seven_day"]; ok {
		pct := entry.Utilization * 100
		p.setSource("api")
		log.Debug().Str("provider", "claude").Str("source", "api").Float64("used_pct", pct).Msg("budget: usage from api")
		return pct, nil
	}
	p.setSource("none")
	return 0, fmt.Errorf("anthropic: seven_day quota not found in response")
}

func (p *anthropicActiveProvider) LastUsedPercentSource() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.src
}

func (p *anthropicActiveProvider) setSource(s string) {
	p.mu.Lock()
	p.src = s
	p.mu.Unlock()
}

// codexActiveProvider fetches usage from the Codex/OpenAI API.
type codexActiveProvider struct {
	client *usage.CodexClient
	mu     sync.Mutex
	src    string
	// Cache last FetchUsage response to avoid duplicate API calls
	// when both GetUsedPercent and GetResetTime are called for the same check.
	cachedResp *usage.CodexUsageResponse
	cachedTime time.Time
	cacheTTL   time.Duration // default 1 minute
}

func (p *codexActiveProvider) Name() string { return "codex" }

func (p *codexActiveProvider) GetUsedPercent(mode string) (float64, error) {
	if p.client == nil {
		p.setSource("none")
		return 0, fmt.Errorf("codex client not initialized")
	}
	resp, err := p.client.FetchUsage(context.Background())
	if err != nil {
		log.Warn().Err(err).Str("provider", "codex").Msg("budget: api fetch failed")
		p.setSource("none")
		return 0, fmt.Errorf("codex api fetch failed: %w", err)
	}
	if resp == nil || resp.RateLimit == nil {
		p.setSource("none")
		log.Warn().Str("provider", "codex").Msg("budget: no rate limit data in response, assuming 0%")
		return 0, nil
	}
	// Cache response for reuse by GetResetTime (1-minute TTL).
	p.mu.Lock()
	p.cachedResp = resp
	p.cachedTime = time.Now()
	if p.cacheTTL == 0 {
		p.cacheTTL = 1 * time.Minute
	}
	p.mu.Unlock()
	// Prefer secondary (weekly) window; fall back to primary (5h) window.
	var pct float64
	if resp.RateLimit.SecondaryWindow != nil {
		pct = resp.RateLimit.SecondaryWindow.UsedPercent
	} else if resp.RateLimit.PrimaryWindow != nil {
		pct = resp.RateLimit.PrimaryWindow.UsedPercent
		log.Debug().Str("provider", "codex").Msg("budget: no secondary window, using primary")
	} else {
		p.setSource("none")
		log.Warn().Str("provider", "codex").Msg("budget: no rate limit windows in response, assuming 0%")
		return 0, nil
	}
	p.setSource("api")
	log.Debug().Str("provider", "codex").Str("source", "api").Float64("used_pct", pct).Msg("budget: usage from api")
	return pct, nil
}

func (p *codexActiveProvider) GetResetTime(mode string) (time.Time, error) {
	// Check cache first to avoid duplicate API calls.
	p.mu.Lock()
	if p.cachedResp != nil && time.Since(p.cachedTime) < p.cacheTTL {
		defer p.mu.Unlock()
		if p.cachedResp.RateLimit != nil && p.cachedResp.RateLimit.SecondaryWindow != nil {
			return p.cachedResp.RateLimit.SecondaryWindow.ResetAt.Time, nil
		}
	}
	p.mu.Unlock()

	if p.client != nil {
		resp, err := p.client.FetchUsage(context.Background())
		if err == nil && resp != nil && resp.RateLimit != nil && resp.RateLimit.SecondaryWindow != nil {
			return resp.RateLimit.SecondaryWindow.ResetAt.Time, nil
		}
	}
	return time.Time{}, nil
}

func (p *codexActiveProvider) LastUsedPercentSource() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.src
}

func (p *codexActiveProvider) setSource(s string) {
	p.mu.Lock()
	p.src = s
	p.mu.Unlock()
}

// copilotActiveProvider fetches usage from the GitHub Copilot API.
type copilotActiveProvider struct {
	client *usage.CopilotClient
	mu     sync.Mutex
	src    string
	// Cache last FetchQuotas response to avoid duplicate API calls
	// when both GetUsedPercent and GetResetTime are called for the same check.
	cachedResp *usage.CopilotUserResponse
	cachedTime time.Time
	cacheTTL   time.Duration // default 1 minute
}

func (p *copilotActiveProvider) Name() string { return "copilot" }

func (p *copilotActiveProvider) GetUsedPercent(mode string) (float64, error) {
	if p.client == nil {
		p.setSource("none")
		return 0, fmt.Errorf("copilot client not initialized")
	}
	resp, err := p.client.FetchQuotas(context.Background())
	if err != nil {
		log.Warn().Err(err).Str("provider", "copilot").Msg("budget: api fetch failed")
		p.setSource("none")
		return 0, fmt.Errorf("copilot api fetch failed: %w", err)
	}
	if resp == nil {
		p.setSource("none")
		return 0, fmt.Errorf("copilot: nil response from api")
	}
	// Cache response for reuse by GetResetTime (1-minute TTL).
	p.mu.Lock()
	p.cachedResp = resp
	p.cachedTime = time.Now()
	if p.cacheTTL == 0 {
		p.cacheTTL = 1 * time.Minute
	}
	p.mu.Unlock()

	if snap, ok := resp.Quotas["premium_interactions"]; ok {
		// PercentRemaining is 0–100; convert to used %.
		pct := 100 - snap.PercentRemaining
		if snap.Unlimited {
			pct = 0
		}
		p.setSource("api")
		log.Debug().Str("provider", "copilot").Str("source", "api").Float64("used_pct", pct).Msg("budget: usage from api")
		return pct, nil
	}
	p.setSource("none")
	return 0, fmt.Errorf("copilot: premium_interactions quota not found")
}

func (p *copilotActiveProvider) GetResetTime(mode string) (time.Time, error) {
	// Check cache first to avoid duplicate API calls.
	p.mu.Lock()
	if p.cachedResp != nil && time.Since(p.cachedTime) < p.cacheTTL {
		defer p.mu.Unlock()
		if p.cachedResp.QuotaResetDate != "" {
			t, parseErr := time.Parse("2006-01-02", p.cachedResp.QuotaResetDate)
			if parseErr == nil {
				return t, nil
			}
			log.Warn().Str("raw", p.cachedResp.QuotaResetDate).Err(parseErr).Msg("budget: copilot reset date parse failed")
		}
	}
	p.mu.Unlock()

	if p.client != nil {
		resp, err := p.client.FetchQuotas(context.Background())
		if err == nil && resp != nil && resp.QuotaResetDate != "" {
			t, parseErr := time.Parse("2006-01-02", resp.QuotaResetDate)
			if parseErr == nil {
				return t, nil
			}
			log.Warn().Str("raw", resp.QuotaResetDate).Err(parseErr).Msg("budget: copilot reset date parse failed")
		}
	}
	return time.Time{}, nil
}

func (p *copilotActiveProvider) LastUsedPercentSource() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.src
}

func (p *copilotActiveProvider) setSource(s string) {
	p.mu.Lock()
	p.src = s
	p.mu.Unlock()
}

// NewManagerWithTracking builds a Manager using active (API-based) tracking.
// If an API client fails to initialize, that provider returns 0% usage.
func NewManagerWithTracking(cfg *config.Config, opts ...Option) *Manager {
	claudeP := &anthropicActiveProvider{client: usage.NewAnthropicClient()}

	var codexP CodexUsageProvider = &codexActiveProvider{}
	codexClient, err := usage.NewCodexClient("")
	if err != nil {
		log.Warn().Err(err).Msg("budget: codex api client unavailable")
	} else {
		codexP = &codexActiveProvider{client: codexClient}
	}

	var copilotP CopilotUsageProvider = &copilotActiveProvider{}
	copilotClient, err := usage.NewCopilotClient()
	if err != nil {
		log.Warn().Err(err).Msg("budget: copilot api client unavailable")
	} else {
		copilotP = &copilotActiveProvider{client: copilotClient}
	}

	return NewManager(cfg, claudeP, codexP, copilotP, opts...)
}

// FetchProviderUsage fetches unified usage data for a named provider using API clients.
// This is a best-effort convenience method; returns zero ProviderUsage on any error.
func FetchProviderUsage(ctx context.Context, provider string) ProviderUsage {
	now := time.Now()
	switch provider {
	case "claude":
		client := usage.NewAnthropicClient()
		resp, err := client.FetchQuotas(ctx)
		if err != nil {
			return ProviderUsage{Provider: provider, Source: "none", FetchedAt: now}
		}
		pu := ProviderUsage{Provider: provider, Source: "api", FetchedAt: now}
		for key, entry := range resp {
			// Collect credits and reset time from all entries; only add known quota windows.
			if entry.UsedCredits != nil && pu.Credits == nil {
				c := *entry.UsedCredits
				pu.Credits = &c
			}
			if !entry.ResetsAt.IsZero() && (pu.ResetTime == nil || entry.ResetsAt.Before(*pu.ResetTime)) {
				t := entry.ResetsAt
				pu.ResetTime = &t
			}
			if !isKnownAnthropicQuotaKey(key) {
				continue
			}
			pu.Quotas = append(pu.Quotas, Quota{
				Window:      key,
				Utilization: entry.Utilization,
				ResetsAt:    entry.ResetsAt,
			})
		}
		return pu

	case "codex":
		client, err := usage.NewCodexClient("")
		if err != nil {
			return ProviderUsage{Provider: provider, Source: "none", FetchedAt: now}
		}
		resp, err := client.FetchUsage(ctx)
		if err != nil || resp == nil {
			return ProviderUsage{Provider: provider, Source: "none", FetchedAt: now}
		}
		pu := ProviderUsage{Provider: provider, Source: "api", FetchedAt: now}
		if resp.RateLimit != nil {
			if sw := resp.RateLimit.SecondaryWindow; sw != nil {
				pu.Quotas = append(pu.Quotas, Quota{
					Window:      fmt.Sprintf("%ds", sw.LimitWindowSeconds),
					Utilization: sw.UsedPercent / 100,
					ResetsAt:    sw.ResetAt.Time,
				})
				t := sw.ResetAt.Time
				pu.ResetTime = &t
			}
			if pw := resp.RateLimit.PrimaryWindow; pw != nil {
				pu.Quotas = append(pu.Quotas, Quota{
					Window:      fmt.Sprintf("%ds-primary", pw.LimitWindowSeconds),
					Utilization: pw.UsedPercent / 100,
					ResetsAt:    pw.ResetAt.Time,
				})
			}
		}
		if resp.Credits != nil {
			c := resp.Credits.Balance
			pu.Credits = &c
		}
		return pu

	case "copilot":
		client, err := usage.NewCopilotClient()
		if err != nil {
			return ProviderUsage{Provider: provider, Source: "none", FetchedAt: now}
		}
		resp, err := client.FetchQuotas(ctx)
		if err != nil || resp == nil {
			return ProviderUsage{Provider: provider, Source: "none", FetchedAt: now}
		}
		pu := ProviderUsage{Provider: provider, Source: "api", FetchedAt: now}
		for key, snap := range resp.Quotas {
			if snap.Unlimited {
				continue
			}
			util := (100 - snap.PercentRemaining) / 100
			pu.Quotas = append(pu.Quotas, Quota{Window: key, Utilization: util})
		}
		if resp.QuotaResetDate != "" {
			if t, parseErr := time.Parse("2006-01-02", resp.QuotaResetDate); parseErr == nil {
				pu.ResetTime = &t
			}
		}
		return pu
	}
	return ProviderUsage{Provider: provider, Source: "none", FetchedAt: now}
}

// FetchAllCostSnapshots fetches cost snapshots from all three providers.
// Results for providers that fail or have no cost data are silently omitted.
// Never returns an error — failures are degraded gracefully.
func FetchAllCostSnapshots(ctx context.Context) []usage.CostSnapshot {
	var out []usage.CostSnapshot

	// Anthropic
	anthropicClient := usage.NewAnthropicClient()
	if resp, err := anthropicClient.FetchQuotas(ctx); err == nil {
		if snap := usage.ExtractAnthropicCost(resp); snap != nil {
			out = append(out, *snap)
		}
	} else {
		log.Debug().Err(err).Str("provider", "anthropic").Msg("cost: fetch failed")
	}

	// Codex
	if codexClient, err := usage.NewCodexClient(""); err == nil {
		if resp, err := codexClient.FetchUsage(ctx); err == nil {
			if snap := usage.ExtractCodexCost(resp); snap != nil {
				out = append(out, *snap)
			}
		} else {
			log.Debug().Err(err).Str("provider", "codex").Msg("cost: fetch failed")
		}
	}

	// Copilot
	if copilotClient, err := usage.NewCopilotClient(); err == nil {
		if resp, err := copilotClient.FetchQuotas(ctx); err == nil {
			if snap := usage.ExtractCopilotCost(resp); snap != nil {
				out = append(out, *snap)
			}
		} else {
			log.Debug().Err(err).Str("provider", "copilot").Msg("cost: fetch failed")
		}
	}

	return out
}

// isKnownAnthropicQuotaKey returns true for main quota windows, excluding
// per-model variants (e.g. "seven_day_sonnet_20250514") and internal keys.
func isKnownAnthropicQuotaKey(key string) bool {
	switch key {
	case "five_hour", "seven_day", "monthly_limit", "extra_usage":
		return true
	}
	return false
}
