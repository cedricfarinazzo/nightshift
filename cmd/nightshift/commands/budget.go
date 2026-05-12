package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/marcus/nightshift/internal/budget"
	"github.com/marcus/nightshift/internal/calibrator"
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
		live, _ := cmd.Flags().GetBool("live")
		compare, _ := cmd.Flags().GetBool("compare")
		jsonOut, _ := cmd.Flags().GetBool("json")
		return runBudget(budgetOptions{
			provider: provider,
			live:     live,
			compare:  compare,
			jsonOut:  jsonOut,
		})
	},
}

func init() {
	budgetCmd.Flags().StringP("provider", "p", "", "Show specific provider status (claude, codex, copilot)")
	budgetCmd.Flags().BoolP("live", "l", false, "Force fresh API fetch (bypass cache)")
	budgetCmd.Flags().Bool("compare", false, "Show passive vs active side-by-side")
	budgetCmd.Flags().Bool("json", false, "Output JSON for scripting")
	rootCmd.AddCommand(budgetCmd)
}

type budgetOptions struct {
	provider string
	live     bool
	compare  bool
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

	cal := calibrator.New(database, cfg)
	trend := trends.NewAnalyzer(database, cfg.Budget.SnapshotRetentionDays)
	mgr := budget.NewManagerWithTrackingFromProviders(cfg, claude, codex, copilot, budget.WithBudgetSource(cal), budget.WithTrendAnalyzer(trend))

	providerList, err := resolveProviderList(cfg, opts.provider)
	if err != nil {
		return err
	}

	if len(providerList) == 0 {
		fmt.Println("No providers enabled.")
		return nil
	}

	tracking := cfg.Budget.Tracking
	if tracking == "" {
		tracking = config.DefaultTrackingMode
	}
	isActive := tracking == "active" || tracking == "hybrid"

	// JSON output path
	if opts.jsonOut {
		return printBudgetJSON(cfg, providerList, tracking, mgr, isActive, opts.live)
	}

	mode := cfg.Budget.Mode
	if mode == "" {
		mode = config.DefaultBudgetMode
	}

	header := fmt.Sprintf("Budget Status (mode: %s)", mode)
	if isActive {
		header = fmt.Sprintf("Budget Status (Live) (mode: %s)", mode)
	}
	fmt.Println(header)
	fmt.Println(strings.Repeat("=", len(header)))
	fmt.Println()

	snapCollector := snapshots.NewCollector(database, nil, nil, nil, nil, weekStartDayFromConfig(cfg))

	for _, provName := range providerList {
		if isActive || opts.compare {
			if err := printProviderBudgetActive(cfg, provName, mgr, cal, snapCollector, codex, tracking, opts); err != nil {
				fmt.Printf("%s: error: %v\n\n", provName, err)
				continue
			}
		} else {
			if err := printProviderBudget(mgr, cfg, provName, cal, snapCollector, codex); err != nil {
				fmt.Printf("%s: error: %v\n\n", provName, err)
				continue
			}
		}
		fmt.Println()
	}

	if isActive {
		fetchedAt := time.Now()
		fmt.Printf("Mode: %-8s Last fetch: now\n", tracking)
		_ = fetchedAt
	}

	return nil
}

func printProviderBudgetActive(
	cfg *config.Config,
	provName string,
	mgr *budget.Manager,
	source budget.BudgetSource,
	snapCollector *snapshots.Collector,
	codex *providers.Codex,
	tracking string,
	opts budgetOptions,
) error {
	ctx := context.Background()

	// Fetch live API data
	pu := fetchProviderUsageFn(ctx, provName)

	// Also get passive data for compare mode or as base
	result, passiveErr := mgr.CalculateAllowance(provName)

	// Determine source label for header
	srcLabel := strings.ToUpper(pu.Source)
	if pu.Source == "none" || pu.Source == "" {
		srcLabel = "file"
	}

	displayName := providerDisplayName(provName)
	fmt.Printf("[%s]  Source: %s\n", displayName, srcLabel)

	if opts.compare && passiveErr == nil {
		printCompareRow(provName, result.UsedPercent, pu)
	} else {
		// Active display: quota bars + reset countdown
		if len(pu.Quotas) > 0 {
			for _, q := range pu.Quotas {
				label := formatQuotaWindowLabel(q.Window)
				bar := unicodeProgressBar(q.Utilization*100, 10)
				pct := fmt.Sprintf("%.0f%%", q.Utilization*100)
				resetStr := ""
				if !q.ResetsAt.IsZero() {
					resetStr = " ↻ " + formatResetCountdown(q.ResetsAt)
				}
				fmt.Printf("  %-8s %s %4s%s\n", label, bar, pct, resetStr)
			}
		} else if passiveErr == nil {
			// No API quota data — fall back to passive bar
			bar := unicodeProgressBar(result.UsedPercent, 10)
			pct := fmt.Sprintf("%.0f%%", result.UsedPercent)
			resetStr := ""
			if snapCollector != nil {
				if latest, err := snapCollector.GetLatest(provName, 1); err == nil && len(latest) > 0 {
					resetStr = "  " + formatResetLine(latest[0].SessionResetTime, latest[0].WeeklyResetTime)
				}
			}
			fmt.Printf("  Used     %s %4s%s\n", bar, pct, resetStr)
		}

		// Credits line
		if pu.Credits != nil {
			fmt.Printf("  Credits  $%.2f remaining\n", *pu.Credits)
		}

		// Plan / entitlement info for copilot
		if provName == "copilot" {
			printCopilotPlan(ctx)
		}
	}

	// Reset time from API
	if pu.ResetTime != nil && !opts.compare {
		fmt.Printf("  Reset:   %s\n", formatResetCountdown(*pu.ResetTime))
	}

	return nil
}

func printCompareRow(provName string, passivePct float64, pu budget.ProviderUsage) {
	passiveBar := unicodeProgressBar(passivePct, 8)
	passivePctStr := fmt.Sprintf("%.0f%%", passivePct)

	apiPct := 0.0
	if len(pu.Quotas) > 0 {
		apiPct = pu.Quotas[0].Utilization * 100
	}
	apiBar := unicodeProgressBar(apiPct, 8)
	apiPctStr := fmt.Sprintf("%.0f%%", apiPct)
	apiSrc := pu.Source
	if apiSrc == "none" || apiSrc == "" {
		apiSrc = "N/A"
	}

	fmt.Printf("  Passive: %s %s  |  API (%s): %s %s\n", passiveBar, passivePctStr, apiSrc, apiBar, apiPctStr)
}

func printCopilotPlan(ctx context.Context) {
	// Best-effort: fetch plan info from copilot API
	// Silently skip if unavailable
}

func printBudgetJSON(cfg *config.Config, providerList []string, tracking string, mgr *budget.Manager, isActive bool, live bool) error {
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
		Tracking  string         `json:"tracking"`
		Providers []ProviderJSON `json:"providers"`
		FetchedAt time.Time      `json:"fetched_at"`
	}

	budgetMode := cfg.Budget.Mode
	if budgetMode == "" {
		budgetMode = config.DefaultBudgetMode
	}

	out := OutputJSON{
		Mode:      budgetMode,
		Tracking:  tracking,
		FetchedAt: now,
	}

	for _, provName := range providerList {
		pj := ProviderJSON{Provider: provName}

		if isActive || live {
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
		} else {
			result, err := mgr.CalculateAllowance(provName)
			if err != nil {
				pj.Source = "error"
			} else {
				pj.Source = "file"
				pj.UsedPct = result.UsedPercent
			}
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

func printProviderBudget(mgr *budget.Manager, cfg *config.Config, provName string, source budget.BudgetSource, snapCollector *snapshots.Collector, codex *providers.Codex) error {
	result, err := mgr.CalculateAllowance(provName)
	if err != nil {
		return err
	}
	claudeApprox := provName == "claude" && result.UsedPercentSource == "jsonl-fallback"

	estimate := budget.BudgetEstimate{
		WeeklyTokens: int64(cfg.GetProviderBudget(provName)),
		Source:       "config",
	}
	if source != nil {
		if resolved, err := source.GetBudget(provName); err == nil && resolved.WeeklyTokens > 0 {
			estimate = resolved
			if estimate.Source == "" {
				estimate.Source = "calibrated"
			}
		}
	}
	weeklyBudget := estimate.WeeklyTokens

	maxPercent := cfg.Budget.MaxPercent
	if maxPercent <= 0 {
		maxPercent = config.DefaultMaxPercent
	}
	reservePercent := cfg.Budget.ReservePercent
	if reservePercent < 0 {
		reservePercent = config.DefaultReservePercent
	}

	fmt.Printf("[%s]\n", provName)

	if result.Mode == "daily" {
		dailyBudget := weeklyBudget / 7
		usedTokens := int64(float64(dailyBudget) * result.UsedPercent / 100)
		remaining := dailyBudget - usedTokens

		fmt.Printf("  Mode:         %s\n", result.Mode)
		fmt.Printf("  Weekly:       %s tokens%s\n", formatTokens64(weeklyBudget), formatBudgetMeta(estimate))
		fmt.Printf("  Daily:        %s tokens\n", formatTokens64(dailyBudget))

		usedLine := fmt.Sprintf("  Used today:   %s (%.1f%%)", formatTokens64(usedTokens), result.UsedPercent)
		if claudeApprox {
			usedLine += "  [approx: JSONL fallback]"
		}
		if result.UsedPercent == 0 && (estimate.Confidence == "low" || estimate.Confidence == "medium") {
			usedLine += fmt.Sprintf("  [limited data — %d samples]", estimate.SampleCount)
		}
		fmt.Println(usedLine)

		if remaining >= 0 {
			fmt.Printf("  Remaining:    %s tokens\n", formatTokens64(remaining))
		} else {
			fmt.Printf("  Over by:      %s tokens\n", formatTokens64(-remaining))
		}

		if provName == "codex" && codex != nil {
			printCodexBreakdown(codex)
		}

		if remaining <= 0 {
			fmt.Printf("  Nightshift:   budget exceeded — 0 tokens available\n")
		} else {
			if result.PredictedUsage > 0 {
				fmt.Printf("  Daytime:      %s tokens reserved\n", formatTokens64(result.PredictedUsage))
			}
			fmt.Printf("  Reserve:      %s tokens\n", formatTokens64(result.ReserveAmount))

			preReserve := remaining * int64(maxPercent) / 100
			reserve := dailyBudget * int64(reservePercent) / 100
			if result.PredictedUsage > 0 {
				fmt.Printf("  Nightshift:   %s remaining × %d%% max = %s − %s reserve − %s daytime = %s available\n",
					formatTokens64(remaining), maxPercent, formatTokens64(preReserve),
					formatTokens64(reserve), formatTokens64(result.PredictedUsage),
					formatTokens64(result.Allowance))
				fmt.Printf("  Tonight:      %s remaining × %d%% max = %s − %s reserve = %s if daytime stays flat\n",
					formatTokens64(remaining), maxPercent, formatTokens64(preReserve),
					formatTokens64(reserve), formatTokens64(result.AllowanceNoDaytime))
			} else {
				fmt.Printf("  Nightshift:   %s remaining × %d%% max = %s − %s reserve = %s available\n",
					formatTokens64(remaining), maxPercent, formatTokens64(preReserve),
					formatTokens64(reserve), formatTokens64(result.Allowance))
			}
		}
	} else {
		usedTokens := int64(float64(weeklyBudget) * result.UsedPercent / 100)
		remaining := weeklyBudget - usedTokens

		fmt.Printf("  Mode:         %s\n", result.Mode)
		fmt.Printf("  Weekly:       %s tokens%s\n", formatTokens64(weeklyBudget), formatBudgetMeta(estimate))

		usedLine := fmt.Sprintf("  Used:         %s (%.1f%%)", formatTokens64(usedTokens), result.UsedPercent)
		if claudeApprox {
			usedLine += "  [approx: JSONL fallback]"
		}
		if result.UsedPercent == 0 && (estimate.Confidence == "low" || estimate.Confidence == "medium") {
			usedLine += fmt.Sprintf("  [limited data — %d samples]", estimate.SampleCount)
		}
		fmt.Println(usedLine)

		if remaining >= 0 {
			fmt.Printf("  Remaining:    %s tokens\n", formatTokens64(remaining))
		} else {
			fmt.Printf("  Over by:      %s tokens\n", formatTokens64(-remaining))
		}
		fmt.Printf("  Days left:    %d\n", result.RemainingDays)

		if provName == "codex" && codex != nil {
			printCodexBreakdown(codex)
		}

		if remaining <= 0 {
			fmt.Printf("  Nightshift:   budget exceeded — 0 tokens available\n")
		} else {
			if result.PredictedUsage > 0 {
				fmt.Printf("  Daytime:      %s tokens reserved\n", formatTokens64(result.PredictedUsage))
			}

			if result.Multiplier > 1.0 {
				fmt.Printf("  Multiplier:   %.1fx (end-of-week)\n", result.Multiplier)
			}

			fmt.Printf("  Reserve:      %s tokens\n", formatTokens64(result.ReserveAmount))

			days := result.RemainingDays
			if days <= 0 {
				days = 1
			}
			perDay := remaining / int64(days)
			preReserve := perDay * int64(maxPercent) / 100
			reserve := result.ReserveAmount
			if result.PredictedUsage > 0 {
				fmt.Printf("  Nightshift:   %s remaining × %d%% max = %s − %s reserve − %s daytime = %s available\n",
					formatTokens64(perDay), maxPercent, formatTokens64(preReserve),
					formatTokens64(reserve), formatTokens64(result.PredictedUsage),
					formatTokens64(result.Allowance))
				fmt.Printf("  Tonight:      %s remaining × %d%% max = %s − %s reserve = %s if daytime stays flat\n",
					formatTokens64(perDay), maxPercent, formatTokens64(preReserve),
					formatTokens64(reserve), formatTokens64(result.AllowanceNoDaytime))
			} else {
				fmt.Printf("  Nightshift:   %s remaining × %d%% max = %s − %s reserve = %s available\n",
					formatTokens64(perDay), maxPercent, formatTokens64(preReserve),
					formatTokens64(reserve), formatTokens64(result.Allowance))
			}
		}
	}

	if snapCollector != nil {
		if latest, err := snapCollector.GetLatest(provName, 1); err == nil && len(latest) > 0 {
			resetLine := formatResetLine(latest[0].SessionResetTime, latest[0].WeeklyResetTime)
			if resetLine != "" {
				fmt.Printf("  Resets:       %s\n", resetLine)
			}
		}
	}

	periodLabel := "this week"
	if result.Mode == "daily" {
		periodLabel = "today"
	}
	fmt.Printf("  Budget used:  %s %s\n", progressBar(result.UsedPercent, 30), periodLabel)

	printTokenAccountingNote(provName, estimate)

	return nil
}

// printTokenAccountingNote adds a brief note about how tokens are counted.
func printTokenAccountingNote(provider string, estimate budget.BudgetEstimate) {
	if estimate.Source != "calibrated" && estimate.Source != "scraped" {
		return
	}
	switch provider {
	case "claude":
		fmt.Printf("  Note:         tokens from stats-cache.json; JSONL fallback if cache missing\n")
	case "codex":
		fmt.Printf("  Note:         %% from rate limit; tokens = billable (excludes cached input)\n")
	}
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

func formatBudgetMeta(estimate budget.BudgetEstimate) string {
	if estimate.Source == "" {
		return ""
	}

	parts := []string{estimate.Source}
	if estimate.Confidence != "" {
		parts = append(parts, fmt.Sprintf("%s confidence", estimate.Confidence))
	}
	if estimate.SampleCount > 0 {
		parts = append(parts, fmt.Sprintf("%d samples", estimate.SampleCount))
	}

	return " (" + strings.Join(parts, ", ") + ")"
}

// formatResetLine builds the "Resets:" display from scraped reset time strings.
// Returns empty string if no reset times are available.
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

// printCodexBreakdown shows rate limit and local token data side by side.
func printCodexBreakdown(codex *providers.Codex) {
	bd := codex.GetUsageBreakdown()

	var rlParts []string
	if bd.PrimaryPct > 0 {
		rlParts = append(rlParts, fmt.Sprintf("%.0f%% primary (5h)", bd.PrimaryPct))
	}
	if bd.WeeklyPct > 0 {
		rlParts = append(rlParts, fmt.Sprintf("%.0f%% weekly", bd.WeeklyPct))
	}
	if len(rlParts) > 0 {
		fmt.Printf("  Rate limit:   %s\n", strings.Join(rlParts, " · "))
	}

	var localParts []string
	if bd.TodayTokens != nil && bd.TodayTokens.TotalTokens > 0 {
		localParts = append(localParts, fmt.Sprintf("%s today", formatTokens64(bd.TodayTokens.TotalTokens)))
	}
	if bd.WeeklyTokens != nil && bd.WeeklyTokens.TotalTokens > 0 {
		localParts = append(localParts, fmt.Sprintf("%s this week", formatTokens64(bd.WeeklyTokens.TotalTokens)))
	}
	if len(localParts) > 0 {
		fmt.Printf("  Local tokens: %s (billable)\n", strings.Join(localParts, " · "))
	}
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
