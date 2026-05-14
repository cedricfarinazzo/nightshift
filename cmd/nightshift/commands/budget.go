package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/marcus/nightshift/internal/budget"
	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/db"
)

var budgetCmd = &cobra.Command{
	Use:   "budget",
	Short: "Show budget status",
	Long: `Display current budget status and usage.

Shows spending across all providers or a specific provider.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, _ := cmd.Flags().GetString("provider")
		jsonOut, _ := cmd.Flags().GetBool("json")
		return runBudget(budgetOptions{
			provider: provider,
			jsonOut:  jsonOut,
		})
	},
}

func init() {
	budgetCmd.Flags().StringP("provider", "p", "", "Show specific provider status (claude, codex, copilot)")
	budgetCmd.Flags().Bool("json", false, "Output JSON for scripting")
	rootCmd.AddCommand(budgetCmd)
}

type budgetOptions struct {
	provider string
	jsonOut  bool
}

// fetchProviderUsageFn is injectable for tests.
var fetchProviderUsageFn = func(ctx context.Context, provider string) budget.ProviderUsage {
	return budget.FetchProviderUsage(ctx, provider)
}

func runBudget(opts budgetOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	database, err := db.Open(cfg.ExpandedDBPath())
	if err != nil {
		return fmt.Errorf("opening db: %w", err)
	}
	defer func() { _ = database.Close() }()

	mgr := budget.NewManagerWithTracking(cfg)

	providerList, err := resolveProviderList(cfg, opts.provider)
	if err != nil {
		return err
	}

	if len(providerList) == 0 {
		fmt.Println("No providers enabled.")
		return nil
	}

	if opts.jsonOut {
		return printBudgetJSON(cfg, providerList, mgr)
	}

	mode := cfg.Budget.Mode
	if mode == "" {
		mode = config.DefaultBudgetMode
	}

	header := fmt.Sprintf("Budget Status (Live) (mode: %s)", mode)
	fmt.Println(header)
	fmt.Println(strings.Repeat("=", len(header)))
	fmt.Println()

	for _, provName := range providerList {
		if err := printProviderBudgetActive(cfg, provName, mgr, opts); err != nil {
			fmt.Printf("%s: error: %v\n\n", provName, err)
			continue
		}
		fmt.Println()
	}

	fmt.Printf("Mode: active  Last fetch: now\n")

	return nil
}

func printProviderBudgetActive(
	cfg *config.Config,
	provName string,
	mgr *budget.Manager,
	opts budgetOptions,
) error {
	ctx := context.Background()

	pu := fetchProviderUsageFn(ctx, provName)

	srcLabel := strings.ToUpper(pu.Source)
	if pu.Source == "none" || pu.Source == "" {
		srcLabel = "N/A"
	}

	displayName := providerDisplayName(provName)
	fmt.Printf("[%s]  Source: %s\n", displayName, srcLabel)
	if pu.Source == "none" || pu.Source == "" {
		switch provName {
		case "claude":
			fmt.Printf("  (set ANTHROPIC_API_KEY to enable active tracking)\n")
		case "codex":
			fmt.Printf("  (set OPENAI_API_KEY to enable active tracking)\n")
		case "copilot":
			fmt.Printf("  (configure GitHub token via 'gh auth' to enable active tracking)\n")
		}
	}

	hcr, hcrErr := mgr.GetHourlyCapacity(ctx, provName)

	if len(pu.Quotas) > 0 {
		for _, q := range pu.Quotas {
			label := formatQuotaWindowLabel(q.Window)
			if label == "" {
				continue
			}
			bar := unicodeProgressBar(q.Utilization*100, 25)
			pct := fmt.Sprintf("%.0f%%", q.Utilization*100)
			resetStr := ""
			if !q.ResetsAt.IsZero() {
				resetStr = " ↻ " + formatResetCountdown(q.ResetsAt)
			}
			fmt.Printf("  %-8s %s %4s%s\n", label, bar, pct, resetStr)
		}
	} else if hcrErr == nil {
		bar := unicodeProgressBar(hcr.BottleneckUsedPct, 25)
		pct := fmt.Sprintf("%.0f%%", hcr.BottleneckUsedPct)
		fmt.Printf("  Used     %s %4s\n", bar, pct)
	}

	if hcrErr == nil {
		printHourlyCapacity(hcr)
	}

	if pu.Credits != nil {
		fmt.Printf("  Credits  $%.2f remaining\n", *pu.Credits)
	}

	if provName == "copilot" {
		printCopilotPlan(ctx)
	}

	// Only show Reset line when no per-window reset info already displayed.
	if pu.ResetTime != nil && (hcrErr != nil || len(hcr.Windows) == 0) {
		fmt.Printf("  Reset:   %s\n", formatResetCountdown(*pu.ResetTime))
	}

	return nil
}

func printCopilotPlan(ctx context.Context) {
	// Best-effort: fetch plan info from copilot API
	// Silently skip if unavailable
}

func printBudgetJSON(cfg *config.Config, providerList []string, mgr *budget.Manager) error {
	ctx := context.Background()
	now := time.Now()

	type QuotaJSON struct {
		Window      string    `json:"window"`
		Utilization float64   `json:"utilization"`
		ResetsAt    time.Time `json:"resets_at,omitempty"`
	}

	type ProviderJSON struct {
		Provider        string      `json:"provider"`
		Source          string      `json:"source"`
		Quotas          []QuotaJSON `json:"quotas,omitempty"`
		Credits         *float64    `json:"credits,omitempty"`
		UsedPct         float64     `json:"used_pct"`
		HourlyCapacity  float64     `json:"hourly_capacity"`
		BottleneckWindow string     `json:"bottleneck_window,omitempty"`
		ResetTime       *time.Time  `json:"reset_time,omitempty"`
	}

	type OutputJSON struct {
		Mode      string         `json:"mode"`
		Providers []ProviderJSON `json:"providers"`
		FetchedAt time.Time      `json:"fetched_at"`
	}

	budgetMode := cfg.Budget.Mode
	if budgetMode == "" {
		budgetMode = config.DefaultBudgetMode
	}

	out := OutputJSON{
		Mode:      budgetMode,
		FetchedAt: now,
	}

	for _, provName := range providerList {
		pj := ProviderJSON{Provider: provName}

		pu := fetchProviderUsageFn(ctx, provName)
		pj.Source = pu.Source
		pj.Credits = pu.Credits
		pj.ResetTime = pu.ResetTime
		for _, q := range pu.Quotas {
			pj.Quotas = append(pj.Quotas, QuotaJSON{
				Window:      q.Window,
				Utilization: q.Utilization,
				ResetsAt:    q.ResetsAt,
			})
		}
		if hcr, err := mgr.GetHourlyCapacity(ctx, provName); err == nil {
			pj.UsedPct = hcr.BottleneckUsedPct
			pj.HourlyCapacity = hcr.Capacity
			pj.BottleneckWindow = hcr.BottleneckWindow
		}
		if pj.Source == "" {
			pj.Source = "none"
		}

		out.Providers = append(out.Providers, pj)
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	fmt.Println(string(b))
	return nil
}


func formatTokens64(tokens int64) string {
	if tokens >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
	}
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}
// providerDisplayName returns a human-friendly provider name.
func providerDisplayName(provName string) string {
	switch provName {
	case "claude":
		return "Anthropic (Claude)"
	case "codex":
		return "OpenAI (Codex)"
	case "copilot":
		return "Copilot"
	default:
		return provName
	}
}
