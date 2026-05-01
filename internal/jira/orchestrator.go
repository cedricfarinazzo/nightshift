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
	TicketKey             string        `json:"ticket_key"`
	Status                TicketStatus  `json:"status"`
	Phase                 Phase         `json:"phase"`
	PRURLs                []string      `json:"pr_urls,omitempty"`
	Plan                  string        `json:"plan,omitempty"`
	ImplementationSummary string        `json:"implementation_summary,omitempty"`
	Summary               string        `json:"summary,omitempty"`
	Error                 string        `json:"error,omitempty"`
	Duration              time.Duration `json:"duration"`
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
	skipValidation  bool
	onPhase         func(ticketKey string, phase Phase, done bool) // optional progress callback
	progressf       func(format string, args ...any)               // optional human-readable progress printer
	log             *logging.Logger

	// ops are injectable for testing; set to real functions by NewOrchestrator.
	fnHasChanges        func(ctx context.Context, repoPath string) (bool, error)
	fnCommitAndPush     func(ctx context.Context, repoPath, message string) error
	fnCreatePR          func(ctx context.Context, repo RepoWorkspace, ticket Ticket, jiraSite string) (*PRInfo, error)
	fnFindPR            func(ctx context.Context, repoPath, branch string) (*PRInfo, error)
	fnFetchReviews      func(ctx context.Context, repoPath, prURL string) (*PRReviewState, error)
	fnPostPRComment     func(ctx context.Context, repoPath, prURL, body string) error
	fnBranchAheadOfBase func(ctx context.Context, repoPath, branch, base string) (bool, error)
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

// WithReviewFixAgent sets the agent used for addressing PR review feedback.
// When not set, ProcessFeedback falls back to the impl agent.
func WithReviewFixAgent(a agents.Agent) OrchestratorOption {
	return func(o *Orchestrator) { o.reviewFixAgent = a }
}

// WithSkipValidation disables the LLM validation phase. The ticket is transitioned
// to in-progress directly, skipping the quality-score check.
func WithSkipValidation() OrchestratorOption {
	return func(o *Orchestrator) { o.skipValidation = true }
}

// WithPhaseCallback registers a callback invoked at the start (done=false) and
// end (done=true) of each phase. Useful for real-time progress reporting.
func WithPhaseCallback(fn func(ticketKey string, phase Phase, done bool)) OrchestratorOption {
	return func(o *Orchestrator) { o.onPhase = fn }
}

// WithProgressPrinter registers a printf-style function called for human-readable
// progress events (agent start, PR creation, Jira transitions, etc.).
func WithProgressPrinter(fn func(format string, args ...any)) OrchestratorOption {
	return func(o *Orchestrator) { o.progressf = fn }
}

// NewOrchestrator creates an Orchestrator with the given client, config, and options.
func NewOrchestrator(client *Client, cfg JiraConfig, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{
		client:          client,
		cfg:             cfg,
		log:             logging.Component("jira.orchestrator"),
		fnHasChanges:    HasChanges,
		fnCommitAndPush: CommitAndPush,
		fnCreatePR:      CreateOrUpdatePR,
		fnFindPR: func(ctx context.Context, repoPath, branch string) (*PRInfo, error) {
			return findExistingPR(ctx, repoPath, branch)
		},
		fnFetchReviews: FetchPRReviewComments,
		fnPostPRComment: func(ctx context.Context, repoPath, prURL, body string) error {
			_, err := ghExec(ctx, repoPath, "pr", "comment", prURL, "--body", body)
			return err
		},
		fnBranchAheadOfBase: BranchAheadOfBase,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// phaseOrder defines the processing order of lifecycle phases.
var phaseOrder = map[Phase]int{
	PhaseValidate:  0,
	PhasePlan:      1,
	PhaseImplement: 2,
	PhaseCommit:    3,
	PhasePR:        4,
	PhaseStatus:    5,
}

// resumeState holds the phase to start from and any data recovered from prior
// nightshift comments on the ticket.
type resumeState struct {
	startPhase      Phase
	alreadyDone     bool
	recoveredPlan   string   // non-empty when resuming from PhaseImplement or later
	recoveredPRURLs []string // non-empty when resuming from PhaseStatus
}

// detectResumeState inspects existing nightshift comments on the ticket and
// returns the phase to start from plus any previously recorded data needed to
// continue processing. This allows a re-run to skip phases that already succeeded.
func detectResumeState(ticket Ticket) resumeState {
	comments := ParseNightshiftComments(ticket.Comments)

	// Walk the phase sequence from latest to earliest to find the furthest
	// completed phase.
	hasPR := GetLastCommentOfType(comments, CommentPR) != nil
	hasImpl := GetLastCommentOfType(comments, CommentImplement) != nil
	hasPlan := GetLastCommentOfType(comments, CommentPlan) != nil
	hasValidation := GetLastCommentOfType(comments, CommentValidation) != nil
	hasStatus := GetLastCommentOfType(comments, CommentStatusChange) != nil

	switch {
	case hasStatus:
		// Already fully completed; signal early exit so ProcessTicket skips all phases
		// and avoids duplicate status comments/transitions.
		return resumeState{alreadyDone: true}

	case hasPR:
		// PRs exist; only the status transition remains.
		// Recover PR URLs from the comment body ("PRs created:\nurl1\nurl2\n...").
		prComment := GetLastCommentOfType(comments, CommentPR)
		return resumeState{
			startPhase:      PhaseStatus,
			recoveredPRURLs: parsePRURLsFromComment(prComment.Body),
		}

	case hasImpl:
		// Implementation done; resume from commit.
		// Also recover the plan in case it is needed for context.
		plan := ""
		if c := GetLastCommentOfType(comments, CommentPlan); c != nil {
			plan = c.Body
		}
		return resumeState{startPhase: PhaseCommit, recoveredPlan: plan}

	case hasPlan:
		// Plan done; resume from implement with the recorded plan.
		c := GetLastCommentOfType(comments, CommentPlan)
		return resumeState{startPhase: PhaseImplement, recoveredPlan: c.Body}

	case hasValidation:
		// Validated but not yet planned; resume from plan.
		return resumeState{startPhase: PhasePlan}

	default:
		return resumeState{startPhase: PhaseValidate}
	}
}

// parsePRURLsFromComment extracts PR URLs from a CommentPR body.
// The body format is "PRs created:\nurl1\nurl2\n...".
func parsePRURLsFromComment(body string) []string {
	var urls []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			urls = append(urls, line)
		}
	}
	return urls
}

// ProcessTicket drives a ticket through all lifecycle phases.
// It inspects existing nightshift comments to resume from the furthest completed
// phase, so re-runs skip phases that already succeeded.
// Phase failures are captured in the result (TicketFailed/TicketRejected);
// a non-nil error is only returned for infrastructure issues (nil agents).
func (o *Orchestrator) ProcessTicket(ctx context.Context, ticket Ticket, ws *Workspace) (*TicketResult, error) {
	if o.log == nil {
		o.log = logging.Component("jira.orchestrator")
	}
	if o.fnHasChanges == nil {
		o.fnHasChanges = HasChanges
	}
	if o.fnCommitAndPush == nil {
		o.fnCommitAndPush = CommitAndPush
	}
	if o.fnCreatePR == nil {
		o.fnCreatePR = CreateOrUpdatePR
	}
	if o.fnFindPR == nil {
		o.fnFindPR = func(ctx context.Context, repoPath, branch string) (*PRInfo, error) {
			return findExistingPR(ctx, repoPath, branch)
		}
	}
	if o.fnFetchReviews == nil {
		o.fnFetchReviews = FetchPRReviewComments
	}
	if o.fnPostPRComment == nil {
		o.fnPostPRComment = func(ctx context.Context, repoPath, prURL, body string) error {
			_, err := ghExec(ctx, repoPath, "pr", "comment", prURL, "--body", body)
			return err
		}
	}
	if o.fnBranchAheadOfBase == nil {
		o.fnBranchAheadOfBase = BranchAheadOfBase
	}

	start := time.Now()
	result := &TicketResult{TicketKey: ticket.Key}

	if o.implAgent == nil {
		return nil, fmt.Errorf("jira: orchestrator: impl agent is required")
	}
	if !o.skipValidation && o.validationAgent == nil {
		return nil, fmt.Errorf("jira: orchestrator: validation agent is required when not skipping validation")
	}

	rs := detectResumeState(ticket)
	if rs.alreadyDone {
		o.log.Infof("ticket %s: already completed, skipping", ticket.Key)
		result.Status = TicketCompleted
		result.Summary = "already completed"
		result.Duration = time.Since(start)
		return result, nil
	}
	if rs.startPhase != PhaseValidate {
		o.log.Infof("ticket %s: resuming from phase %s", ticket.Key, rs.startPhase)
	}

	// Seed result with any recovered data.
	result.Plan = rs.recoveredPlan
	result.PRURLs = rs.recoveredPRURLs

	skip := func(phase Phase) bool {
		return phaseOrder[phase] < phaseOrder[rs.startPhase]
	}

	// When resuming past the validate phase, ensure the ticket is in-progress
	// if it is still in the TODO status category (the validate phase handles this
	// for a fresh run, but a crash before the transition can leave it behind).
	if skip(PhaseValidate) && ticket.Status.CategoryKey == "new" {
		if err := o.client.TransitionToInProgress(ctx, ticket.Key); err != nil {
			o.postErrorComment(ctx, ticket.Key, rs.startPhase, err)
			result.Status = TicketFailed
			result.Error = err.Error()
			result.Duration = time.Since(start)
			return result, nil
		}
	}

	// Phase 1: Validate
	if !skip(PhaseValidate) {
		result.Phase = PhaseValidate
		o.notifyPhase(ticket.Key, PhaseValidate, false)
		if !o.skipValidation {
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
		} else {
			o.log.Infof("ticket %s: validation skipped", ticket.Key)
		}

		// Transition to In Progress
		if err := o.client.TransitionToInProgress(ctx, ticket.Key); err != nil {
			o.postErrorComment(ctx, ticket.Key, PhaseValidate, err)
			result.Status = TicketFailed
			result.Error = err.Error()
			result.Duration = time.Since(start)
			return result, nil
		}
	}

	// Phase 2: Plan
	if !skip(PhasePlan) {
		result.Phase = PhasePlan
		o.notifyPhase(ticket.Key, PhasePlan, false)
		o.emit("🤖 %s running: plan  (%s)", o.cfg.Plan.Provider, o.cfg.Plan.Model)
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
		o.emit("📝 posting plan to Jira %s", ticket.Key)
		o.postPhaseComment(ctx, ticket.Key, CommentPlan, planResult.Output, time.Since(planStart))
		o.log.Infof("ticket %s: plan complete", ticket.Key)
	}

	// Phase 3: Implement
	if !skip(PhaseImplement) {
		result.Phase = PhaseImplement
		o.notifyPhase(ticket.Key, PhaseImplement, false)
		timeout := parseTimeout(o.cfg.Implement.Timeout, 30*time.Minute)
		o.emit("🤖 %s running: implement  (%s, timeout %s)", o.cfg.Implement.Provider, o.cfg.Implement.Model, timeout.Round(time.Minute))
		implStart := time.Now()
		workDir := ""
		if ws != nil && len(ws.Repos) > 0 {
			workDir = ws.Repos[0].Path
		}
		implResult, err := o.implAgent.Execute(ctx, agents.ExecuteOptions{
			Prompt:  o.buildImplementPrompt(ticket, result.Plan, ws),
			WorkDir: workDir,
			Timeout: timeout,
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
		result.ImplementationSummary = implResult.Output
		o.emit("📝 posting implementation summary to Jira %s", ticket.Key)
		o.postPhaseComment(ctx, ticket.Key, CommentImplement, implResult.Output, time.Since(implStart))
		o.log.Infof("ticket %s: implementation complete", ticket.Key)
	}

	// Phase 4: Commit
	if !skip(PhaseCommit) {
		result.Phase = PhaseCommit
		o.notifyPhase(ticket.Key, PhaseCommit, false)
		var changedRepos []RepoWorkspace
		var skippedRepos []RepoWorkspace // repos with no uncommitted changes
		if ws != nil {
			for _, repo := range ws.Repos {
				changed, err := o.fnHasChanges(ctx, repo.Path)
				if err != nil {
					o.postErrorComment(ctx, ticket.Key, PhaseCommit, err)
					result.Status = TicketFailed
					result.Error = err.Error()
					result.Duration = time.Since(start)
					return result, nil
				}
				if !changed {
					o.emit("  no changes in repo %s — skipping commit", repo.Name)
					// NOTE: If the agent only modified repo[0] but the ticket required
					// changes in this repo too, we silently skip it here. Enforcement
					// (fail-fast when expected repos are untouched) is deferred to a
					// follow-up ticket.
					skippedRepos = append(skippedRepos, repo)
					continue
				}
				msg := CommitMessage(ticket.Key, "", ticket.Summary)
				o.emit("  committing + pushing %s → %s", repo.Name, repo.Branch)
				if err := o.fnCommitAndPush(ctx, repo.Path, msg); err != nil {
					o.postErrorComment(ctx, ticket.Key, PhaseCommit, err)
					result.Status = TicketFailed
					result.Error = err.Error()
					result.Duration = time.Since(start)
					return result, nil
				}
				changedRepos = append(changedRepos, repo)
			}
		}

		// Recovery pass: a repo with no uncommitted changes may have been committed and pushed
		// in a prior run that crashed before the CommentPR was posted. If the branch is ahead
		// of its base on the remote, treat it as if it was committed in this run so the PR
		// creation loop can create or recover the PR.
		var recoveredRepos []RepoWorkspace
		for _, repo := range skippedRepos {
			ahead, err := o.fnBranchAheadOfBase(ctx, repo.Path, repo.Branch, repo.BaseBranch)
			if err != nil {
				o.postErrorComment(ctx, ticket.Key, PhaseCommit, err)
				result.Status = TicketFailed
				result.Error = err.Error()
				result.Duration = time.Since(start)
				o.log.Errorf("ticket %s: branch-ahead check for %s: %v", ticket.Key, repo.Name, err)
				return result, nil
			}
			if ahead {
				o.emit("  branch %s already pushed — recovering PR creation", repo.Branch)
				recoveredRepos = append(recoveredRepos, repo)
			}
		}

		// Phase 5: PR
		result.Phase = PhasePR
		o.notifyPhase(ticket.Key, PhasePR, false)
		prStart := time.Now()
		type repoPR struct {
			repo RepoWorkspace
			url  string
		}
		var repoPRs []repoPR

		// Repos committed in this run: create or update the PR via fnCreatePR (which
		// already handles deduplication internally via findExistingPR).
		for _, repo := range changedRepos {
			o.emit("  creating PR for %s (%s → %s)", repo.Name, repo.Branch, repo.BaseBranch)
			prInfo, err := o.fnCreatePR(ctx, repo, ticket, o.cfg.Site)
			if err != nil {
				o.postErrorComment(ctx, ticket.Key, PhasePR, err)
				result.Status = TicketFailed
				result.Error = err.Error()
				result.Duration = time.Since(start)
				return result, nil
			}
			o.emit("  ✓ PR created: %s", prInfo.URL)
			result.PRURLs = append(result.PRURLs, prInfo.URL)
			repoPRs = append(repoPRs, repoPR{repo: repo, url: prInfo.URL})
		}

		// Recovered repos: check for an existing open PR first to avoid duplicates.
		for _, repo := range recoveredRepos {
			existing, err := o.fnFindPR(ctx, repo.Path, repo.Branch)
			if err != nil {
				o.postErrorComment(ctx, ticket.Key, PhasePR, err)
				result.Status = TicketFailed
				result.Error = err.Error()
				result.Duration = time.Since(start)
				return result, nil
			}
			if existing != nil {
				o.emit("  PR already exists for %s: %s", repo.Name, existing.URL)
				result.PRURLs = append(result.PRURLs, existing.URL)
				repoPRs = append(repoPRs, repoPR{repo: repo, url: existing.URL})
			} else {
				o.emit("  creating PR for %s (%s → %s)", repo.Name, repo.Branch, repo.BaseBranch)
				prInfo, err := o.fnCreatePR(ctx, repo, ticket, o.cfg.Site)
				if err != nil {
					o.postErrorComment(ctx, ticket.Key, PhasePR, err)
					result.Status = TicketFailed
					result.Error = err.Error()
					result.Duration = time.Since(start)
					return result, nil
				}
				o.emit("  ✓ PR created: %s", prInfo.URL)
				result.PRURLs = append(result.PRURLs, prInfo.URL)
				repoPRs = append(repoPRs, repoPR{repo: repo, url: prInfo.URL})
			}
		}

		if len(result.PRURLs) > 0 {
			o.emit("📝 posting PR links to Jira %s", ticket.Key)
			o.postPhaseComment(ctx, ticket.Key, CommentPR,
				fmt.Sprintf("PRs created:\n%s", strings.Join(result.PRURLs, "\n")),
				time.Since(prStart))
			// Post implementation summary as PR comment so reviewers have full context.
			if result.ImplementationSummary != "" {
				for _, rpr := range repoPRs {
					o.emit("  posting implementation summary to PR %s", rpr.url)
					prComment := buildPRImplementationComment(ticket, result.ImplementationSummary, o.cfg.Site)
					if err := o.fnPostPRComment(ctx, rpr.repo.Path, rpr.url, prComment); err != nil {
						o.log.Errorf("ticket %s: post impl summary on PR %s: %v", ticket.Key, rpr.url, err)
					}
				}
			}
		}
	} else if skip(PhaseCommit) && !skip(PhaseStatus) {
		// Resuming at PhaseStatus: scan workspace repos for open PRs and merge with
		// any URLs recovered from comments, deduplicating to avoid duplicates when
		// the comment contained only a subset of the actual PRs.
		if ws != nil {
			branch := BranchName(ticket.Key)
			seenURLs := make(map[string]struct{}, len(result.PRURLs))
			for _, url := range result.PRURLs {
				seenURLs[url] = struct{}{}
			}
			for _, repo := range ws.Repos {
				pr, err := o.fnFindPR(ctx, repo.Path, branch)
				if err == nil && pr != nil {
					if _, seen := seenURLs[pr.URL]; !seen {
						result.PRURLs = append(result.PRURLs, pr.URL)
						seenURLs[pr.URL] = struct{}{}
					}
				}
			}
		}
	}

	// Phase 6: Status
	if !skip(PhaseStatus) {
		result.Phase = PhaseStatus
		o.notifyPhase(ticket.Key, PhaseStatus, false)
		o.emit("🔄 transitioning %s → review (Jira)", ticket.Key)
		if err := o.client.TransitionToReview(ctx, ticket.Key); err != nil {
			o.postErrorComment(ctx, ticket.Key, PhaseStatus, err)
			result.Status = TicketFailed
			result.Error = err.Error()
			result.Duration = time.Since(start)
			return result, nil
		}
	}

	result.Status = TicketCompleted
	result.Summary = fmt.Sprintf("completed: %d PRs", len(result.PRURLs))
	result.Duration = time.Since(start)
	statusBody := fmt.Sprintf("Ticket processing complete. Duration: %s. PRs: %d.",
		result.Duration.Round(time.Second), len(result.PRURLs))
	if len(result.PRURLs) > 0 {
		statusBody += "\n\n" + strings.Join(result.PRURLs, "\n")
	}
	o.postPhaseComment(ctx, ticket.Key, CommentStatusChange, statusBody, result.Duration)
	o.log.Infof("ticket %s: completed in %s", ticket.Key, result.Duration.Round(time.Second))
	return result, nil
}

// buildParentSection appends a parent ticket section to b when the ticket has a parent.
func buildParentSection(b *strings.Builder, ticket Ticket) {
	if ticket.ParentKey == "" {
		return
	}
	fmt.Fprintf(b, "\n## Parent Ticket\nKey: %s\nSummary: %s\n", ticket.ParentKey, ticket.ParentSummary)
	if ticket.ParentDescription != "" {
		fmt.Fprintf(b, "Description:\n%s\n", ticket.ParentDescription)
	}
}

// buildCommentsSection appends a comments section to b when the ticket has comments.
func buildCommentsSection(b *strings.Builder, ticket Ticket) {
	if len(ticket.Comments) == 0 {
		return
	}
	b.WriteString("\n## Comments\n")
	for _, c := range ticket.Comments {
		fmt.Fprintf(b, "- %s: %s\n", c.Author, c.Body)
	}
}

// buildPlanPrompt constructs the prompt for the plan phase.
func (o *Orchestrator) buildPlanPrompt(ticket Ticket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are a planning agent. Create a detailed implementation plan for this Jira ticket.\n\n")
	buildParentSection(&b, ticket)
	fmt.Fprintf(&b, "\n## Ticket\nKey: %s\nTitle: %s\n", ticket.Key, ticket.Summary)
	fmt.Fprintf(&b, "Description:\n%s\n", ticket.Description)
	if ticket.AcceptanceCriteria != "" {
		fmt.Fprintf(&b, "\nAcceptance Criteria:\n%s\n", ticket.AcceptanceCriteria)
	}
	buildCommentsSection(&b, ticket)
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
	buildParentSection(&b, ticket)
	fmt.Fprintf(&b, "\n## Ticket\nKey: %s\nTitle: %s\n", ticket.Key, ticket.Summary)
	fmt.Fprintf(&b, "Description:\n%s\n", ticket.Description)
	if ticket.AcceptanceCriteria != "" {
		fmt.Fprintf(&b, "\nAcceptance Criteria:\n%s\n", ticket.AcceptanceCriteria)
	}
	buildCommentsSection(&b, ticket)
	fmt.Fprintf(&b, "\n## Plan\n%s\n", plan)
	if ws != nil && len(ws.Repos) > 0 {
		b.WriteString("\n## Workspace\n")
		for _, repo := range ws.Repos {
			fmt.Fprintf(&b, "- %s: %s (branch: %s, base: %s)\n",
				repo.Name, repo.Path, repo.Branch, repo.BaseBranch)
		}
		if len(ws.Repos) > 1 {
			b.WriteString("\nYou are responsible for making changes across ALL repos listed above. ")
			b.WriteString("Use their absolute paths to edit files in each repo. ")
			b.WriteString("Do not limit your edits to your working directory.\n")
		}
	}
	b.WriteString("\n## Instructions\n")
	b.WriteString("1. Implement the plan step by step — complete EVERY step before stopping\n")
	b.WriteString("2. Make all necessary code changes\n")
	b.WriteString("3. Run tests and fix all failures before stopping\n")
	b.WriteString("4. Verify ALL acceptance criteria are met before stopping\n")
	b.WriteString("5. Do not commit or push — that will be handled separately\n")
	b.WriteString("6. If you encounter ambiguity, make a reasonable assumption and document it in a comment\n")
	b.WriteString("7. Do NOT stop early — continue until the entire plan is implemented and tests pass\n")
	return b.String()
}

// buildPRImplementationComment builds the GitHub PR comment body with the agent's
// implementation summary so reviewers have full context inline.
func buildPRImplementationComment(ticket Ticket, summary, jiraSite string) string {
	var b strings.Builder
	b.WriteString("🤖 **Nightshift — Implementation Summary**\n\n")
	fmt.Fprintf(&b, "**Ticket:** [%s — %s](%s)\n\n", ticket.Key, ticket.Summary, jiraBrowseURL(jiraSite, ticket.Key))
	b.WriteString("### What was done\n\n")
	b.WriteString(summary)
	b.WriteString("\n\n---\n")
	b.WriteString("*Generated automatically by [Nightshift](https://github.com/cedricfarinazzo/nightshift)*\n")
	return b.String()
}

// providerForCommentType selects provider/model attribution metadata for a comment type.
func (o *Orchestrator) providerForCommentType(ct CommentType) (provider, model string) {
	switch ct {
	case CommentValidation:
		return o.cfg.Validation.Provider, o.cfg.Validation.Model
	case CommentPlan:
		return o.cfg.Plan.Provider, o.cfg.Plan.Model
	default:
		return o.cfg.Implement.Provider, o.cfg.Implement.Model
	}
}

// providerForPhase returns the configured provider/model metadata associated with
// a phase for comment attribution and error reporting. This reflects phase config
// rather than necessarily the actual agent that executed the phase.
func (o *Orchestrator) providerForPhase(phase Phase) (provider, model string) {
	return o.providerForCommentType(commentTypeForPhase(phase))
}

func commentTypeForPhase(phase Phase) CommentType {
	switch phase {
	case PhaseValidate:
		return CommentValidation
	case PhasePlan:
		return CommentPlan
	default: // PhaseImplement, PhaseCommit, PhasePR, PhaseStatus
		return CommentImplement
	}
}

// postErrorComment posts an error comment to the Jira ticket.
func (o *Orchestrator) postErrorComment(ctx context.Context, ticketKey string, phase Phase, err error) {
	ct := CommentError
	provider, model := o.providerForPhase(phase)
	comment := NightshiftComment{
		Type:      ct,
		Timestamp: time.Now(),
		Provider:  provider,
		Model:     model,
		Body:      fmt.Sprintf("Error during %s phase: %s", phase, err.Error()),
	}
	if pErr := o.client.PostComment(ctx, ticketKey, comment); pErr != nil {
		o.log.Errorf("ticket %s: post error comment: %v", ticketKey, pErr)
	}
}

// postPhaseComment posts a success comment for a completed phase.
func (o *Orchestrator) postPhaseComment(ctx context.Context, ticketKey string, ct CommentType, body string, duration time.Duration) {
	provider, model := o.providerForCommentType(ct)
	comment := NightshiftComment{
		Type:      ct,
		Timestamp: time.Now(),
		Provider:  provider,
		Model:     model,
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

// notifyPhase invokes the onPhase callback when one is registered.
func (o *Orchestrator) notifyPhase(ticketKey string, phase Phase, done bool) {
	if o.onPhase != nil {
		o.onPhase(ticketKey, phase, done)
	}
}

// emit calls the progress printer when one is registered.
func (o *Orchestrator) emit(format string, args ...any) {
	if o.progressf != nil {
		o.progressf(format, args...)
	}
}
