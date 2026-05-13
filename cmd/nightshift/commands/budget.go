package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/marcus/nightshift/internal/budget"
	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/db"
	"github.com/marcus/nightshift/internal/providers"
	"github.com/marcus/nightshift/internal/snapshots"
	"github.com/marcus/nightshift/internal/trends"
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

var budgetHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show recent budget snapshots",
	Long:  `Show recent usage snapshots stored by the active tracking system.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, _ := cmd.Flags().GetString("provider")
		n, _ := cmd.Flags().GetInt("n")
		return runBudgetHistory(provider, n)
	},
}

func init() {
	budgetCmd.Flags().StringP("provider", "p", "", "Show specific provider status (claude, codex, copilot)")
	budgetCmd.Flags().Bool("json", false, "Output JSON for scripting")
	rootCmd.AddCommand(budgetCmd)

	budgetHistoryCmd.Flags().StringP("provider", "p", "", "Provider to show history for (claude, codex, copilot)")
	budgetHistoryCmd.Flags().IntP("n", "n", 20, "Number of snapshots to show")
	budgetCmd.AddCommand(budgetHistoryCmd)
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

	var claude *providers.Claude
	var codex *providers.Codex
	var copilot *providers.Copilot

	if cfg.Providers.Claude.Enabled {
		dataPath := cfg.ExpandedProviderPath("claude")
		if dataPath != "" {
			claude = providers.NewClaudeWithPath(dataPath)
		} else {
			claude = providers.NewClaude()
		}
	}

	if cfg.Providers.Codex.Enabled {
		dataPath := cfg.ExpandedProviderPath("codex")
		if dataPath != "" {
			codex = providers.NewCodexWithPath(dataPath)
		} else {
			codex = providers.NewCodex()
		}
	}

	if cfg.Providers.Copilot.Enabled {
		dataPath := cfg.ExpandedProviderPath("copilot")
		if dataPath != "" {
			copilot = providers.NewCopilotWithPath(dataPath)
		} else {
			copilot = providers.NewCopilot()
		}
	}

	trend := trends.NewAnalyzer(database, cfg.Budget.SnapshotRetentionDays)
	mgr := budget.NewManagerWithTrackingFromProviders(cfg, claude, codex, copilot, budget.WithTrendAnalyzer(trend))

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

	snapCollector := snapshots.NewCollector(database, weekStartDayFromConfig(cfg))

	for _, provName := range providerList {
		if err := printProviderBudgetActive(cfg, provName, mgr, snapCollector, codex, opts); err != nil {
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
	snapCollector *snapshots.Collector,
	codex *providers.Codex,
	opts budgetOptions,
) error {
	ctx := context.Background()

	pu := fetchProviderUsageFn(ctx, provName)

	result, allowanceErr := mgr.CalculateAllowance(provName)

	srcLabel := strings.ToUpper(pu.Source)
	if pu.Source == "none" || pu.Source == "" {
		srcLabel = "file"
	}

	displayName := providerDisplayName(provName)
	fmt.Printf("[%s]  Source: %s\n", displayName, srcLabel)

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
	} else if allowanceErr == nil {
		bar := unicodeProgressBar(result.UsedPercent, 25)
		pct := fmt.Sprintf("%.0f%%", result.UsedPercent)
		resetStr := ""
		if snapCollector != nil {
			if latest, err := snapCollector.GetLatest(provName, 1); err == nil && len(latest) > 0 {
				resetStr = "  " + formatResetLine(latest[0].SessionResetTime, latest[0].WeeklyResetTime)
			}
		}
		fmt.Printf("  Used     %s %4s%s\n", bar, pct, resetStr)
	}

	if pu.Credits != nil {
		fmt.Printf("  Credits  $%.2f remaining\n", *pu.Credits)
	}

	if provName == "copilot" {
		printCopilotPlan(ctx)
	}

	if pu.ResetTime != nil {
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
		Provider  string      `json:"provider"`
		Source    string      `json:"source"`
		Quotas    []QuotaJSON `json:"quotas,omitempty"`
		Credits   *float64    `json:"credits,omitempty"`
		UsedPct   float64     `json:"used_pct"`
		ResetTime *time.Time  `json:"reset_time,omitempty"`
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
			if pj.UsedPct == 0 {
				pj.UsedPct = q.Utilization * 100
			}
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

func runBudgetHistory(filterProvider string, n int) error {
	if n <= 0 {
		return fmt.Errorf("n must be positive")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	database, err := db.Open(cfg.ExpandedDBPath())
	if err != nil {
		return fmt.Errorf("opening db: %w", err)
	}
	defer func() { _ = database.Close() }()

	providerList, err := resolveProviderList(cfg, filterProvider)
	if err != nil {
		return err
	}

	if len(providerList) == 0 {
		fmt.Println("No providers enabled.")
		return nil
	}

	collector := snapshots.NewCollector(database, weekStartDayFromConfig(cfg))

	for _, provider := range providerList {
		history, err := collector.GetLatest(provider, n)
		if err != nil {
			fmt.Printf("%s: error: %v\n\n", provider, err)
			continue
		}
		if len(history) == 0 {
			fmt.Printf("[%s]\n  No snapshots yet.\n\n", provider)
			continue
		}

		fmt.Printf("[%s]\n", provider)
		printSnapshotTable(history)
		fmt.Println()
	}

	return nil
}

func printSnapshotTable(history []snapshots.Snapshot) {
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "Time\tLocal\tDaily\tPct\tInferred\tResets")
	for _, snapshot := range history {
		pct := "-"
		if snapshot.ScrapedPct != nil {
			pct = fmt.Sprintf("%.1f%%", *snapshot.ScrapedPct)
		}
		inferred := "-"
		if snapshot.InferredBudget != nil {
			inferred = formatTokens64(*snapshot.InferredBudget)
		}
		resets := formatResetLine(snapshot.SessionResetTime, snapshot.WeeklyResetTime)
		if resets == "" {
			resets = "-"
		}
		_, _ = fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			snapshot.Timestamp.Format("Jan 02 15:04"),
			formatTokens64(snapshot.LocalTokens),
			formatTokens64(snapshot.LocalDaily),
			pct,
			inferred,
			resets,
		)
	}
	_ = writer.Flush()
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

// formatResetLine builds the "Resets:" display from reset time strings.
func formatResetLine(sessionReset, weeklyReset string) string {
	var parts []string
	if sessionReset != "" {
		parts = append(parts, "session "+sessionReset)
	}
	if weeklyReset != "" {
		parts = append(parts, "week "+weeklyReset)
	}
	return strings.Join(parts, " · ")
}

func progressBar(percent float64, width int) string {
	displayPercent := percent
	if percent < 0 {
		percent = 0
	}
	fillPercent := percent
	if fillPercent > 100 {
		fillPercent = 100
	}

	filled := int(fillPercent * float64(width) / 100)
	empty := width - filled

	bar := ""
	for i := 0; i < filled; i++ {
		bar += "#"
	}
	for i := 0; i < empty; i++ {
		bar += "-"
	}

	return fmt.Sprintf("[%s] %.1f%%", bar, displayPercent)
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
