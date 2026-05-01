package jira

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/nightshift/internal/agents"
	"github.com/marcus/nightshift/internal/logging"
)

// FeedbackResult holds the outcome of processing review feedback for a ticket.
type FeedbackResult struct {
	TicketKey        string
	PRURLs           []string
	ReviewsFound     int // total review comments found
	FixesMade        int // number of repos where review feedback was addressed
	PushedCommits    int // number of commits pushed
	AcknowledgedOnly bool // true when the agent ran but made no changes (review already handled)
	Summary          string
	Error            string
	Duration         time.Duration
}

// ProcessFeedback handles the full PR review feedback loop for a ticket in ON REVIEW state.
// For each repo workspace it finds the associated PR, checks for CHANGES_REQUESTED, invokes
// the review-fix agent, commits and pushes the fixes, and posts summary comments on both
// the GitHub PR and the Jira ticket.
func (o *Orchestrator) ProcessFeedback(ctx context.Context, ticket Ticket, ws *Workspace) (*FeedbackResult, error) {
	if o.log == nil {
		o.log = logging.Component("jira.orchestrator")
	}
	if o.fnHasChanges == nil {
		o.fnHasChanges = HasChanges
	}
	if o.fnCommitAndPush == nil {
		o.fnCommitAndPush = CommitAndPush
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

	result := &FeedbackResult{TicketKey: ticket.Key}
	start := time.Now()

	agent := o.reviewFixAgent
	if agent == nil {
		agent = o.implAgent
	}
	if agent == nil {
		return nil, fmt.Errorf("jira: feedback: no agent available")
	}

	if ws != nil {
		for _, repo := range ws.Repos {
			// Find the PR for this ticket's branch.
			branch := BranchName(ticket.Key)
			prInfo, err := o.fnFindPR(ctx, repo.Path, branch)
			if err != nil {
				return nil, fmt.Errorf("jira: feedback: find pr %s: %w", repo.Name, err)
			}
			if prInfo == nil {
				o.log.Infof("ticket %s: no open PR found for branch %s in repo %s", ticket.Key, branch, repo.Name)
				continue
			}
			o.log.Infof("ticket %s: found PR %s in repo %s", ticket.Key, prInfo.URL, repo.Name)
			o.emit("  📎 PR %s", prInfo.URL)
			result.PRURLs = append(result.PRURLs, prInfo.URL)

			// Determine last rework timestamp for idempotency: skip reviews/comments
			// that were already addressed in a previous run.
			var lastReworkAt time.Time
			if nsComments := ParseNightshiftComments(ticket.Comments); len(nsComments) > 0 {
				if last := GetLastCommentOfType(nsComments, CommentRework); last != nil {
					lastReworkAt = last.Timestamp
				}
			}

			// Fetch the current review state.
			o.emit("  fetching PR review comments from GitHub…")
			reviewState, err := o.fnFetchReviews(ctx, repo.Path, prInfo.URL)
			if err != nil {
				return nil, fmt.Errorf("jira: feedback: fetch reviews %s: %w", repo.Name, err)
			}

			// Filter to only reviews/comments newer than the last rework.
			if !lastReworkAt.IsZero() {
				var newReviews []Review
				for _, r := range reviewState.Reviews {
					if r.CreatedAt.After(lastReworkAt) {
						newReviews = append(newReviews, r)
					}
				}
				reviewState.Reviews = newReviews
				reviewState.Comments = filterNewComments(reviewState.Comments, lastReworkAt)
				o.log.Infof("ticket %s: idempotency filter lastReworkAt=%s — reviews=%d comments=%d",
					ticket.Key, lastReworkAt.Format(time.RFC3339), len(reviewState.Reviews), len(reviewState.Comments))
			}

			inlineCount := 0
			resolvedCount := 0
			for _, c := range reviewState.Comments {
				if c.Path != "" {
					if c.Resolved {
						resolvedCount++
					} else {
						inlineCount++
					}
				}
			}
			o.log.Infof("ticket %s: PR %s — reviewDecision=%q reviews=%d comments=%d inline=%d resolved=%d actionable=%v",
				ticket.Key, prInfo.URL, reviewState.ReviewDecision,
				len(reviewState.Reviews), len(reviewState.Comments), inlineCount, resolvedCount,
				hasActionableComments(reviewState))
			result.ReviewsFound += len(reviewState.Reviews) + len(reviewState.Comments)

			// Skip logic depends on whether we've previously reworked this ticket.
			// After filtering by lastReworkAt, if nothing remains → skip regardless
			// of ReviewDecision (it stays CHANGES_REQUESTED until a re-review).
			// On first run (lastReworkAt zero), fall back to the original heuristic.
			if !lastReworkAt.IsZero() {
				if len(reviewState.Reviews) == 0 && !hasActionableComments(reviewState) {
					o.log.Infof("ticket %s: skipping rework — no new content since lastReworkAt=%s",
						ticket.Key, lastReworkAt.Format(time.RFC3339))
					o.emit("  ✓ no new review comments since last rework — skipping")
					continue
				}
			} else {
				// No previous rework: proceed only when changes are explicitly requested
				// or there are actionable inline comments (Copilot posts COMMENTED reviews,
				// not CHANGES_REQUESTED). Outdated comments are included — position=null
				// means the diff moved after a push, not that the suggestion was resolved.
				if reviewState.ReviewDecision != "CHANGES_REQUESTED" && !hasActionableComments(reviewState) {
					o.log.Infof("ticket %s: skipping rework — no actionable comments", ticket.Key)
					o.emit("  ✓ no actionable review comments — skipping")
					continue
				}
			}

			// Build a prompt from the review comments and execute the agent.
			prompt := buildReworkPrompt(ticket, reviewState, repo)
			rfCfg := o.cfg.EffectiveReviewFix(o.proj)
			timeout := parseTimeout(rfCfg.Timeout, 20*time.Minute)
			o.emit("🤖 %s running: review-fix  (%s, timeout %s)", rfCfg.Provider, rfCfg.Model, timeout.Round(time.Minute))
			agentResult, err := agent.Execute(ctx, agents.ExecuteOptions{
				Prompt:  prompt,
				WorkDir: repo.Path,
				Timeout: timeout,
				Model:   rfCfg.Model,
			})
			if err != nil {
				return nil, fmt.Errorf("jira: feedback: rework agent %s: %w", repo.Name, err)
			}

			// Only commit and report when the agent produced file changes.
			changed, err := o.fnHasChanges(ctx, repo.Path)
			if err != nil {
				return nil, fmt.Errorf("jira: feedback: check changes %s: %w", repo.Name, err)
			}
			if !changed {
				o.log.Infof("ticket %s: rework agent made no file changes — acknowledging timestamp", ticket.Key)
				o.emit("  ✓ agent made no changes — acknowledging review as handled")
				// Still record a CommentRework timestamp on Jira so the next run's
				// timestamp filter knows these review comments were already examined.
				result.AcknowledgedOnly = true
				continue
			}

			result.FixesMade++
			msg := CommitMessage(ticket.Key, "", "address review feedback")
			o.emit("  committing + pushing review fixes → %s", repo.Branch)
			if err := o.fnCommitAndPush(ctx, repo.Path, msg); err != nil {
				return nil, fmt.Errorf("jira: feedback: push fixes %s: %w", repo.Name, err)
			}
			result.PushedCommits++

			// Post a summary comment on the GitHub PR.
			o.emit("  posting rework summary to PR %s", prInfo.URL)
			if err := o.fnPostPRComment(ctx, repo.Path, prInfo.URL, buildPRReworkComment(agentResult.Output)); err != nil {
				o.log.Errorf("ticket %s: post pr comment %s: %v", ticket.Key, repo.Name, err)
			}
		}
	}

	result.Summary = fmt.Sprintf("Review feedback addressed in %d repo(s) across %d commit(s).", result.FixesMade, result.PushedCommits)

	// Post a Jira rework comment when:
	// - code was actually committed (PushedCommits > 0), OR
	// - agent examined the review but found no changes needed (AcknowledgedOnly).
	// In both cases, recording the CommentRework timestamp prevents the next run
	// from re-triggering the agent on the same already-seen review comments.
	if result.PushedCommits > 0 || result.AcknowledgedOnly {
		body := result.Summary
		if result.AcknowledgedOnly {
			body = "Reviewed open comments — no code changes needed; review feedback already addressed."
		}
		o.emit("📝 recording rework acknowledgement on Jira %s", ticket.Key)
		comment := NightshiftComment{
			Type:      CommentRework,
			Timestamp: time.Now(),
			Provider:  o.cfg.ReviewFix.Provider,
			Model:     o.cfg.ReviewFix.Model,
			Duration:  time.Since(start),
			Body:      body,
		}
		if err := o.client.PostComment(ctx, ticket.Key, comment); err != nil {
			o.log.Errorf("ticket %s: post rework comment: %v", ticket.Key, err)
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// hasActionableComments returns true when the review state has at least one
// unresolved inline comment. Resolved threads are excluded — they have already
// been addressed. Outdated comments are included: position=null means the diff
// moved after a push, not that the suggestion was resolved.
func hasActionableComments(rs *PRReviewState) bool {
	for _, c := range rs.Comments {
		if c.Path != "" && !c.Resolved {
			return true
		}
	}
	return false
}

// buildReworkPrompt constructs the agent prompt from PR review comments.
// Ticket context (title, description, AC, comments) is prepended so the agent
// can cross-reference the original intent while addressing feedback.
func buildReworkPrompt(ticket Ticket, review *PRReviewState, repo RepoWorkspace) string {
	var b strings.Builder

	// Ticket context — mirrors buildPlanPrompt / buildImplementPrompt pattern.
	fmt.Fprintf(&b, "## Ticket\nKey: %s\nTitle: %s\n", ticket.Key, ticket.Summary)
	fmt.Fprintf(&b, "Description:\n%s\n", ticket.Description)
	if ticket.AcceptanceCriteria != "" {
		fmt.Fprintf(&b, "\nAcceptance Criteria:\n%s\n", ticket.AcceptanceCriteria)
	}
	buildCommentsSection(&b, ticket)
	b.WriteString("\n---\n\n")

	fmt.Fprintf(&b, "## Review Feedback for %s\n\n", ticket.Key)
	fmt.Fprintf(&b, "PR: %s\nRepo: %s (branch: %s)\n\n", review.URL, repo.Name, repo.Branch)
	b.WriteString("### Reviewer Comments\n\n")
	for _, r := range review.Reviews {
		if r.State == "CHANGES_REQUESTED" || r.State == "COMMENTED" {
			fmt.Fprintf(&b, "**%s** (%s):\n%s\n\n", r.Author, r.State, r.Body)
		}
	}
	b.WriteString("### Inline Comments\n\n")
	for _, c := range review.Comments {
		if c.Path != "" && !c.Resolved {
			if c.Outdated {
				fmt.Fprintf(&b, "**%s:%d** (%s) [OUTDATED — verify if still applies to current code]:\n%s\n\n", c.Path, c.Line, c.Author, c.Body)
			} else {
				fmt.Fprintf(&b, "**%s:%d** (%s):\n%s\n\n", c.Path, c.Line, c.Author, c.Body)
			}
		}
	}
	b.WriteString("### Instructions\n")
	b.WriteString("Address ALL reviewer feedback above. For each comment:\n")
	b.WriteString("1. Make the requested change\n")
	b.WriteString("2. If you disagree, explain why in a code comment\n")
	b.WriteString("3. Do not modify code unrelated to the review feedback\n")
	lintCmd := repo.LintCommand
	if lintCmd == "" {
		lintCmd = "golangci-lint run ./..."
	}
	testCmd := repo.TestCommand
	if testCmd == "" {
		testCmd = "go test ./..."
	}
	b.WriteString("\n### Quality Checks (REQUIRED before finishing)\n")
	b.WriteString("After addressing all feedback, run the following commands and fix ALL failures:\n\n")
	fmt.Fprintf(&b, "- Lint: `%s`\n", lintCmd)
	fmt.Fprintf(&b, "- Test: `%s`\n\n", testCmd)
	b.WriteString("Do not finish until both commands exit with code 0. Do not commit or push — that will be handled separately.\n")
	return b.String()
}

// filterNewComments returns only comments created after lastSeen.
// When lastSeen is the zero time, all comments are returned.
func filterNewComments(comments []PRComment, lastSeen time.Time) []PRComment {
	if lastSeen.IsZero() {
		return comments
	}
	var filtered []PRComment
	for _, c := range comments {
		if c.CreatedAt.After(lastSeen) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// buildPRReworkComment formats the GitHub PR comment body summarising agent rework output.
func buildPRReworkComment(agentOutput string) string {
	var b strings.Builder
	b.WriteString("🤖 **Nightshift — Review Feedback Addressed**\n\n")
	b.WriteString("I've addressed the reviewer feedback.\n\n")
	if agentOutput != "" {
		b.WriteString("### Summary\n\n")
		b.WriteString(agentOutput)
		b.WriteString("\n\n")
	}
	b.WriteString("_This update was generated automatically by Nightshift._")
	return b.String()
}
