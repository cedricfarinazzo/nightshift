package usage

import "time"

// CostSnapshot holds monetary cost data captured from a provider API at a point in time.
type CostSnapshot struct {
	Provider     string
	TotalBudget  *float64 // monthly limit (Anthropic) or nil
	Used         *float64 // credits consumed (Anthropic used_credits)
	Remaining    *float64 // credits remaining (Codex credits.balance)
	OverageCount int      // Copilot overage_count
	Currency     string   // "USD" or "credits"
	Period       string   // "monthly", "balance"
	CapturedAt   time.Time
}

// ExtractAnthropicCost pulls cost data from an Anthropic quota response.
// Returns nil if no monthly_limit entry or no credit data is present.
func ExtractAnthropicCost(resp AnthropicQuotaResponse) *CostSnapshot {
	entry, ok := resp["monthly_limit"]
	if !ok {
		return nil
	}
	if entry.MonthlyLimit == nil && entry.UsedCredits == nil {
		return nil
	}

	snap := &CostSnapshot{
		Provider:   "anthropic",
		Currency:   "USD",
		Period:     "monthly",
		CapturedAt: time.Now(),
	}
	if entry.MonthlyLimit != nil {
		v := float64(*entry.MonthlyLimit) / 100 // cents → dollars
		snap.TotalBudget = &v
	}
	if entry.UsedCredits != nil {
		snap.Used = entry.UsedCredits
		if snap.TotalBudget != nil {
			rem := *snap.TotalBudget - *snap.Used
			snap.Remaining = &rem
		}
	}
	return snap
}

// ExtractCodexCost pulls cost data from a Codex usage response.
// Returns nil if resp is nil or Credits is nil.
func ExtractCodexCost(resp *CodexUsageResponse) *CostSnapshot {
	if resp == nil || resp.Credits == nil {
		return nil
	}
	bal := resp.Credits.Balance
	return &CostSnapshot{
		Provider:   "codex",
		Remaining:  &bal,
		Currency:   "credits",
		Period:     "balance",
		CapturedAt: time.Now(),
	}
}

// ExtractCopilotCost pulls cost data from a Copilot user response.
// Returns nil if resp is nil or no premium_interactions quota exists.
func ExtractCopilotCost(resp *CopilotUserResponse) *CostSnapshot {
	if resp == nil {
		return nil
	}
	snap, ok := resp.Quotas["premium_interactions"]
	if !ok {
		return nil
	}
	entitlement := snap.Entitlement
	remaining := snap.Remaining
	entF := float64(entitlement)
	remF := float64(remaining)
	s := &CostSnapshot{
		Provider:     "copilot",
		TotalBudget:  &entF,
		Remaining:    &remF,
		OverageCount: snap.OverageCount,
		Currency:     "requests",
		Period:       "monthly",
		CapturedAt:   time.Now(),
	}
	return s
}
