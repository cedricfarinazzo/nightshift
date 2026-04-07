package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/signal"
	"syscall"
	"time"

	"github.com/marcus/nightshift/internal/agents"
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
execution order, and budget without executing anything.`,
	RunE: runJiraPreview,
}

func init() {
	jiraPreviewCmd.Flags().StringP("project", "p", "", "Jira project key (overrides config)")
	jiraPreviewCmd.Flags().Bool("json", false, "Output as JSON")
	jiraPreviewCmd.Flags().Bool("plain", false, "Disable TUI pager")
	jiraPreviewCmd.Flags().Bool("validate", false, "Run LLM validation on each ticket (costs tokens)")
	jiraPreviewCmd.Flags().Bool("explain", false, "Show detailed budget breakdown")
	jiraCmd.AddCommand(jiraPreviewCmd)
}

type jiraPreviewResult struct {
	GeneratedAt    time.Time               `json:"generated_at"`
	JiraProject    string                  `json:"jira_project"`
	JiraUser       string                  `json:"jira_user"`
	ConnectionOK   bool                    `json:"connection_ok"`
	ConnectionErr  string                  `json:"connection_err,omitempty"`
	TodoTickets    []jiraPreviewTicket     `json:"todo_tickets"`
	ReviewTickets  []jiraPreviewTicket     `json:"review_tickets"`
	ExecutionOrder []string                `json:"execution_order"`
	BlockedTickets []jiraPreviewBlocked    `json:"blocked_tickets,omitempty"`
	Budget         *budget.AllowanceResult `json:"budget,omitempty"`
	BudgetErr      string                  `json:"budget_err,omitempty"`
	SkippedTickets []jiraPreviewSkipped    `json:"skipped_tickets,omitempty"`
	Phases         map[string]string       `json:"phases"`
}

type jiraPreviewTicket struct {
	Key             string   `json:"key"`
	Summary         string   `json:"summary"`
	Status          string   `json:"status"`
	Dependencies    []string `json:"dependencies,omitempty"`
	Blocks          []string `json:"blocks,omitempty"`
	BranchName      string   `json:"branch_name"`
	ValidationScore *int     `json:"validation_score,omitempty"`
	ValidationMsg   string   `json:"validation_msg,omitempty"`
}

type jiraPreviewBlocked struct {
	Key      string   `json:"key"`
	Reason   string   `json:"reason"`
	Blockers []string `json:"blockers,omitempty"`
}

type jiraPreviewSkipped struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

func runJiraPreview(cmd *cobra.Command, _ []string) error {
	projectOverride, _ := cmd.Flags().GetString("project")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	plainOutput, _ := cmd.Flags().GetBool("plain")
	explain, _ := cmd.Flags().GetBool("explain")
	runValidate, _ := cmd.Flags().GetBool("validate")

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

	// Optionally prepare the validation agent up front.
	var valAgent agents.Agent
	if runValidate {
		a, agentErr := createJiraAgent(cfg, cfg.Jira.Validation)
		if agentErr != nil {
			result.SkippedTickets = append(result.SkippedTickets, jiraPreviewSkipped{
				Key:    "*",
				Reason: fmt.Sprintf("create validation agent: %v", agentErr),
			})
		} else {
			valAgent = a
		}
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

				for _, t := range ready {
					result.ExecutionOrder = append(result.ExecutionOrder, t.Key)
					pt := buildJiraPreviewTicket(t)
					if valAgent != nil {
						vr, vErr := jira.ValidateTicket(ctx, valAgent, t)
						if vErr == nil && vr != nil {
							score := vr.Score
							pt.ValidationScore = &score
							if !vr.Valid {
								pt.ValidationMsg = fmt.Sprintf("score %d/10 — below threshold", vr.Score)
							}
						}
					}
					result.TodoTickets = append(result.TodoTickets, pt)
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
				for _, t := range reviewTickets {
					result.ReviewTickets = append(result.ReviewTickets, buildJiraPreviewTicket(t))
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

func buildJiraPreviewTicket(t jira.Ticket) jiraPreviewTicket {
	pt := jiraPreviewTicket{
		Key:        t.Key,
		Summary:    t.Summary,
		Status:     t.Status.Name,
		BranchName: jira.BranchName(t.Key),
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
