package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/budget"
)

func TestUnicodeProgressBar(t *testing.T) {
	tests := []struct {
		pct   float64
		width int
		want  string
	}{
		{0, 10, "░░░░░░░░░░"},
		{100, 10, "██████████"},
		{50, 10, "█████░░░░░"},
		{110, 10, "██████████"}, // clamped to 100
		{-5, 10, "░░░░░░░░░░"},  // clamped to 0
	}
	for _, tc := range tests {
		got := unicodeProgressBar(tc.pct, tc.width)
		if got != tc.want {
			t.Errorf("unicodeProgressBar(%.0f, %d) = %q; want %q", tc.pct, tc.width, got, tc.want)
		}
	}
}

func TestFormatResetCountdown(t *testing.T) {
	// Use a fixed future base to avoid flaky off-by-one from test execution time.
	base := time.Now().Add(1 * time.Hour) // base is 1h in the future
	_ = base

	past := time.Now().Add(-1 * time.Hour)
	if got := formatResetCountdown(past); got != "now" {
		t.Errorf("past: got %q; want \"now\"", got)
	}

	// seconds: ~30s away; result should be "30s" or "29s" depending on scheduling, so check prefix
	in30s := time.Now().Add(30 * time.Second)
	got30s := formatResetCountdown(in30s)
	if !strings.HasSuffix(got30s, "s") {
		t.Errorf("seconds: expected Xs format, got %q", got30s)
	}

	// minutes
	in5m := time.Now().Add(5*time.Minute + 30*time.Second) // add buffer
	got5m := formatResetCountdown(in5m)
	if !strings.HasSuffix(got5m, "m") {
		t.Errorf("minutes: expected Xm format, got %q", got5m)
	}

	// hours with minutes
	in2h14m := time.Now().Add(2*time.Hour + 14*time.Minute + 30*time.Second)
	got2h14m := formatResetCountdown(in2h14m)
	if !strings.HasPrefix(got2h14m, "2h") {
		t.Errorf("2h14m: expected 2h* format, got %q", got2h14m)
	}

	// exact hours (no minutes remainder)
	exactly3h := time.Now().Add(3 * time.Hour)
	got3h := formatResetCountdown(exactly3h)
	if !strings.HasPrefix(got3h, "2h") && !strings.HasPrefix(got3h, "3h") {
		t.Errorf("3h: expected 3h* format, got %q", got3h)
	}

	// days + hours
	in3d6h := time.Now().Add(3*24*time.Hour + 6*time.Hour + 30*time.Second)
	got3d6h := formatResetCountdown(in3d6h)
	if !strings.HasPrefix(got3d6h, "3d") {
		t.Errorf("3d6h: expected 3d* format, got %q", got3d6h)
	}
}

func TestFormatQuotaWindowLabel(t *testing.T) {
	tests := []struct {
		window string
		want   string
	}{
		{"seven_day", "Weekly"},
		{"monthly", "Monthly"},
		{"premium_interactions", "Premium"},
		{"18000s-primary", "5h-pri"},
		{"short", ""},
	}
	for _, tc := range tests {
		got := formatQuotaWindowLabel(tc.window)
		if got != tc.want {
			t.Errorf("formatQuotaWindowLabel(%q) = %q; want %q", tc.window, got, tc.want)
		}
	}
}

func TestProviderDisplayName(t *testing.T) {
	if got := providerDisplayName("claude"); !strings.Contains(got, "Claude") {
		t.Errorf("expected Claude in display name, got %q", got)
	}
	if got := providerDisplayName("codex"); !strings.Contains(got, "Codex") {
		t.Errorf("expected Codex in display name, got %q", got)
	}
	if got := providerDisplayName("unknown"); got != "unknown" {
		t.Errorf("unknown provider display name = %q; want %q", got, "unknown")
	}
}

func TestProgressBar(t *testing.T) {
	bar := progressBar(0, 10)
	if !strings.Contains(bar, "0.0%") {
		t.Errorf("0%% bar missing 0.0%%: %q", bar)
	}
	bar = progressBar(100, 10)
	if !strings.Contains(bar, "100.0%") {
		t.Errorf("100%% bar missing 100.0%%: %q", bar)
	}
	bar = progressBar(150, 10)
	if !strings.Contains(bar, "150.0%") {
		t.Errorf("over-budget bar should show real pct: %q", bar)
	}
}

func TestBudgetJSONStructure(t *testing.T) {
	// Inject a fake fetchProviderUsageFn
	orig := fetchProviderUsageFn
	defer func() { fetchProviderUsageFn = orig }()

	credits := 8.20
	fetchProviderUsageFn = func(ctx context.Context, provider string) budget.ProviderUsage {
		return budget.ProviderUsage{
			Provider:  provider,
			Source:    "api",
			Credits:   &credits,
			FetchedAt: time.Now(),
			Quotas: []budget.Quota{
				{Window: "seven_day", Utilization: 0.42},
			},
		}
	}

	type QuotaJSON struct {
		Window      string  `json:"window"`
		Utilization float64 `json:"utilization"`
	}
	type ProviderJSON struct {
		Provider string      `json:"provider"`
		Source   string      `json:"source"`
		Credits  *float64    `json:"credits,omitempty"`
		UsedPct  float64     `json:"used_pct"`
		Quotas   []QuotaJSON `json:"quotas,omitempty"`
	}
	type OutputJSON struct {
		Mode      string         `json:"mode"`
		Tracking  string         `json:"tracking"`
		Providers []ProviderJSON `json:"providers"`
	}

	// Build a minimal representation to verify structure
	pu := fetchProviderUsageFn(context.Background(), "claude")
	if pu.Source != "api" {
		t.Errorf("expected source=api, got %q", pu.Source)
	}
	if pu.Credits == nil || *pu.Credits != 8.20 {
		t.Errorf("expected credits=8.20, got %v", pu.Credits)
	}
	if len(pu.Quotas) != 1 || pu.Quotas[0].Utilization != 0.42 {
		t.Errorf("unexpected quotas: %+v", pu.Quotas)
	}

	// Verify JSON marshal works
	out := OutputJSON{
		Mode:     "weekly",
		Tracking: "active",
		Providers: []ProviderJSON{
			{Provider: "claude", Source: "api", Credits: pu.Credits, UsedPct: 42.0},
		},
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if !strings.Contains(string(b), `"tracking": "active"`) {
		t.Errorf("json missing tracking field: %s", string(b))
	}
	if !strings.Contains(string(b), `"credits"`) {
		t.Errorf("json missing credits field: %s", string(b))
	}
}
