package usage

import (
	"testing"
	"time"
)

func ptr64(v float64) *float64 { return &v }
func ptrI64(v int64) *int64    { return &v }

func TestExtractAnthropicCost(t *testing.T) {
	tests := []struct {
		name      string
		resp      AnthropicQuotaResponse
		wantNil   bool
		wantTotal *float64
		wantUsed  *float64
		wantRem   *float64
	}{
		{
			name:    "empty response",
			resp:    AnthropicQuotaResponse{},
			wantNil: true,
		},
		{
			name: "no monthly_limit key",
			resp: AnthropicQuotaResponse{
				"five_hour": {Utilization: 0.5, ResetsAt: time.Now()},
			},
			wantNil: true,
		},
		{
			name: "monthly_limit present but nil fields",
			resp: AnthropicQuotaResponse{
				"monthly_limit": {IsEnabled: true},
			},
			wantNil: true,
		},
		{
			name: "monthly_limit with used_credits only",
			resp: AnthropicQuotaResponse{
				"monthly_limit": {
					IsEnabled:   true,
					UsedCredits: ptr64(12.40),
				},
			},
			wantNil:  false,
			wantUsed: ptr64(12.40),
		},
		{
			name: "monthly_limit with both fields",
			resp: AnthropicQuotaResponse{
				"monthly_limit": {
					IsEnabled:    true,
					MonthlyLimit: ptrI64(5000), // 5000 cents = $50.00
					UsedCredits:  ptr64(12.40),
				},
			},
			wantNil:   false,
			wantTotal: ptr64(50.00),
			wantUsed:  ptr64(12.40),
			wantRem:   ptr64(37.60),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAnthropicCost(tt.resp)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil")
			}
			if got.Provider != "anthropic" {
				t.Errorf("provider = %q, want %q", got.Provider, "anthropic")
			}
			if got.Currency != "USD" {
				t.Errorf("currency = %q, want USD", got.Currency)
			}
			cmpOptF64(t, "TotalBudget", got.TotalBudget, tt.wantTotal)
			cmpOptF64(t, "Used", got.Used, tt.wantUsed)
			cmpOptF64(t, "Remaining", got.Remaining, tt.wantRem)
		})
	}
}

func TestExtractCodexCost(t *testing.T) {
	tests := []struct {
		name    string
		resp    *CodexUsageResponse
		wantNil bool
		wantBal float64
	}{
		{name: "nil resp", resp: nil, wantNil: true},
		{name: "nil credits", resp: &CodexUsageResponse{}, wantNil: true},
		{
			name:    "credits present",
			resp:    &CodexUsageResponse{Credits: &CodexCredits{Balance: 8.20}},
			wantBal: 8.20,
		},
		{
			name:    "zero balance",
			resp:    &CodexUsageResponse{Credits: &CodexCredits{Balance: 0}},
			wantBal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCodexCost(tt.resp)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil")
			}
			if got.Provider != "codex" {
				t.Errorf("provider = %q, want codex", got.Provider)
			}
			if got.Remaining == nil {
				t.Fatal("Remaining is nil")
			}
			if *got.Remaining != tt.wantBal {
				t.Errorf("Remaining = %v, want %v", *got.Remaining, tt.wantBal)
			}
		})
	}
}

func TestExtractCopilotCost(t *testing.T) {
	tests := []struct {
		name        string
		resp        *CopilotUserResponse
		wantNil     bool
		wantOverage int
		wantRem     float64
	}{
		{name: "nil resp", resp: nil, wantNil: true},
		{name: "no quotas", resp: &CopilotUserResponse{Quotas: CopilotQuotaMap{}}, wantNil: true},
		{
			name: "premium_interactions present",
			resp: &CopilotUserResponse{
				Quotas: CopilotQuotaMap{
					"premium_interactions": {
						Entitlement:  80,
						Remaining:    12,
						OverageCount: 3,
					},
				},
			},
			wantOverage: 3,
			wantRem:     12,
		},
		{
			name: "no overage",
			resp: &CopilotUserResponse{
				Quotas: CopilotQuotaMap{
					"premium_interactions": {
						Entitlement:  80,
						Remaining:    80,
						OverageCount: 0,
					},
				},
			},
			wantOverage: 0,
			wantRem:     80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCopilotCost(tt.resp)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil")
			}
			if got.Provider != "copilot" {
				t.Errorf("provider = %q, want copilot", got.Provider)
			}
			if got.OverageCount != tt.wantOverage {
				t.Errorf("OverageCount = %d, want %d", got.OverageCount, tt.wantOverage)
			}
			if got.Remaining == nil {
				t.Fatal("Remaining is nil")
			}
			if *got.Remaining != tt.wantRem {
				t.Errorf("Remaining = %v, want %v", *got.Remaining, tt.wantRem)
			}
		})
	}
}

func cmpOptF64(t *testing.T, field string, got, want *float64) {
	t.Helper()
	if want == nil && got == nil {
		return
	}
	if want == nil {
		t.Errorf("%s: got %v, want nil", field, *got)
		return
	}
	if got == nil {
		t.Errorf("%s: got nil, want %v", field, *want)
		return
	}
	if *got != *want {
		t.Errorf("%s = %v, want %v", field, *got, *want)
	}
}
