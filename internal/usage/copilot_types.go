// Package usage provides API-based usage fetching for AI providers.
package usage

import "strings"

// CopilotQuotaSnapshot holds usage quota data for a single Copilot feature.
type CopilotQuotaSnapshot struct {
	Entitlement      int     `json:"entitlement"`
	Remaining        int     `json:"remaining"`
	QuotaRemaining   float64 `json:"quota_remaining"`
	PercentRemaining float64 `json:"percent_remaining"`
	OverageCount     int     `json:"overage_count"`
	OveragePermitted bool    `json:"overage_permitted"`
	Unlimited        bool    `json:"unlimited"`
}

// CopilotQuotaMap is a normalized map of quota name → snapshot.
type CopilotQuotaMap map[string]CopilotQuotaSnapshot

// copilotAPIResponse is the raw JSON from api.github.com/copilot_internal/user.
// Both premium and free plan fields are present; normalize() unifies them.
type copilotAPIResponse struct {
	Login          string                     `json:"login"`
	CopilotPlan    string                     `json:"copilot_plan"`
	AccessTypeSKU  string                     `json:"access_type_sku"`
	QuotaResetDate string                     `json:"quota_reset_date"`
	// Premium plan
	QuotaSnapshots map[string]CopilotQuotaSnapshot `json:"quota_snapshots"`
	// Free plan
	LimitedUserQuotas map[string]int          `json:"limited_user_quotas"`
	MonthlyQuotas     map[string]int          `json:"monthly_quotas"`
}

// CopilotUserResponse is the normalized, caller-facing response.
type CopilotUserResponse struct {
	Login          string
	CopilotPlan    string
	AccessTypeSKU  string
	QuotaResetDate string
	Quotas         CopilotQuotaMap
}

// normalize converts raw API response into CopilotUserResponse.
func normalize(raw copilotAPIResponse) *CopilotUserResponse {
	resp := &CopilotUserResponse{
		Login:          raw.Login,
		CopilotPlan:    raw.CopilotPlan,
		AccessTypeSKU:  raw.AccessTypeSKU,
		QuotaResetDate: raw.QuotaResetDate,
		Quotas:         make(CopilotQuotaMap),
	}

	// Premium plan: quota_snapshots
	for k, v := range raw.QuotaSnapshots {
		resp.Quotas[k] = v
	}

	// Free plan: limited_user_quotas (per-feature remaining counts)
	for k, remaining := range raw.LimitedUserQuotas {
		snap := resp.Quotas[k] // may already exist from quota_snapshots
		snap.Remaining = remaining
		resp.Quotas[k] = snap
	}

	// Free plan: monthly_quotas (total entitlements)
	for k, entitlement := range raw.MonthlyQuotas {
		snap := resp.Quotas[k]
		snap.Entitlement = entitlement
		if entitlement > 0 {
			snap.PercentRemaining = float64(snap.Remaining) / float64(entitlement) * 100
		}
		resp.Quotas[k] = snap
	}

	return resp
}

var quotaDisplayNames = map[string]string{
	"premium_interactions": "Premium Requests",
	"chat":                 "Chat",
	"completions":          "Completions",
	"code_review":          "Code Review",
}

// DisplayName maps an API quota key to a human-readable label.
func DisplayName(key string) string {
	if name, ok := quotaDisplayNames[key]; ok {
		return name
	}
	// Fallback: replace underscores with spaces and title-case.
	words := strings.Split(key, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
