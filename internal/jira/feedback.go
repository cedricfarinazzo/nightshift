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
	TicketKey     string
	PRURLs        []string
	ReviewsFound  int // total review comments found
	FixesMade     int // number of repos where review feedback was addressed
	PushedCommits int // number of commits pushed
	Summary       string
	Error         string
	Duration      time.Duration
}

// ProcessFeedback handles the full PR review feedback loop for a ticket in ON REVIEW state.
// For each repo workspace it finds the associated PR, checks for CHANGES_REQUESTED, invokes
// the review-fix agent, commits and pushes the fixes, and posts summary comments on both
// the GitHub PR and the Jira ticket.
func (o *Orchestrator) ProcessFeedback(ctx context.Context, ticket Ticket, ws *Workspace) (*FeedbackResult, error) {
	if o.log == nil {
		o.log = logging.Component("jira.orchestrator")
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
			prInfo, err := o.fnFindPR(ctx, repo.Path, BranchName(ticket.Key))
			if err != nil {
				return nil, fmt.Errorf("jira: feedback: find pr %s: %w", repo.Name, err)
			}
			if prInfo == nil {
				continue
			}
			result.PRURLs = append(result.PRURLs, prInfo.URL)

			// Fetch the current review state.
			reviewState, err := o.fnFetchReviews(ctx, repo.Path, prInfo.URL)
			if err != nil {
				return nil, fmt.Errorf("jira: feedback: fetch reviews %s: %w", repo.Name, err)
			}
			result.ReviewsFound += len(reviewState.Reviews) + len(reviewState.Comments)

			// Skip repos where the reviewer has not requested changes.
			if reviewState.ReviewDecision != "CHANGES_REQUESTED" {
				continue
			}

			// Build a prompt from the review comments and execute the agent.
			prompt := buildReworkPrompt(ticket, reviewState, repo)
			agentResult, err := agent.Execute(ctx, agents.ExecuteOptions{
				Prompt:  prompt,
				WorkDir: repo.Path,
				Timeout: parseTimeout(o.cfg.ReviewFix.Timeout, 20*time.Minute),
				Model:   o.cfg.ReviewFix.Model,
			})
			if err != nil {
				return nil, fmt.Errorf("jira: feedback: rework agent %s: %w", repo.Name, err)
			}

			// Commit and push the fixes.
			msg := CommitMessage(ticket.Key, "", "address review feedback")
			if err := o.fnCommitAndPush(ctx, repo.Path, msg); err != nil {
				return nil, fmt.Errorf("jira: feedback: push fixes %s: %w", repo.Name, err)
			}
			result.PushedCommits++
			result.FixesMade++

			// Post a summary comment on the GitHub PR.
			if err := o.fnPostPRComment(ctx, repo.Path, prInfo.URL, buildPRReworkComment(agentResult.Output)); err != nil {
				o.log.Errorf("ticket %s: post pr comment %s: %v", ticket.Key, repo.Name, err)
			}
		}
	}

	result.Summary = fmt.Sprintf("%d review item(s) addressed across %d commit(s).", result.FixesMade, result.PushedCommits)

	// Post a Jira rework comment when fixes were made.
	if result.FixesMade > 0 {
		comment := NightshiftComment{
			Type:      CommentRework,
			Timestamp: time.Now(),
			Provider:  o.cfg.ReviewFix.Provider,
			Model:     o.cfg.ReviewFix.Model,
			Duration:  time.Since(start),
			Body:      result.Summary,
		}
		if err := o.client.PostComment(ctx, ticket.Key, comment); err != nil {
			o.log.Errorf("ticket %s: post rework comment: %v", ticket.Key, err)
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// buildReworkPrompt constructs the agent prompt from PR review comments.
func buildReworkPrompt(ticket Ticket, review *PRReviewState, repo RepoWorkspace) string {
	var b strings.Builder
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
		if c.Path != "" {
			fmt.Fprintf(&b, "**%s:%d** (%s):\n%s\n\n", c.Path, c.Line, c.Author, c.Body)
		}
	}
	b.WriteString("### Instructions\n")
	b.WriteString("Address ALL reviewer feedback above. For each comment:\n")
	b.WriteString("1. Make the requested change\n")
	b.WriteString("2. If you disagree, explain why in a code comment\n")
	b.WriteString("3. Do not modify code unrelated to the review feedback\n")
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
