package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/signal"
	"syscall"
	"time"

	"github.com/marcus/nightshift/internal/budget"
	"github.com/marcus/nightshift/internal/calibrator"
	"github.com/marcus/nightshift/internal/db"
	"github.com/marcus/nightshift/internal/jira"
	"github.com/marcus/nightshift/internal/providers"
	"github.com/marcus/nightshift/internal/trends"
	"github.com/spf13/cobra"
)

var jiraPreviewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Preview what nightshift jira run would do",
	Long: `Dry-run the Jira autonomous pipeline. Shows tickets, dependencies,
validation status, and budget without executing anything.`,
	RunE: runJiraPreview,
}

func init() {
	jiraPreviewCmd.Flags().StringP("project", "p", "", "Jira project key (overrides config)")
	jiraPreviewCmd.Flags().Bool("json", false, "Output as JSON")
	jiraPreviewCmd.Flags().Bool("plain", false, "Disable TUI pager")
	jiraPreviewCmd.Flags().Bool("validate", false, "Run LLM validation on each ticket (costs tokens)")
	jiraPreviewCmd.Flags().Bool("explain", false, "Show detailed budget and filtering explanations")
	jiraCmd.AddCommand(jiraPreviewCmd)
}

type jiraPreviewResult struct {
	GeneratedAt    time.Time
	JiraProject    string
	JiraUser       string
	ConnectionOK   bool
	ConnectionErr  string
	TodoTickets    []jiraPreviewTicket
	ReviewTickets  []jiraPreviewTicket
	ExecutionOrder []string // ticket keys in topo sort order
	BlockedTickets []jiraPreviewBlocked
	Budget         *budget.AllowanceResult
	BudgetErr      string
	SkippedTickets []jiraPreviewSkipped
	Phases         map[string]string // phase name → provider
}

type jiraPreviewTicket struct {
	Key          string
	Summary      string
	Status       string
	Dependencies []string // blocked-by keys
	Blocks       []string
	BranchName   string
	Phases       map[string]string // phase → provider
	CostTier     string
}

type jiraPreviewBlocked struct {
	Key      string
	Reason   string
	Blockers []string
}

type jiraPreviewSkipped struct {
	Key    string
	Reason string
}

func runJiraPreview(cmd *cobra.Command, _ []string) error {
	projectOverride, _ := cmd.Flags().GetString("project")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	plainOutput, _ := cmd.Flags().GetBool("plain")
	explain, _ := cmd.Flags().GetBool("explain")

	cfg, err := loadConfig("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.Jira.Defaults()

	if projectOverride != "" {
		cfg.Jira.Project = projectOverride
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	result := &jiraPreviewResult{
		GeneratedAt: time.Now(),
		JiraProject: cfg.Jira.Project,
		Phases: map[string]string{
			"validation": cfg.Jira.Validation.Provider,
			"plan":       cfg.Jira.Plan.Provider,
			"implement":  cfg.Jira.Implement.Provider,
			"review_fix": cfg.Jira.ReviewFix.Provider,
		},
	}

	// Connect to Jira.
	client, err := jira.NewClient(cfg.Jira)
	if err != nil {
		result.ConnectionErr = err.Error()
	} else if pingErr := client.Ping(ctx); pingErr != nil {
		result.ConnectionErr = pingErr.Error()
	} else {
		result.ConnectionOK = true
		result.JiraUser = cfg.Jira.Email
	}

	// Fetch tickets if connection succeeded.
	if result.ConnectionOK {
		statusMap, err := client.DiscoverStatuses(ctx)
		if err != nil {
			result.SkippedTickets = append(result.SkippedTickets, jiraPreviewSkipped{
				Key:    "*",
				Reason: fmt.Sprintf("discover statuses: %v", err),
			})
		} else {
			todoTickets, err := client.FetchTodoTickets(ctx)
			if err != nil {
				result.SkippedTickets = append(result.SkippedTickets, jiraPreviewSkipped{
					Key:    "*",
					Reason: fmt.Sprintf("fetch todo tickets: %v", err),
				})
			} else {
				graph := jira.BuildDependencyGraph(todoTickets)
				ready, blocked := graph.ResolveOrder()

				phases := result.Phases
				for _, t := range ready {
					result.ExecutionOrder = append(result.ExecutionOrder, t.Key)
					result.TodoTickets = append(result.TodoTickets, buildJiraPreviewTicket(t, phases))
				}
				for _, bt := range blocked {
					result.BlockedTickets = append(result.BlockedTickets, jiraPreviewBlocked{
						Key:      bt.Ticket.Key,
						Reason:   bt.Reason,
						Blockers: bt.Blockers,
					})
				}
			}

			reviewTickets, err := client.FetchReviewTickets(ctx, statusMap)
			if err != nil {
				result.SkippedTickets = append(result.SkippedTickets, jiraPreviewSkipped{
					Key:    "*review*",
					Reason: fmt.Sprintf("fetch review tickets: %v", err),
				})
			} else {
				phases := result.Phases
				for _, t := range reviewTickets {
					result.ReviewTickets = append(result.ReviewTickets, buildJiraPreviewTicket(t, phases))
				}
			}
		}
	}

	// Budget.
	if cfg.Jira.BudgetEnabled {
		database, dbErr := db.Open(cfg.ExpandedDBPath())
		if dbErr != nil {
			result.BudgetErr = fmt.Sprintf("open db: %v", dbErr)
		} else {
			defer func() { _ = database.Close() }()
			claudeProvider := providers.NewClaudeWithPath(cfg.ExpandedProviderPath("claude"))
			codexProvider := providers.NewCodexWithPath(cfg.ExpandedProviderPath("codex"))
			copilotProvider := providers.NewCopilotWithPath(cfg.ExpandedProviderPath("copilot"))
			cal := calibrator.New(database, cfg)
			trend := trends.NewAnalyzer(database, cfg.Budget.SnapshotRetentionDays)
			budgetMgr := budget.NewManagerFromProviders(cfg, claudeProvider, codexProvider, copilotProvider, budget.WithBudgetSource(cal), budget.WithTrendAnalyzer(trend))

			provider := cfg.Jira.Implement.Provider
			if provider == "" {
				provider = "claude"
			}
			allowance, budgetErr := budgetMgr.CalculateAllowance(provider)
			if budgetErr != nil {
				result.BudgetErr = budgetErr.Error()
			} else {
				result.Budget = allowance
			}
		}
	}

	if jsonOutput {
		return writeJiraPreviewJSON(cmd.OutOrStdout(), result)
	}

	text := renderJiraPreviewText(result, jiraPreviewTextOptions{Explain: explain})
	return writePreviewText(cmd.OutOrStdout(), text, previewPagerOptions{Plain: plainOutput})
}

func buildJiraPreviewTicket(t jira.Ticket, phases map[string]string) jiraPreviewTicket {
	pt := jiraPreviewTicket{
		Key:        t.Key,
		Summary:    t.Summary,
		Status:     t.Status.Name,
		BranchName: jira.BranchName(t.Key),
		Phases:     phases,
		CostTier:   "Medium",
	}
	for _, link := range t.IssueLinks {
		if link.Type != "Blocks" {
			continue
		}
		if link.Direction == "inward" {
			pt.Dependencies = append(pt.Dependencies, link.InwardKey)
		}
		if link.Direction == "outward" {
			pt.Blocks = append(pt.Blocks, link.OutwardKey)
		}
	}
	return pt
}

// writeJiraPreviewJSON encodes the preview result as JSON.
func writeJiraPreviewJSON(w io.Writer, result *jiraPreviewResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
