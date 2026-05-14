// Package budget implements budget enforcement for nightshift using live API quotas.
// Budget is enforced as a percentage gate: if hourly capacity reaches 0, the provider
// is considered exhausted. No token arithmetic is performed.
package budget

import (
	"context"
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
	GetHourlyCapacity(ctx context.Context, maxPercent int) (HourlyCapacityResult, error)
}

// CodexUsageProvider extends UsageProvider for Codex-specific usage methods.
type CodexUsageProvider interface {
	UsageProvider
	GetHourlyCapacity(ctx context.Context, maxPercent int) (HourlyCapacityResult, error)
	GetResetTime(mode string) (time.Time, error)
}

// CopilotUsageProvider extends UsageProvider for Copilot-specific usage methods.
type CopilotUsageProvider interface {
	UsageProvider
	GetHourlyCapacity(ctx context.Context, maxPercent int) (HourlyCapacityResult, error)
	GetResetTime(mode string) (time.Time, error)
}

// WindowCapacity holds computed capacity for a single quota window.
type WindowCapacity struct {
	Name     string
	UsedPct  float64
	ResetIn  time.Duration
	Capacity float64 // 0–1
}

// HourlyCapacityResult holds the computed hourly capacity for a provider.
// Capacity is the minimum across all quota windows; 0 = exhausted.
type HourlyCapacityResult struct {
	Capacity          float64          // 0–1; 0 = blocked, 1 = full capacity
	BottleneckWindow  string           // most constraining window name
	BottleneckUsedPct float64          // raw utilization of bottleneck window (for display)
	Windows           []WindowCapacity // per-window breakdown
	Source            string           // "api" or "none"
}

// Option configures a Manager.
type Option func(*Manager)

// Manager checks budget capacity across providers using live API quotas.
type Manager struct {
	cfg     *config.Config
	claude  ClaudeUsageProvider
	codex   CodexUsageProvider
	copilot CopilotUsageProvider
}

// NewManager creates a budget manager with the given configuration and providers.
func NewManager(cfg *config.Config, claude ClaudeUsageProvider, codex CodexUsageProvider, copilot CopilotUsageProvider, opts ...Option) *Manager {
	mgr := &Manager{
		cfg:     cfg,
		claude:  claude,
		codex:   codex,
		copilot: copilot,
	}
	for _, opt := range opts {
		opt(mgr)
	}
	return mgr
}

// AllowanceResult holds the live budget check result for a single provider.
// All budget decisions are based on hourly capacity; no token arithmetic is performed.
type AllowanceResult struct {
	HourlyCapacity    float64 // 0–1; 0 = exhausted
	BottleneckWindow  string
	BottleneckUsedPct float64 // raw utilization of bottleneck window (for display)
	MaxPercent        int
	Source            string
	Windows           []WindowCapacity // per-window input data used to compute capacity
}

// EnforcementResult holds the budget check outcome for a single provider.
type EnforcementResult struct {
	Provider  string
	OK        bool
	Reason    string
	Allowance *AllowanceResult
}

// GetHourlyCapacity retrieves the live hourly capacity from the appropriate provider.
func (m *Manager) GetHourlyCapacity(ctx context.Context, provider string) (HourlyCapacityResult, error) {
	maxPercent := m.cfg.Budget.MaxPercent
	if maxPercent <= 0 {
		maxPercent = config.DefaultMaxPercent
	}

	switch provider {
	case "claude":
		if m.claude == nil {
			return HourlyCapacityResult{Source: "none"}, fmt.Errorf("claude provider not configured")
		}
		return m.claude.GetHourlyCapacity(ctx, maxPercent)

	case "codex":
		if m.codex == nil {
			return HourlyCapacityResult{Source: "none"}, fmt.Errorf("codex provider not configured")
		}
		return m.codex.GetHourlyCapacity(ctx, maxPercent)

	case "copilot":
		if m.copilot == nil {
			return HourlyCapacityResult{Source: "none"}, fmt.Errorf("copilot provider not configured")
		}
		return m.copilot.GetHourlyCapacity(ctx, maxPercent)

	default:
		return HourlyCapacityResult{Source: "none"}, fmt.Errorf("unknown provider: %s", provider)
	}
}

// GetUsedPercent returns the bottleneck window's raw utilization for a provider.
// Kept for backward-compat display use in budget and doctor commands.
func (m *Manager) GetUsedPercent(provider string) (float64, error) {
	result, err := m.GetHourlyCapacity(context.Background(), provider)
	if err != nil {
		return 0, err
	}
	return result.BottleneckUsedPct, nil
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

	ctx := context.Background()
	results := make([]EnforcementResult, 0, len(providers))
	for _, provider := range providers {
		hcr, err := m.GetHourlyCapacity(ctx, provider)
		if err != nil {
			results = append(results, EnforcementResult{
				Provider: provider,
				OK:       ignoreBudget,
				Reason:   fmt.Sprintf("budget error: %v", err),
			})
			continue
		}
		ok := ignoreBudget || hcr.Capacity > 0
		reason := ""
		if !ok {
			reason = fmt.Sprintf("budget exhausted on %s (%.0f%% used, limit: %d%%)",
				hcr.BottleneckWindow, hcr.BottleneckUsedPct, maxPercent)
		}
		results = append(results, EnforcementResult{
			Provider: provider,
			OK:       ok,
			Reason:   reason,
			Allowance: &AllowanceResult{
				HourlyCapacity:    hcr.Capacity,
				BottleneckWindow:  hcr.BottleneckWindow,
				BottleneckUsedPct: hcr.BottleneckUsedPct,
				MaxPercent:        maxPercent,
				Source:            hcr.Source,
				Windows:           hcr.Windows,
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
	hcr, err := m.GetHourlyCapacity(context.Background(), provider)
	if err != nil {
		return "", err
	}
	status := "OK"
	if hcr.Capacity <= 0 {
		status = "exhausted"
	}
	return fmt.Sprintf("%s: %.1f%% used on %s (capacity: %.0f%%, limit: %d%%) [%s]",
		provider, hcr.BottleneckUsedPct, hcr.BottleneckWindow,
		hcr.Capacity*100, maxPercent, status), nil
}

// computeWindowCapacity computes the hourly capacity (0–1) for a single quota window.
//
// The formula maximises quota ROI by pacing consumption to reach max_pct by reset:
//   - exhausted (remaining ≤ 0): 0
//   - expiring (reset < window/4): boost by time urgency — min(1, remaining/max * window/reset)
//   - nearly depleted (remaining < 15): absolute remaining fraction (no urgency boost)
//   - normal: pace-adjusted — min(1, remaining/max * max(1, ideal_rate/current_rate))
func computeWindowCapacity(usedPct, maxPct, windowHours, resetHours float64) float64 {
	const nearlyDepletedThreshold = 15.0

	remaining := maxPct - usedPct
	if remaining <= 0 {
		return 0
	}
	if resetHours <= 0 {
		resetHours = 0.01
	}

	remainingFrac := remaining / maxPct

	if resetHours < windowHours/4 {
		// Expiring window: boost urgency so remaining capacity is shown at full rate.
		// Takes priority over nearly-depleted: no point conserving when window resets soon.
		v := remainingFrac * windowHours / resetHours
		if v > 1 {
			return 1
		}
		return v
	}

	if remaining < nearlyDepletedThreshold {
		// Nearly depleted with lots of time left: return raw fraction, no boost.
		return remainingFrac
	}

	// Normal: pace-based. Boost when we should be consuming faster than current headroom rate.
	// idealRate: target consumption rate to exhaust maxPct by window end.
	// headroomRate: remaining capacity per hour left — how hard we can still run.
	// When idealRate > headroomRate we need to run harder to hit the target → boost.
	idealRate := maxPct / windowHours
	headroomRate := remaining / resetHours
	paceFactor := 1.0
	if headroomRate > 0 && idealRate > headroomRate {
		paceFactor = idealRate / headroomRate
	}
	v := remainingFrac * paceFactor
	if v > 1 {
		return 1
	}
	return v
}
