package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/marcus/nightshift/internal/agents"
	"github.com/marcus/nightshift/internal/budget"
	"github.com/marcus/nightshift/internal/db"
	"github.com/marcus/nightshift/internal/jira"
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
	jiraPreviewCmd.Flags().String("label", "", "Jira label filter (overrides config, default \"nightshift\")")
	jiraPreviewCmd.Flags().Bool("json", false, "Output as JSON")
	jiraPreviewCmd.Flags().Bool("plain", false, "Disable TUI pager")
	jiraPreviewCmd.Flags().Bool("validate", false, "Run LLM validation on each ticket (costs tokens)")
	jiraPreviewCmd.Flags().Bool("explain", false, "Show detailed budget breakdown")
	jiraPreviewCmd.Flags().String("type", "", "Filter tickets by issue type (e.g. Bug, Story)")
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
	ExecutionOrder []string                `json:"execution_order"` // ready tickets only
	FullOrder      []jiraPreviewOrderEntry `json:"full_order,omitempty"`
	BlockedTickets []jiraPreviewBlocked    `json:"blocked_tickets,omitempty"`
	Budget         *budget.AllowanceResult `json:"budget,omitempty"`
	BudgetErr      string                  `json:"budget_err,omitempty"`
	SkippedTickets []jiraPreviewSkipped    `json:"skipped_tickets,omitempty"`
	Phases         []jiraPreviewPhase      `json:"phases"`
}

type jiraPreviewOrderEntry struct {
	Key     string `json:"key"`
	Ready   bool   `json:"ready"`
	Reason  string `json:"reason,omitempty"`  // non-empty when not ready
	Blocker string `json:"blocker,omitempty"` // first blocker when not ready
}

type jiraPreviewPhase struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Timeout  string `json:"timeout,omitempty"`
}

type jiraPreviewTicket struct {
	Key             string   `json:"key"`
	Summary         string   `json:"summary"`
	Status          string   `json:"status"`
	IssueType       string   `json:"issue_type,omitempty"`
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
	projectFilter, _ := cmd.Flags().GetString("project")
	labelOverride, _ := cmd.Flags().GetString("label")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	plainOutput, _ := cmd.Flags().GetBool("plain")
	explain, _ := cmd.Flags().GetBool("explain")
	runValidate, _ := cmd.Flags().GetBool("validate")
	typeFilter, _ := cmd.Flags().GetString("type")

	cfg, err := loadConfig("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.Jira.Defaults()

	// Apply --label override to all projects.
	if labelOverride != "" {
		for i := range cfg.Jira.Projects {
			cfg.Jira.Projects[i].Label = labelOverride
		}
	}

	// Filter projects with --project flag.
	projects := cfg.Jira.Projects
	if projectFilter != "" {
		var filtered []jira.ProjectConfig
		for _, p := range projects {
			if strings.EqualFold(p.Key, projectFilter) {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no project with key %q found in config", projectFilter)
		}
		projects = filtered
	}

	// Validate the (potentially filtered) config.
	cfg.Jira.Projects = projects
	if err := cfg.Jira.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Build comma-joined project keys for display/JSON.
	keys := make([]string, len(projects))
	for i, p := range projects {
		keys[i] = p.Key
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Build phases using effective configs. When multiple projects are configured
	// each may have different overrides; we show the first project's effective
	// values as a representative (or global defaults when projects is empty).
	buildPhases := func() []jiraPreviewPhase {
		if len(projects) == 0 {
			return nil
		}
		p := projects[0]
		jc := cfg.Jira
		return []jiraPreviewPhase{
			{Name: "validation", Provider: jc.EffectiveValidation(p).Provider, Model: jc.EffectiveValidation(p).Model, Timeout: jc.EffectiveValidation(p).Timeout},
			{Name: "plan", Provider: jc.EffectivePlan(p).Provider, Model: jc.EffectivePlan(p).Model, Timeout: jc.EffectivePlan(p).Timeout},
			{Name: "implement", Provider: jc.EffectiveImplement(p).Provider, Model: jc.EffectiveImplement(p).Model, Timeout: jc.EffectiveImplement(p).Timeout},
			{Name: "review_fix", Provider: jc.EffectiveReviewFix(p).Provider, Model: jc.EffectiveReviewFix(p).Model, Timeout: jc.EffectiveReviewFix(p).Timeout},
		}
	}

	result := &jiraPreviewResult{
		GeneratedAt: time.Now(),
		JiraProject: strings.Join(keys, ", "),
		Phases:      buildPhases(),
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

	// Fetch tickets from each project if connection succeeded.
	if result.ConnectionOK {
		statusMap, err := client.DiscoverStatuses(ctx)
		if err != nil {
			result.SkippedTickets = append(result.SkippedTickets, jiraPreviewSkipped{
				Key:    "*",
				Reason: fmt.Sprintf("discover statuses: %v", err),
			})
		} else {
			for _, proj := range projects {
				// Optionally build a per-project validation agent using effective config.
				var valAgent agents.Agent
				if runValidate {
					a, agentErr := createJiraAgent(cfg, cfg.Jira.EffectiveValidation(proj))
					if agentErr != nil {
						result.SkippedTickets = append(result.SkippedTickets, jiraPreviewSkipped{
							Key:    proj.Key + ":*",
							Reason: fmt.Sprintf("create validation agent: %v", agentErr),
						})
					} else {
						valAgent = a
					}
				}

				todoTickets, err := client.FetchTodoTickets(ctx, proj)
				if err != nil {
					result.SkippedTickets = append(result.SkippedTickets, jiraPreviewSkipped{
						Key:    proj.Key + ":*",
						Reason: fmt.Sprintf("fetch todo tickets: %v", err),
					})
					continue
				}
				inProgressTickets, ipErr := client.FetchInProgressTickets(ctx, proj, statusMap)
				if ipErr != nil {
					result.SkippedTickets = append(result.SkippedTickets, jiraPreviewSkipped{
						Key:    proj.Key + ":*",
						Reason: fmt.Sprintf("fetch in-progress tickets: %v", ipErr),
					})
				} else {
					todoTickets = append(todoTickets, inProgressTickets...)
				}
				// Apply optional issue-type filter.
				if typeFilter != "" {
					filtered := todoTickets[:0]
					for _, t := range todoTickets {
						if strings.EqualFold(t.IssueType, typeFilter) {
							filtered = append(filtered, t)
						}
					}
					todoTickets = filtered
				}

				graph := jira.BuildDependencyGraph(todoTickets)
				ready, blocked := graph.ResolveOrder()

				for _, t := range ready {
					result.ExecutionOrder = append(result.ExecutionOrder, t.Key)
					result.FullOrder = append(result.FullOrder, jiraPreviewOrderEntry{Key: t.Key, Ready: true})
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
					blocker := ""
					if len(bt.Blockers) > 0 {
						blocker = bt.Blockers[0]
					}
					result.FullOrder = append(result.FullOrder, jiraPreviewOrderEntry{
						Key:     bt.Ticket.Key,
						Ready:   false,
						Reason:  bt.Reason,
						Blocker: blocker,
					})
					result.BlockedTickets = append(result.BlockedTickets, jiraPreviewBlocked{
						Key:      bt.Ticket.Key,
						Reason:   bt.Reason,
						Blockers: bt.Blockers,
					})
				}

				reviewTickets, err := client.FetchReviewTickets(ctx, proj, statusMap)
				if err != nil {
					result.SkippedTickets = append(result.SkippedTickets, jiraPreviewSkipped{
						Key:    proj.Key + ":*review*",
						Reason: fmt.Sprintf("fetch review tickets: %v", err),
					})
				} else {
					for _, t := range reviewTickets {
						result.ReviewTickets = append(result.ReviewTickets, buildJiraPreviewTicket(t))
					}
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
			budgetMgr := newBudgetManager(cfg, database)

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
		IssueType:  t.IssueType,
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
