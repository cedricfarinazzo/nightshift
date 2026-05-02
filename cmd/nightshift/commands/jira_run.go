package commands

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/marcus/nightshift/internal/agents"
	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/db"
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

	jiraRunCmd.Flags().String("label", "", "Jira label filter (overrides config, default \"nightshift\")")
	jiraRunCmd.Flags().Int("max-tickets", 0, "Override max tickets per run (0 = use config)")
	jiraRunCmd.Flags().String("ticket", "", "Process a single ticket by key (e.g., VC-123)")
	jiraRunCmd.Flags().Bool("skip-validation", false, "Skip LLM ticket validation step")
	jiraRunCmd.Flags().Bool("todo-only", false, "Only process TODO tickets (skip feedback loop)")
	jiraRunCmd.Flags().Bool("review-only", false, "Only process ON REVIEW feedback (skip TODO)")
}

func runJira(cmd *cobra.Command, _ []string) error {
	log := logging.Component("jira")

	cfg, err := loadConfig("")
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
	// Apply --label override to all projects.
	if v, _ := cmd.Flags().GetString("label"); v != "" {
		for i := range cfg.Jira.Projects {
			cfg.Jira.Projects[i].Label = v
		}
	}
	skipValidation, _ := cmd.Flags().GetBool("skip-validation")
	todoOnly, _ := cmd.Flags().GetBool("todo-only")
	reviewOnly, _ := cmd.Flags().GetBool("review-only")
	singleTicket, _ := cmd.Flags().GetString("ticket")

	if todoOnly && reviewOnly {
		return fmt.Errorf("--todo-only and --review-only are mutually exclusive")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	runID := uuid.New().String()
	runDB, dbErr := openDB()
	if dbErr != nil {
		log.Errorf("open db (run history disabled): %v", dbErr)
		runDB = nil
	}
	if runDB != nil {
		defer func() { _ = runDB.Close() }()
		projectKey := firstProjectKey(cfg.Jira)
		// When multiple projects run, use "multi" to avoid misleading metadata.
		if len(cfg.Jira.Projects) > 1 {
			projectKey = "multi"
		}
		if err := runDB.SaveJiraRun(ctx, runID, projectKey, time.Now()); err != nil {
			log.Errorf("save jira run: %v", err)
		}
	}

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

	printJiraPreflightSummary(cfg.Jira, skipValidation, statusMap)

	var results []jira.TicketResult
	var feedbackResults []jira.FeedbackResult
	start := time.Now()

	if singleTicket != "" {
		found, err := runSingleTicket(ctx, log, client, cfg, statusMap, singleTicket, skipValidation, runDB, runID, &results, &feedbackResults)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("ticket %s not found in TODO or ON REVIEW lists", singleTicket)
		}
	} else {
		for _, proj := range cfg.Jira.Projects {
			orch, err := buildOrchestrator(client, cfg, proj, skipValidation, runDB, runID)
			if err != nil {
				return err
			}
			if !reviewOnly {
				if err := runTodoPhase(ctx, log, orch, client, cfg.Jira, proj, statusMap, &results); err != nil {
					return err
				}
			}
			if !todoOnly {
				if err := runReviewPhase(ctx, log, orch, client, cfg.Jira, proj, statusMap, &feedbackResults); err != nil {
					return err
				}
			}
		}
	}

	if runDB != nil {
		completed, failed := countResults(results)
		if err := runDB.UpdateJiraRun(ctx, runID, time.Now(), len(results), completed, failed); err != nil {
			log.Errorf("update jira run: %v", err)
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

// buildOrchestrator creates an Orchestrator for the given project,
// constructing agents from the project's effective phase configs.
func buildOrchestrator(client *jira.Client, cfg *config.Config, proj jira.ProjectConfig, skipValidation bool, runDB *db.DB, runID string) (*jira.Orchestrator, error) {
	jiracfg := cfg.Jira

	var validationAgent agents.Agent
	if !skipValidation {
		var err error
		validationAgent, err = createJiraAgent(cfg, jiracfg.EffectiveValidation(proj))
		if err != nil {
			return nil, fmt.Errorf("validation agent: %w", err)
		}
	}
	planAgent, err := createJiraAgent(cfg, jiracfg.EffectivePlan(proj))
	if err != nil {
		return nil, fmt.Errorf("plan agent: %w", err)
	}
	implAgent, err := createJiraAgent(cfg, jiracfg.EffectiveImplement(proj))
	if err != nil {
		return nil, fmt.Errorf("implement agent: %w", err)
	}
	reviewFixAgent, err := createJiraAgent(cfg, jiracfg.EffectiveReviewFix(proj))
	if err != nil {
		return nil, fmt.Errorf("review-fix agent: %w", err)
	}

	orchOpts := []jira.OrchestratorOption{
		jira.WithPlanAgent(planAgent),
		jira.WithImplAgent(implAgent),
		jira.WithReviewFixAgent(reviewFixAgent),
		jira.WithPhaseCallback(func(ticketKey string, phase jira.Phase, done bool) {
			if !done {
				switch phase {
				case jira.PhaseValidate:
					if skipValidation {
						fmt.Printf("    ⟳ validate      skipped (validation disabled)…\n")
					} else {
						fmt.Printf("    ⟳ validate      checking ticket quality…\n")
					}
				case jira.PhasePlan:
					fmt.Printf("    ⟳ plan          generating implementation plan…\n")
				case jira.PhaseImplement:
					fmt.Printf("    ⟳ implement     coding — this may take a while…\n")
				case jira.PhaseCommit:
					fmt.Printf("    ⟳ commit        committing changes…\n")
				case jira.PhasePR:
					fmt.Printf("    ⟳ pr            opening pull request…\n")
				case jira.PhaseStatus:
					fmt.Printf("    ⟳ status        updating Jira ticket…\n")
				default:
					fmt.Printf("    ⟳ %-12s …\n", phase)
				}
			}
		}),
		jira.WithProgressPrinter(func(format string, args ...any) {
			fmt.Printf("    "+format+"\n", args...)
		}),
	}
	if skipValidation {
		orchOpts = append(orchOpts, jira.WithSkipValidation())
	} else {
		orchOpts = append(orchOpts, jira.WithValidationAgent(validationAgent))
	}
	if runDB != nil && runID != "" {
		orchOpts = append(orchOpts, jira.WithDB(runDB, runID))
	}
	return jira.NewOrchestrator(client, jiracfg, proj, orchOpts...), nil
}

func runSingleTicket(
	ctx context.Context,
	log *logging.Logger,
	client *jira.Client,
	cfg *config.Config,
	statusMap *jira.StatusMap,
	key string,
	skipValidation bool,
	runDB *db.DB,
	runID string,
	results *[]jira.TicketResult,
	feedbackResults *[]jira.FeedbackResult,
) (found bool, err error) {
	jiracfg := cfg.Jira

	// Parse the project key prefix (e.g. "VC" from "VC-123") to avoid fetching
	// tickets from unrelated projects that may fail due to permissions or outages.
	keyPrefix, _, validKey := strings.Cut(key, "-")
	if !validKey {
		return false, fmt.Errorf("invalid ticket key %q: expected format PROJECT-NUMBER", key)
	}
	keyPrefix = strings.ToUpper(keyPrefix)

	for _, proj := range jiracfg.Projects {
		if !strings.EqualFold(proj.Key, keyPrefix) {
			continue
		}
		orch, err := buildOrchestrator(client, cfg, proj, skipValidation, runDB, runID)
		if err != nil {
			return false, err
		}

		todoTickets, err := client.FetchTodoTickets(ctx, proj)
		if err != nil {
			return false, fmt.Errorf("fetch tickets: %w", err)
		}
		inProgressTickets, err := client.FetchInProgressTickets(ctx, proj, statusMap)
		if err != nil {
			return false, fmt.Errorf("fetch in-progress tickets: %w", err)
		}
		reviewTickets, err := client.FetchReviewTickets(ctx, proj, statusMap)
		if err != nil {
			return false, fmt.Errorf("fetch review tickets: %w", err)
		}

		for _, t := range append(todoTickets, inProgressTickets...) {
			if t.Key != key {
				continue
			}
			found = true
			ws, err := jira.SetupWorkspace(ctx, jiracfg, proj, t.Key)
			if err != nil {
				log.Errorf("workspace setup: %v", err)
				*results = append(*results, jira.TicketResult{TicketKey: t.Key, Status: jira.TicketFailed, Error: err.Error()})
				return found, nil
			}
			result, err := orch.ProcessTicket(ctx, t, ws)
			if err != nil {
				log.Errorf("process ticket %s: %v", t.Key, err)
			}
			if result != nil {
				*results = append(*results, *result)
			}
			return found, nil
		}

		for _, t := range reviewTickets {
			if t.Key != key {
				continue
			}
			found = true
			ws, err := jira.SetupWorkspace(ctx, jiracfg, proj, t.Key)
			if err != nil {
				log.Errorf("workspace setup: %v", err)
				*feedbackResults = append(*feedbackResults, jira.FeedbackResult{TicketKey: t.Key, Error: err.Error()})
				return found, nil
			}
			result, err := orch.ProcessFeedback(ctx, t, ws)
			if err != nil {
				log.Errorf("process feedback %s: %v", t.Key, err)
			}
			if result != nil {
				*feedbackResults = append(*feedbackResults, *result)
			}
			return found, nil
		}
	}
	return false, nil
}

func runTodoPhase(
	ctx context.Context,
	log *logging.Logger,
	orch *jira.Orchestrator,
	client *jira.Client,
	jiracfg jira.JiraConfig,
	proj jira.ProjectConfig,
	statusMap *jira.StatusMap,
	results *[]jira.TicketResult,
) error {
	todoTickets, err := client.FetchTodoTickets(ctx, proj)
	if err != nil {
		return fmt.Errorf("fetch todo tickets: %w", err)
	}
	inProgressTickets, err := client.FetchInProgressTickets(ctx, proj, statusMap)
	if err != nil {
		return fmt.Errorf("fetch in-progress tickets: %w", err)
	}
	allTickets := append(todoTickets, inProgressTickets...)
	log.Infof("todo tickets [%s]: %d found (%d in-progress)", proj.Key, len(allTickets), len(inProgressTickets))

	graph := jira.BuildDependencyGraph(allTickets)
	ready, blocked := graph.ResolveOrder()
	for _, b := range blocked {
		log.Infof("ticket %s blocked by %v, skipping", b.Ticket.Key, b.Blockers)
		fmt.Printf("  ⏭  %s  blocked by %v\n", b.Ticket.Key, b.Blockers)
	}
	if len(ready) == 0 {
		fmt.Printf("  no tickets ready to process [%s]\n", proj.Key)
	}

	count := 0
	for _, ticket := range ready {
		if count >= jiracfg.MaxTickets {
			break
		}
		fmt.Printf("\n  ▶ %s  %s\n", ticket.Key, ticket.Summary)
		fmt.Printf("    setting up workspace…\n")
		ws, err := jira.SetupWorkspace(ctx, jiracfg, proj, ticket.Key)
		if err != nil {
			log.Errorf("workspace %s: %v", ticket.Key, err)
			fmt.Printf("    ✗ workspace setup failed: %v\n", err)
			*results = append(*results, jira.TicketResult{TicketKey: ticket.Key, Status: jira.TicketFailed, Error: err.Error()})
			count++
			continue
		}
		result, err := orch.ProcessTicket(ctx, ticket, ws)
		if err != nil {
			log.Errorf("process %s: %v", ticket.Key, err)
		}
		if result != nil {
			printTicketResult(result)
			*results = append(*results, *result)
		}
		count++
	}
	return nil
}

func runReviewPhase(
	ctx context.Context,
	log *logging.Logger,
	orch *jira.Orchestrator,
	client *jira.Client,
	jiracfg jira.JiraConfig,
	proj jira.ProjectConfig,
	statusMap *jira.StatusMap,
	feedbackResults *[]jira.FeedbackResult,
) error {
	reviewTickets, err := client.FetchReviewTickets(ctx, proj, statusMap)
	if err != nil {
		return fmt.Errorf("fetch review tickets: %w", err)
	}
	log.Infof("review tickets [%s]: %d found", proj.Key, len(reviewTickets))

	if len(reviewTickets) == 0 {
		fmt.Printf("  no tickets in review [%s]\n", proj.Key)
	}
	for _, ticket := range reviewTickets {
		fmt.Printf("\n  🔍 %s  %s\n", ticket.Key, ticket.Summary)
		fmt.Printf("    setting up workspace…\n")
		ws, err := jira.SetupWorkspace(ctx, jiracfg, proj, ticket.Key)
		if err != nil {
			log.Errorf("workspace %s: %v", ticket.Key, err)
			fmt.Printf("    ✗ workspace setup failed: %v\n", err)
			*feedbackResults = append(*feedbackResults, jira.FeedbackResult{TicketKey: ticket.Key, Error: err.Error()})
			continue
		}
		result, err := orch.ProcessFeedback(ctx, ticket, ws)
		if err != nil {
			log.Errorf("process feedback %s: %v", ticket.Key, err)
		}
		if result != nil {
			printFeedbackResult(result)
			*feedbackResults = append(*feedbackResults, *result)
		}
	}
	return nil
}

// createJiraAgent creates an agent for the given Jira phase config, applying
// both global provider settings (permissions, binary path) and phase-specific
// overrides (model, timeout). Returns an error if the provider CLI is not
// available or the timeout string is malformed.
func createJiraAgent(cfg *config.Config, phase jira.PhaseConfig) (agents.Agent, error) {
	provider := strings.ToLower(phase.Provider)
	if provider == "" {
		provider = "claude"
	}

	var timeout time.Duration
	if phase.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(phase.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout %q: %w", phase.Timeout, err)
		}
	}

	switch provider {
	case "codex":
		var extra []agents.CodexOption
		if m := phase.Model; m != "" {
			extra = append(extra, agents.WithCodexModel(m))
		}
		if timeout > 0 {
			extra = append(extra, agents.WithCodexDefaultTimeout(timeout))
		}
		a := newCodexAgentFromConfig(cfg, extra...)
		if !a.Available() {
			return nil, fmt.Errorf("codex CLI not found in PATH")
		}
		return a, nil

	case "copilot":
		var extra []agents.CopilotOption
		if m := phase.Model; m != "" {
			extra = append(extra, agents.WithCopilotModel(m))
		}
		if timeout > 0 {
			extra = append(extra, agents.WithCopilotDefaultTimeout(timeout))
		}
		a := newCopilotAgentFromConfig(cfg, "", extra...)
		if !a.Available() {
			return nil, fmt.Errorf("copilot CLI not found in PATH")
		}
		return a, nil

	default: // "claude" or unrecognized
		var extra []agents.ClaudeOption
		if m := phase.Model; m != "" {
			extra = append(extra, agents.WithModel(m))
		}
		if timeout > 0 {
			extra = append(extra, agents.WithDefaultTimeout(timeout))
		}
		a := newClaudeAgentFromConfig(cfg, extra...)
		if !a.Available() {
			return nil, fmt.Errorf("claude CLI not found in PATH")
		}
		return a, nil
	}
}

func printJiraPreflightSummary(cfg jira.JiraConfig, skipValidation bool, _ *jira.StatusMap) {
	fmt.Println("🌙 Nightshift Jira Run")
	fmt.Println("──────────────────────────────")
	fmt.Printf("  Site:         %s.atlassian.net\n", cfg.Site)
	fmt.Printf("  Max tickets:  %d\n", cfg.MaxTickets)
	for _, p := range cfg.Projects {
		fmt.Printf("  Project: %s  [label: %s]\n", p.Key, p.Label)
		if skipValidation {
			fmt.Printf("    Validation:   skipped\n")
		} else {
			v := cfg.EffectiveValidation(p)
			fmt.Printf("    Validation:   %s/%s\n", v.Provider, v.Model)
		}
		pl := cfg.EffectivePlan(p)
		im := cfg.EffectiveImplement(p)
		rv := cfg.EffectiveReviewFix(p)
		fmt.Printf("    Plan:         %s/%s\n", pl.Provider, pl.Model)
		fmt.Printf("    Implement:    %s/%s\n", im.Provider, im.Model)
		fmt.Printf("    ReviewFix:    %s/%s\n", rv.Provider, rv.Model)
	}
}

func printTicketResult(r *jira.TicketResult) {
	switch r.Status {
	case jira.TicketCompleted:
		fmt.Printf("    ✅ completed in %s", r.Duration.Round(time.Second))
		if len(r.PRURLs) > 0 {
			fmt.Printf("  →  %s", strings.Join(r.PRURLs, "  "))
		}
		fmt.Println()
	case jira.TicketRejected:
		fmt.Printf("    ❌ rejected — %s\n", r.Summary)
	case jira.TicketSkipped:
		fmt.Printf("    ⏭️  skipped\n")
	default:
		fmt.Printf("    ⚠️  failed at phase %s — %s\n", r.Phase, r.Error)
	}
}

func printFeedbackResult(r *jira.FeedbackResult) {
	if r.Error != "" {
		fmt.Printf("    ⚠️  error — %s\n", r.Error)
		return
	}
	if r.FixesMade == 0 {
		fmt.Printf("    ℹ️  no changes requested in %s\n", r.Duration.Round(time.Second))
		return
	}
	fmt.Printf("    🔄 reworked %d repo(s), %d commit(s) in %s\n", r.FixesMade, r.PushedCommits, r.Duration.Round(time.Second))
}

func printJiraRunSummary(results []jira.TicketResult, feedback []jira.FeedbackResult, d time.Duration) {
	completed, failed := countResults(results)
	rejected, skipped := 0, 0
	for _, r := range results {
		switch r.Status {
		case jira.TicketRejected:
			rejected++
		case jira.TicketSkipped:
			skipped++
		}
	}

	fmt.Println("\n🌙 Nightshift Jira Run Complete")
	fmt.Println("──────────────────────────────")
	fmt.Printf("  Duration:     %s\n", d.Round(time.Second))
	fmt.Printf("  Tickets:      %d processed\n", len(results))
	fmt.Printf("  ✅ Completed: %d\n", completed)
	fmt.Printf("  ❌ Rejected:  %d\n", rejected)
	if skipped > 0 {
		fmt.Printf("  ⏭️  Skipped:  %d\n", skipped)
	}
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

func countResults(results []jira.TicketResult) (completed, failed int) {
	for _, r := range results {
		switch r.Status {
		case jira.TicketCompleted:
			completed++
		default:
			if r.Status != jira.TicketSkipped && r.Status != jira.TicketRejected {
				failed++
			}
		}
	}
	return
}

func firstProjectKey(cfg jira.JiraConfig) string {
	if len(cfg.Projects) > 0 {
		return cfg.Projects[0].Key
	}
	return ""
}

// openDB opens the nightshift database; returns nil,err if it cannot be opened.
func openDB() (*db.DB, error) {
	return db.Open(db.DefaultPath())
}
