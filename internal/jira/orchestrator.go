package jira

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/nightshift/internal/agents"
	"github.com/marcus/nightshift/internal/logging"
)

// Phase represents a stage in the Jira ticket processing lifecycle.
type Phase string

const (
	PhaseValidate  Phase = "validate"
	PhasePlan      Phase = "plan"
	PhaseImplement Phase = "implement"
	PhaseCommit    Phase = "commit"
	PhasePR        Phase = "pr"
	PhaseStatus    Phase = "status"
)

// TicketStatus represents the outcome of processing a ticket.
type TicketStatus string

const (
	TicketCompleted TicketStatus = "completed"
	TicketRejected  TicketStatus = "rejected"
	TicketFailed    TicketStatus = "failed"
	TicketSkipped   TicketStatus = "skipped"
)

// TicketResult holds the outcome of processing a single Jira ticket.
type TicketResult struct {
	TicketKey string        `json:"ticket_key"`
	Status    TicketStatus  `json:"status"`
	Phase     Phase         `json:"phase"`
	PRURLs    []string      `json:"pr_urls,omitempty"`
	Plan      string        `json:"plan,omitempty"`
	Summary   string        `json:"summary,omitempty"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
}

// jiraClient defines the Jira operations needed by the orchestrator.
type jiraClient interface {
	PostComment(ctx context.Context, ticketKey string, comment NightshiftComment) error
	HandleInvalidTicket(ctx context.Context, ticketKey string, result *ValidationResult) error
	TransitionToInProgress(ctx context.Context, issueKey string) error
	TransitionToReview(ctx context.Context, issueKey string) error
}

// Orchestrator drives the Jira ticket lifecycle: validate, plan, implement,
// commit, PR, and status transition.
type Orchestrator struct {
	client          jiraClient
	cfg             JiraConfig
	validationAgent agents.Agent
	implAgent       agents.Agent
	reviewFixAgent  agents.Agent
	statusMap       *StatusMap
	log             *logging.Logger
}

// OrchestratorOption configures an Orchestrator.
type OrchestratorOption func(*Orchestrator)

// WithValidationAgent sets the agent used for ticket validation.
func WithValidationAgent(a agents.Agent) OrchestratorOption {
	return func(o *Orchestrator) { o.validationAgent = a }
}

// WithImplAgent sets the agent used for planning and implementation.
func WithImplAgent(a agents.Agent) OrchestratorOption {
	return func(o *Orchestrator) { o.implAgent = a }
}

// WithReviewFixAgent sets the agent used for addressing review feedback.
func WithReviewFixAgent(a agents.Agent) OrchestratorOption {
	return func(o *Orchestrator) { o.reviewFixAgent = a }
}

// WithStatusMap injects a pre-discovered status map.
func WithStatusMap(sm *StatusMap) OrchestratorOption {
	return func(o *Orchestrator) { o.statusMap = sm }
}

// NewOrchestrator creates an Orchestrator with the given client, config, and options.
func NewOrchestrator(client *Client, cfg JiraConfig, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{
		client: client,
		cfg:    cfg,
		log:    logging.Component("jira.orchestrator"),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// ProcessTicket drives a ticket through all lifecycle phases.
// Phase failures are captured in the result (TicketFailed/TicketRejected);
// a non-nil error is only returned for infrastructure issues (nil agents).
func (o *Orchestrator) ProcessTicket(ctx context.Context, ticket Ticket, ws *Workspace) (*TicketResult, error) {
	if o.log == nil {
		o.log = logging.Component("jira.orchestrator")
	}

	start := time.Now()
	result := &TicketResult{TicketKey: ticket.Key}

	if o.validationAgent == nil || o.implAgent == nil {
		return nil, fmt.Errorf("jira: orchestrator: validation and impl agents are required")
	}

	// Phase 1: Validate
	result.Phase = PhaseValidate
	vr, err := ValidateTicket(ctx, o.validationAgent, ticket)
	if err != nil {
		o.postErrorComment(ctx, ticket.Key, PhaseValidate, err)
		result.Status = TicketFailed
		result.Error = err.Error()
		result.Duration = time.Since(start)
		o.log.Errorf("ticket %s: validation failed: %v", ticket.Key, err)
		return result, nil
	}
	if !vr.Valid {
		if hErr := o.client.HandleInvalidTicket(ctx, ticket.Key, vr); hErr != nil {
			o.log.Errorf("ticket %s: handle invalid: %v", ticket.Key, hErr)
		}
		result.Status = TicketRejected
		result.Summary = fmt.Sprintf("rejected: score %d/10", vr.Score)
		result.Duration = time.Since(start)
		o.log.Infof("ticket %s rejected (score %d/10)", ticket.Key, vr.Score)
		return result, nil
	}
	o.postPhaseComment(ctx, ticket.Key, CommentValidation,
		fmt.Sprintf("Ticket validated (score %d/10).", vr.Score), time.Since(start))
	o.log.Infof("ticket %s validated (score %d/10)", ticket.Key, vr.Score)

	// Transition to In Progress
	if err := o.client.TransitionToInProgress(ctx, ticket.Key); err != nil {
		o.postErrorComment(ctx, ticket.Key, PhaseValidate, err)
		result.Status = TicketFailed
		result.Error = err.Error()
		result.Duration = time.Since(start)
		return result, nil
	}

	// Phase 2: Plan
	result.Phase = PhasePlan
	planStart := time.Now()
	planResult, err := o.implAgent.Execute(ctx, agents.ExecuteOptions{
		Prompt:  o.buildPlanPrompt(ticket),
		Timeout: parseTimeout(o.cfg.Plan.Timeout, 5*time.Minute),
		Model:   o.cfg.Plan.Model,
	})
	if err != nil {
		o.postErrorComment(ctx, ticket.Key, PhasePlan, err)
		result.Status = TicketFailed
		result.Error = err.Error()
		result.Duration = time.Since(start)
		o.log.Errorf("ticket %s: plan failed: %v", ticket.Key, err)
		return result, nil
	}
	result.Plan = planResult.Output
	o.postPhaseComment(ctx, ticket.Key, CommentPlan, planResult.Output, time.Since(planStart))
	o.log.Infof("ticket %s: plan complete", ticket.Key)

	// Phase 3: Implement
	result.Phase = PhaseImplement
	implStart := time.Now()
	workDir := ""
	if len(ws.Repos) > 0 {
		workDir = ws.Repos[0].Path
	}
	implResult, err := o.implAgent.Execute(ctx, agents.ExecuteOptions{
		Prompt:  o.buildImplementPrompt(ticket, result.Plan, ws),
		WorkDir: workDir,
		Timeout: parseTimeout(o.cfg.Implement.Timeout, 30*time.Minute),
		Model:   o.cfg.Implement.Model,
	})
	if err != nil {
		o.postErrorComment(ctx, ticket.Key, PhaseImplement, err)
		result.Status = TicketFailed
		result.Error = err.Error()
		result.Duration = time.Since(start)
		o.log.Errorf("ticket %s: implement failed: %v", ticket.Key, err)
		return result, nil
	}
	o.postPhaseComment(ctx, ticket.Key, CommentImplement, implResult.Output, time.Since(implStart))
	o.log.Infof("ticket %s: implementation complete", ticket.Key)

	// Phase 4: Commit
	result.Phase = PhaseCommit
	var changedRepos []RepoWorkspace
	for _, repo := range ws.Repos {
		changed, err := HasChanges(ctx, repo.Path)
		if err != nil {
			o.postErrorComment(ctx, ticket.Key, PhaseCommit, err)
			result.Status = TicketFailed
			result.Error = err.Error()
			result.Duration = time.Since(start)
			return result, nil
		}
		if !changed {
			continue
		}
		msg := CommitMessage(ticket.Key, "", ticket.Summary)
		if err := CommitAndPush(ctx, repo.Path, msg); err != nil {
			o.postErrorComment(ctx, ticket.Key, PhaseCommit, err)
			result.Status = TicketFailed
			result.Error = err.Error()
			result.Duration = time.Since(start)
			return result, nil
		}
		changedRepos = append(changedRepos, repo)
	}

	// Phase 5: PR
	result.Phase = PhasePR
	for _, repo := range changedRepos {
		prInfo, err := CreateOrUpdatePR(ctx, repo, ticket, o.cfg.Site)
		if err != nil {
			o.postErrorComment(ctx, ticket.Key, PhasePR, err)
			result.Status = TicketFailed
			result.Error = err.Error()
			result.Duration = time.Since(start)
			return result, nil
		}
		result.PRURLs = append(result.PRURLs, prInfo.URL)
	}
	if len(result.PRURLs) > 0 {
		o.postPhaseComment(ctx, ticket.Key, CommentPR,
			fmt.Sprintf("PRs created:\n%s", strings.Join(result.PRURLs, "\n")),
			time.Since(implStart))
	}

	// Phase 6: Status
	result.Phase = PhaseStatus
	if err := o.client.TransitionToReview(ctx, ticket.Key); err != nil {
		o.postErrorComment(ctx, ticket.Key, PhaseStatus, err)
		result.Status = TicketFailed
		result.Error = err.Error()
		result.Duration = time.Since(start)
		return result, nil
	}

	result.Status = TicketCompleted
	result.Summary = fmt.Sprintf("completed: %d PRs", len(result.PRURLs))
	result.Duration = time.Since(start)
	o.postPhaseComment(ctx, ticket.Key, CommentStatusChange,
		fmt.Sprintf("Ticket processing complete. Duration: %s. PRs: %d.",
			result.Duration.Round(time.Second), len(result.PRURLs)),
		result.Duration)
	o.log.Infof("ticket %s: completed in %s", ticket.Key, result.Duration.Round(time.Second))
	return result, nil
}

// buildPlanPrompt constructs the prompt for the plan phase.
func (o *Orchestrator) buildPlanPrompt(ticket Ticket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are a planning agent. Create a detailed implementation plan for this Jira ticket.\n\n")
	fmt.Fprintf(&b, "## Ticket\nKey: %s\nTitle: %s\n", ticket.Key, ticket.Summary)
	fmt.Fprintf(&b, "Description:\n%s\n", ticket.Description)
	if ticket.AcceptanceCriteria != "" {
		fmt.Fprintf(&b, "\nAcceptance Criteria:\n%s\n", ticket.AcceptanceCriteria)
	}
	if len(ticket.Comments) > 0 {
		b.WriteString("\n## Comments\n")
		for _, c := range ticket.Comments {
			fmt.Fprintf(&b, "- %s: %s\n", c.Author, c.Body)
		}
	}
	b.WriteString("\n## Instructions\n")
	b.WriteString("1. Break the work into clear, ordered steps\n")
	b.WriteString("2. Identify files to create or modify\n")
	b.WriteString("3. Note any dependencies or risks\n")
	b.WriteString("4. Output the plan as plain text\n")
	return b.String()
}

// buildImplementPrompt constructs the prompt for the implementation phase.
func (o *Orchestrator) buildImplementPrompt(ticket Ticket, plan string, ws *Workspace) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are an implementation agent. Implement the following Jira ticket.\n\n")
	fmt.Fprintf(&b, "## Ticket\nKey: %s\nTitle: %s\n", ticket.Key, ticket.Summary)
	fmt.Fprintf(&b, "Description:\n%s\n", ticket.Description)
	if ticket.AcceptanceCriteria != "" {
		fmt.Fprintf(&b, "\nAcceptance Criteria:\n%s\n", ticket.AcceptanceCriteria)
	}
	fmt.Fprintf(&b, "\n## Plan\n%s\n", plan)
	if ws != nil && len(ws.Repos) > 0 {
		b.WriteString("\n## Workspace\n")
		for _, repo := range ws.Repos {
			fmt.Fprintf(&b, "- %s: %s (branch: %s, base: %s)\n",
				repo.Name, repo.Path, repo.Branch, repo.BaseBranch)
		}
	}
	b.WriteString("\n## Instructions\n")
	b.WriteString("1. Implement the plan step by step\n")
	b.WriteString("2. Make all necessary code changes\n")
	b.WriteString("3. Ensure tests pass\n")
	b.WriteString("4. Do not commit or push — that will be handled separately\n")
	return b.String()
}

// postErrorComment posts an error comment to the Jira ticket.
func (o *Orchestrator) postErrorComment(ctx context.Context, ticketKey string, phase Phase, err error) {
	comment := NightshiftComment{
		Type:      CommentError,
		Timestamp: time.Now(),
		Provider:  o.cfg.Implement.Provider,
		Model:     o.cfg.Implement.Model,
		Body:      fmt.Sprintf("Error during %s phase: %s", phase, err.Error()),
	}
	if pErr := o.client.PostComment(ctx, ticketKey, comment); pErr != nil {
		o.log.Errorf("ticket %s: post error comment: %v", ticketKey, pErr)
	}
}

// postPhaseComment posts a success comment for a completed phase.
func (o *Orchestrator) postPhaseComment(ctx context.Context, ticketKey string, ct CommentType, body string, duration time.Duration) {
	comment := NightshiftComment{
		Type:      ct,
		Timestamp: time.Now(),
		Provider:  o.cfg.Implement.Provider,
		Model:     o.cfg.Implement.Model,
		Duration:  duration,
		Body:      body,
	}
	if err := o.client.PostComment(ctx, ticketKey, comment); err != nil {
		o.log.Errorf("ticket %s: post phase comment: %v", ticketKey, err)
	}
}

// parseTimeout parses a duration string, returning fallback on error.
func parseTimeout(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
