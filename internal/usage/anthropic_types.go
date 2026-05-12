package usage

import "time"

// anthropicQuotaEntryRaw is the raw JSON structure from the API.
type anthropicQuotaEntryRaw struct {
	Utilization  float64  `json:"utilization"`
	ResetsAt     string   `json:"resets_at"`
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *int64   `json:"monthly_limit,omitempty"`
	UsedCredits  *float64 `json:"used_credits,omitempty"`
}

// AnthropicQuotaEntry is a parsed quota entry with typed fields.
type AnthropicQuotaEntry struct {
	// Utilization is 0.0–1.0 fraction of quota used.
	Utilization float64
	// ResetsAt is when the quota window resets.
	ResetsAt time.Time
	// ResetsAtRaw preserves the original RFC3339 string if parsing failed,
	// so callers can log/inspect the raw value if needed.
	ResetsAtRaw string
	// IsEnabled indicates whether this quota is active.
	IsEnabled bool
	// MonthlyLimit is the optional monthly token/credit limit.
	MonthlyLimit *int64
	// UsedCredits is optional credits consumed this period.
	UsedCredits *float64
}

// AnthropicQuotaResponse maps quota names to entries.
// Known keys: five_hour, seven_day, seven_day_sonnet, monthly_limit, extra_usage.
type AnthropicQuotaResponse map[string]AnthropicQuotaEntry
