// Package budget implements budget enforcement for nightshift using live API quotas.
// Budget is enforced as a percentage gate: if usedPercent >= maxPercent, the provider
// is considered exhausted. No token arithmetic is performed.
package budget

import (
	"fmt"
	"time"

	"github.com/marcus/nightshift/internal/config"
)

// UsageProvider is the interface for getting usage data from a provider.
type UsageProvider interface {
	Name() string
}

// ClaudeUsageProvider extends UsageProvider for Claude-specific usage methods.
type ClaudeUsageProvider interface {
	UsageProvider
	GetUsedPercent(mode string) (float64, error)
}

// CodexUsageProvider extends UsageProvider for Codex-specific usage methods.
type CodexUsageProvider interface {
	UsageProvider
	GetUsedPercent(mode string) (float64, error)
	GetResetTime(mode string) (time.Time, error)
}

// CopilotUsageProvider extends UsageProvider for Copilot-specific usage methods.
type CopilotUsageProvider interface {
	UsageProvider
	GetUsedPercent(mode string) (float64, error)
	GetResetTime(mode string) (time.Time, error)
}

// UsedPercentSourceProvider reports where the last used-percent value came from.
// Implemented optionally by providers to improve CLI diagnostics.
type UsedPercentSourceProvider interface {
	LastUsedPercentSource() string
}

// Option configures a Manager.
type Option func(*Manager)

// Manager checks budget capacity across providers using live API quotas.
type Manager struct {
	cfg     *config.Config
	claude  ClaudeUsageProvider
	codex   CodexUsageProvider
	copilot CopilotUsageProvider
	nowFunc func() time.Time // for testing
}

// NewManager creates a budget manager with the given configuration and providers.
func NewManager(cfg *config.Config, claude ClaudeUsageProvider, codex CodexUsageProvider, copilot CopilotUsageProvider, opts ...Option) *Manager {
	mgr := &Manager{
		cfg:     cfg,
		claude:  claude,
		codex:   codex,
		copilot: copilot,
		nowFunc: time.Now,
	}
	for _, opt := range opts {
		opt(mgr)
	}
	return mgr
}

// AllowanceResult holds the live usage data for a provider.
// All budget decisions are percentage-based; no token arithmetic is performed.
type AllowanceResult struct {
	UsedPercent       float64
	UsedPercentSource string
	MaxPercent        int
}

// EnforcementResult holds the budget check outcome for a single provider.
type EnforcementResult struct {
	Provider  string
	OK        bool
	Reason    string
	Allowance *AllowanceResult
}

// GetUsedPercent retrieves the live used percentage from the appropriate provider.
func (m *Manager) GetUsedPercent(provider string) (float64, error) {
	mode := m.cfg.Budget.Mode
	if mode == "" {
		mode = config.DefaultBudgetMode
	}

	switch provider {
	case "claude":
		if m.claude == nil {
			return 0, fmt.Errorf("claude provider not configured")
		}
		return m.claude.GetUsedPercent(mode)

	case "codex":
		if m.codex == nil {
			return 0, fmt.Errorf("codex provider not configured")
		}
		return m.codex.GetUsedPercent(mode)

	case "copilot":
		if m.copilot == nil {
			return 0, fmt.Errorf("copilot provider not configured")
		}
		return m.copilot.GetUsedPercent(mode)

	default:
		return 0, fmt.Errorf("unknown provider: %s", provider)
	}
}

func (m *Manager) usedPercentSource(provider string) string {
	switch provider {
	case "claude":
		if reporter, ok := m.claude.(UsedPercentSourceProvider); ok {
			return reporter.LastUsedPercentSource()
		}
	case "codex":
		if reporter, ok := m.codex.(UsedPercentSourceProvider); ok {
			return reporter.LastUsedPercentSource()
		}
	case "copilot":
		return "api"
	}
	return ""
}

// CheckProviders checks budget capacity for a set of providers using live API percentages.
//
// For run/preview: pass providers in priority order — caller picks first OK result.
// For task run: pass single explicit provider; error if not OK.
// For jira: pass only providers needed for remaining phases; skip ticket if any not OK.
//
// When ignoreBudget is true, all results have OK=true (bypass).
func (m *Manager) CheckProviders(providers []string, ignoreBudget bool) ([]EnforcementResult, error) {
	maxPercent := m.cfg.Budget.MaxPercent
	if maxPercent <= 0 {
		maxPercent = config.DefaultMaxPercent
	}

	results := make([]EnforcementResult, 0, len(providers))
	for _, provider := range providers {
		usedPct, err := m.GetUsedPercent(provider)
		if err != nil {
			results = append(results, EnforcementResult{
				Provider: provider,
				OK:       ignoreBudget,
				Reason:   fmt.Sprintf("budget error: %v", err),
			})
			continue
		}
		ok := ignoreBudget || usedPct < float64(maxPercent)
		reason := ""
		if !ok {
			reason = fmt.Sprintf("budget at %.0f%% (limit: %d%%)", usedPct, maxPercent)
		}
		results = append(results, EnforcementResult{
			Provider: provider,
			OK:       ok,
			Reason:   reason,
			Allowance: &AllowanceResult{
				UsedPercent:       usedPct,
				UsedPercentSource: m.usedPercentSource(provider),
				MaxPercent:        maxPercent,
			},
		})
	}
	return results, nil
}

// Summary returns a human-readable budget status for a provider.
func (m *Manager) Summary(provider string) (string, error) {
	maxPercent := m.cfg.Budget.MaxPercent
	if maxPercent <= 0 {
		maxPercent = config.DefaultMaxPercent
	}
	usedPct, err := m.GetUsedPercent(provider)
	if err != nil {
		return "", err
	}
	status := "OK"
	if usedPct >= float64(maxPercent) {
		status = "exhausted"
	}
	return fmt.Sprintf("%s: %.1f%% used (limit: %d%%) [%s]", provider, usedPct, maxPercent, status), nil
}
