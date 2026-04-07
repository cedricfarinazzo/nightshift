package commands

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/marcus/nightshift/internal/agents"
	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/jira"
	"github.com/marcus/nightshift/internal/logging"
	"github.com/spf13/cobra"
)

var jiraRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run Jira autonomous cycle",
	Long: `Fetch TODO and ON REVIEW tickets from Jira, validate, implement, create PRs,
and handle review feedback in a single run cycle.

TODO tickets:      validate → plan → implement → commit → PR → ON REVIEW
ON REVIEW tickets: fetch PR feedback → re-work → push`,
	RunE: runJira,
}

func init() {
	jiraCmd.AddCommand(jiraRunCmd)

	jiraRunCmd.Flags().Int("max-tickets", 0, "Override max tickets per run (0 = use config)")
	jiraRunCmd.Flags().String("ticket", "", "Process a single ticket by key (e.g., VC-123)")
	jiraRunCmd.Flags().Bool("skip-validation", false, "Skip LLM ticket validation step")
	jiraRunCmd.Flags().Bool("todo-only", false, "Only process TODO tickets (skip feedback loop)")
	jiraRunCmd.Flags().Bool("review-only", false, "Only process ON REVIEW feedback (skip TODO)")
}

func runJira(cmd *cobra.Command, _ []string) error {
	log := logging.Component("jira")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.Jira.Defaults()
	if err := cfg.Jira.Validate(); err != nil {
		return fmt.Errorf("jira config: %w", err)
	}

	if v, _ := cmd.Flags().GetInt("max-tickets"); v > 0 {
		cfg.Jira.MaxTickets = v
	}
	skipValidation, _ := cmd.Flags().GetBool("skip-validation")
	todoOnly, _ := cmd.Flags().GetBool("todo-only")
	reviewOnly, _ := cmd.Flags().GetBool("review-only")
	singleTicket, _ := cmd.Flags().GetString("ticket")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	client, err := jira.NewClient(cfg.Jira)
	if err != nil {
		return fmt.Errorf("jira client: %w", err)
	}
	if err := client.Ping(ctx); err != nil {
		return fmt.Errorf("jira connection: %w", err)
	}

	statusMap, err := client.DiscoverStatuses(ctx)
	if err != nil {
		return fmt.Errorf("discover statuses: %w", err)
	}

	validationAgent := createJiraAgent(cfg.Jira.Validation)
	implAgent := createJiraAgent(cfg.Jira.Implement)
	reviewFixAgent := createJiraAgent(cfg.Jira.ReviewFix)

	var orchOpts []jira.OrchestratorOption
	if !skipValidation {
		orchOpts = append(orchOpts, jira.WithValidationAgent(validationAgent))
	}
	orchOpts = append(orchOpts,
		jira.WithImplAgent(implAgent),
		jira.WithReviewFixAgent(reviewFixAgent),
	)
	orch := jira.NewOrchestrator(client, cfg.Jira, orchOpts...)

	printJiraPreflightSummary(cfg.Jira, statusMap)

	var results []jira.TicketResult
	var feedbackResults []jira.FeedbackResult
	start := time.Now()

	if singleTicket != "" {
		// Single-ticket mode: fetch all todo+review tickets and find the one requested.
		todoTickets, err := client.FetchTodoTickets(ctx)
		if err != nil {
			return fmt.Errorf("fetch tickets: %w", err)
		}
		reviewTickets, err := client.FetchReviewTickets(ctx, statusMap)
		if err != nil {
			return fmt.Errorf("fetch review tickets: %w", err)
		}
		for _, t := range todoTickets {
			if t.Key == singleTicket {
				ws, err := jira.SetupWorkspace(ctx, cfg.Jira, t.Key)
				if err != nil {
					log.Errorf("workspace setup: %v", err)
					results = append(results, jira.TicketResult{TicketKey: t.Key, Status: jira.TicketFailed, Error: err.Error()})
					break
				}
				result, err := orch.ProcessTicket(ctx, t, ws)
				if err != nil {
					log.Errorf("process ticket %s: %v", t.Key, err)
				}
				if result != nil {
					results = append(results, *result)
				}
				break
			}
		}
		for _, t := range reviewTickets {
			if t.Key == singleTicket {
				ws, err := jira.SetupWorkspace(ctx, cfg.Jira, t.Key)
				if err != nil {
					log.Errorf("workspace setup: %v", err)
					break
				}
				result, err := orch.ProcessFeedback(ctx, t, ws)
				if err != nil {
					log.Errorf("process feedback %s: %v", t.Key, err)
				}
				if result != nil {
					feedbackResults = append(feedbackResults, *result)
				}
				break
			}
		}
	} else {
		// Phase A: TODO tickets.
		if !reviewOnly {
			todoTickets, err := client.FetchTodoTickets(ctx)
			if err != nil {
				return fmt.Errorf("fetch todo tickets: %w", err)
			}
			log.Infof("todo tickets: %d found", len(todoTickets))

			graph := jira.BuildDependencyGraph(todoTickets)
			ready, blocked := graph.ResolveOrder()
			for _, b := range blocked {
				log.Infof("ticket %s blocked by %v, skipping", b.Ticket.Key, b.Blockers)
			}

			count := 0
			for _, ticket := range ready {
				if count >= cfg.Jira.MaxTickets {
					break
				}
				ws, err := jira.SetupWorkspace(ctx, cfg.Jira, ticket.Key)
				if err != nil {
					log.Errorf("workspace %s: %v", ticket.Key, err)
					results = append(results, jira.TicketResult{TicketKey: ticket.Key, Status: jira.TicketFailed, Error: err.Error()})
					count++
					continue
				}
				result, err := orch.ProcessTicket(ctx, ticket, ws)
				if err != nil {
					log.Errorf("process %s: %v", ticket.Key, err)
				}
				if result != nil {
					results = append(results, *result)
				}
				count++
			}
		}

		// Phase B: ON REVIEW feedback.
		if !todoOnly {
			reviewTickets, err := client.FetchReviewTickets(ctx, statusMap)
			if err != nil {
				return fmt.Errorf("fetch review tickets: %w", err)
			}
			log.Infof("review tickets: %d found", len(reviewTickets))

			for _, ticket := range reviewTickets {
				ws, err := jira.SetupWorkspace(ctx, cfg.Jira, ticket.Key)
				if err != nil {
					log.Errorf("workspace %s: %v", ticket.Key, err)
					continue
				}
				result, err := orch.ProcessFeedback(ctx, ticket, ws)
				if err != nil {
					log.Errorf("process feedback %s: %v", ticket.Key, err)
				}
				if result != nil {
					feedbackResults = append(feedbackResults, *result)
				}
			}
		}
	}

	if n, err := jira.CleanupStaleWorkspaces(cfg.Jira); err != nil {
		log.Errorf("workspace cleanup: %v", err)
	} else if n > 0 {
		log.Infof("cleaned up %d stale workspaces", n)
	}

	printJiraRunSummary(results, feedbackResults, time.Since(start))
	return nil
}

// createJiraAgent creates an agent for the given Jira phase config.
// Defaults to Claude when the provider is empty or unrecognized.
func createJiraAgent(phase jira.PhaseConfig) agents.Agent {
	timeout, _ := time.ParseDuration(phase.Timeout)

	switch phase.Provider {
	case "codex":
		opts := []agents.CodexOption{}
		if phase.Model != "" {
			opts = append(opts, agents.WithCodexModel(phase.Model))
		}
		if timeout > 0 {
			opts = append(opts, agents.WithCodexDefaultTimeout(timeout))
		}
		return agents.NewCodexAgent(opts...)
	case "copilot":
		opts := []agents.CopilotOption{}
		if phase.Model != "" {
			opts = append(opts, agents.WithCopilotModel(phase.Model))
		}
		if timeout > 0 {
			opts = append(opts, agents.WithCopilotDefaultTimeout(timeout))
		}
		return agents.NewCopilotAgent(opts...)
	default: // "claude" or empty
		opts := []agents.ClaudeOption{}
		if phase.Model != "" {
			opts = append(opts, agents.WithModel(phase.Model))
		}
		if timeout > 0 {
			opts = append(opts, agents.WithDefaultTimeout(timeout))
		}
		return agents.NewClaudeAgent(opts...)
	}
}

func printJiraPreflightSummary(cfg jira.JiraConfig, _ *jira.StatusMap) {
	fmt.Println("🌙 Nightshift Jira Run")
	fmt.Println("──────────────────────────────")
	fmt.Printf("  Site:         %s.atlassian.net\n", cfg.Site)
	fmt.Printf("  Project:      %s\n", cfg.Project)
	fmt.Printf("  Label:        %s\n", cfg.Label)
	fmt.Printf("  Validation:   %s/%s\n", cfg.Validation.Provider, cfg.Validation.Model)
	fmt.Printf("  Implement:    %s/%s\n", cfg.Implement.Provider, cfg.Implement.Model)
	fmt.Printf("  ReviewFix:    %s/%s\n", cfg.ReviewFix.Provider, cfg.ReviewFix.Model)
	fmt.Printf("  Max tickets:  %d\n", cfg.MaxTickets)
}

func printJiraRunSummary(results []jira.TicketResult, feedback []jira.FeedbackResult, d time.Duration) {
	completed, rejected, failed := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case jira.TicketCompleted:
			completed++
		case jira.TicketRejected:
			rejected++
		default:
			failed++
		}
	}

	fmt.Println("\n🌙 Nightshift Jira Run Complete")
	fmt.Println("──────────────────────────────")
	fmt.Printf("  Duration:     %s\n", d.Round(time.Second))
	fmt.Printf("  Tickets:      %d processed\n", len(results))
	fmt.Printf("  ✅ Completed: %d\n", completed)
	fmt.Printf("  ❌ Rejected:  %d\n", rejected)
	if failed > 0 {
		fmt.Printf("  ⚠️  Failed:   %d\n", failed)
	}
	if len(feedback) > 0 {
		reworked := 0
		for _, f := range feedback {
			if f.FixesMade > 0 {
				reworked++
			}
		}
		fmt.Printf("  🔄 Reworked: %d\n", reworked)
	}
}
