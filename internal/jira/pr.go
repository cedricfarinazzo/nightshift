package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/marcus/nightshift/internal/logging"
)

// PRInfo describes a GitHub pull request created or updated for a Jira ticket.
type PRInfo struct {
	URL        string
	Number     int
	Repo       string
	Branch     string
	BaseBranch string
	IsNew      bool
}

// PRReviewState captures the current review state of a pull request.
type PRReviewState struct {
	URL            string
	Number         int
	State          string
	ReviewDecision string
	Reviews        []Review
	Comments       []PRComment
}

// Review represents a single pull request review.
type Review struct {
	Author    string
	State     string
	Body      string
	CreatedAt time.Time
}

// PRComment represents a pull request comment. When populated via FetchPRReviewComments
// (which fetches general PR conversation comments), Path and Line are always empty/zero
// because the underlying gh query does not request inline review comment metadata.
// Extend the gh query with review thread data to populate Path and Line for inline comments.
type PRComment struct {
	Author string
	Body   string
	// Path is the commented file path for inline review comments. Empty when the data
	// source does not include inline review comment metadata (e.g., general PR comments).
	Path string
	// Line is the commented line number for inline review comments. Zero when the data
	// source does not include inline review comment metadata.
	Line int
	// Outdated is true when the comment was made on a diff that no longer applies
	// (GitHub sets position=null for such comments).
	Outdated bool
	// Resolved is true when the review thread this comment belongs to has been resolved
	// on GitHub. Resolved threads should not trigger rework.
	Resolved  bool
	CreatedAt time.Time
}

// CreateOrUpdatePR creates a GitHub PR for the given ticket and repo, or updates it if one
// already exists targeting the same head branch. jiraSite is the Atlassian site hostname or
// full base URL (e.g. "sedinfra" or "https://sedinfra.atlassian.net") used to build the
// Jira browse link in the PR body.
func CreateOrUpdatePR(ctx context.Context, repo RepoWorkspace, ticket Ticket, jiraSite string) (*PRInfo, error) {
	title := prTitle(ticket)
	body := buildPRBody(ticket, jiraSite)

	// Check whether a PR already exists for this branch.
	existing, err := findExistingPR(ctx, repo.Path, repo.Branch)
	if err != nil {
		return nil, fmt.Errorf("jira: pr: check existing: %w", err)
	}

	if existing != nil {
		// Update existing PR.
		_, err = ghExec(ctx, repo.Path, "pr", "edit", fmt.Sprintf("%d", existing.Number),
			"--title", title, "--body", body)
		if err != nil {
			return nil, fmt.Errorf("jira: pr: edit: %w", err)
		}
		existing.IsNew = false
		existing.Branch = repo.Branch
		existing.BaseBranch = repo.BaseBranch
		existing.Repo = repo.Name
		return existing, nil
	}

	// Create new PR.
	out, err := ghExec(ctx, repo.Path, "pr", "create",
		"--title", title,
		"--body", body,
		"--base", repo.BaseBranch,
		"--head", repo.Branch)
	if err != nil {
		return nil, fmt.Errorf("jira: pr: create: %w", err)
	}

	// The URL is the last non-empty line of the output.
	prURL := lastLine(out)

	// Fetch number from the newly created PR.
	info, err := fetchPRInfo(ctx, repo.Path, prURL)
	if err != nil {
		return nil, fmt.Errorf("jira: pr: fetch info after create: %w", err)
	}
	info.IsNew = true
	info.Branch = repo.Branch
	info.BaseBranch = repo.BaseBranch
	info.Repo = repo.Name
	return info, nil
}

// jiraBrowseURL returns the Jira browse URL for a ticket key. jiraSite may be a bare
// hostname ("sedinfra"), a hostname with domain ("sedinfra.atlassian.net"), or a full URL
// ("https://sedinfra.atlassian.net"). Returns an empty string when jiraSite is empty.
func jiraBrowseURL(jiraSite, ticketKey string) string {
	base := strings.TrimSpace(jiraSite)
	if base == "" {
		return ""
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		// bare hostname — check if it looks like just a subdomain (e.g. "sedinfra")
		if !strings.Contains(base, ".") {
			base = base + ".atlassian.net"
		}
		base = "https://" + base
	}
	base = strings.TrimRight(base, "/")
	return fmt.Sprintf("%s/browse/%s", base, ticketKey)
}

// buildPRBody constructs the PR description body for a Jira ticket. jiraSite is passed to
// jiraBrowseURL to build the Jira ticket link; see jiraBrowseURL for accepted formats.
func buildPRBody(ticket Ticket, jiraSite string) string {
	// Jira browse link: https://<site>.atlassian.net/browse/<key>
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — %s\n\n", ticket.Key, ticket.Summary)
	if browseURL := jiraBrowseURL(jiraSite, ticket.Key); browseURL != "" {
		fmt.Fprintf(&b, "**Jira ticket:** %s\n\n", browseURL)
	}
	if ticket.Description != "" {
		b.WriteString("### Description\n\n")
		b.WriteString(ticket.Description)
		b.WriteString("\n\n")
	}
	if ticket.AcceptanceCriteria != "" {
		b.WriteString("### Acceptance Criteria\n\n")
		b.WriteString(ticket.AcceptanceCriteria)
		b.WriteString("\n\n")
	}
	b.WriteString("---\n")
	b.WriteString("*Generated by [Nightshift](https://github.com/cedricfarinazzo/nightshift) — automated agent*\n")
	return b.String()
}

// FetchPRReviewComments fetches the current review state for a PR using `gh pr view --json`
// and appends inline review thread comments (with resolved status) via the GitHub GraphQL API.
func FetchPRReviewComments(ctx context.Context, repoPath, prURL string) (*PRReviewState, error) {
	out, err := ghExec(ctx, repoPath, "pr", "view", prURL,
		"--json", "url,state,reviewDecision,reviews,comments,number")
	if err != nil {
		return nil, fmt.Errorf("jira: pr: fetch review state: %w", err)
	}
	rs, err := parsePRReviewState(out)
	if err != nil {
		return nil, err
	}

	// Fetch inline review thread comments with isResolved via GraphQL.
	inline, err := fetchReviewThreads(ctx, repoPath, rs.Number)
	if err != nil {
		logging.Get().Warnf("jira: pr: fetch review threads for PR #%d (%s) in repo %s: %v", rs.Number, prURL, repoPath, err)
	}
	if err == nil {
		rs.Comments = append(rs.Comments, inline...)
	}
	return rs, nil
}

// fetchReviewThreads fetches per-line review thread comments via the GitHub GraphQL API.
// Each comment carries the isResolved and isOutdated status of its parent thread.
func fetchReviewThreads(ctx context.Context, repoPath string, prNumber int) ([]PRComment, error) {
	if prNumber == 0 {
		return nil, nil
	}
	query := `query($owner:String!,$repo:String!,$number:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$number){reviewThreads(first:100){nodes{isResolved isOutdated comments(first:20){nodes{author{login}body path line createdAt}}}}}}}`
	out, err := ghExec(ctx, repoPath, "api", "graphql",
		"-F", "owner={owner}",
		"-F", "repo={repo}",
		"-F", fmt.Sprintf("number=%d", prNumber),
		"-f", fmt.Sprintf("query=%s", query))
	if err != nil {
		return nil, fmt.Errorf("gh graphql review threads: %w", err)
	}
	return parseReviewThreads(out)
}

// parseReviewThreads parses the GraphQL response for review threads.
func parseReviewThreads(raw string) ([]PRComment, error) {
	var resp struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved bool `json:"isResolved"`
							IsOutdated bool `json:"isOutdated"`
							Comments   struct {
								Nodes []struct {
									Author struct {
										Login string `json:"login"`
									} `json:"author"`
									Body      string    `json:"body"`
									Path      string    `json:"path"`
									Line      *int      `json:"line"`
									CreatedAt time.Time `json:"createdAt"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse review threads: %w", err)
	}
	var comments []PRComment
	for _, thread := range resp.Data.Repository.PullRequest.ReviewThreads.Nodes {
		for _, c := range thread.Comments.Nodes {
			line := 0
			if c.Line != nil {
				line = *c.Line
			}
			comments = append(comments, PRComment{
				Author:    c.Author.Login,
				Body:      c.Body,
				Path:      c.Path,
				Line:      line,
				Outdated:  thread.IsOutdated,
				Resolved:  thread.IsResolved,
				CreatedAt: c.CreatedAt,
			})
		}
	}
	return comments, nil
}

// parsePRReviewState decodes the JSON output of `gh pr view --json ...` into a PRReviewState.
func parsePRReviewState(raw string) (*PRReviewState, error) {
	var v struct {
		URL            string `json:"url"`
		Number         int    `json:"number"`
		State          string `json:"state"`
		ReviewDecision string `json:"reviewDecision"`
		Reviews        []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			State     string    `json:"state"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"submittedAt"`
		} `json:"reviews"`
		Comments []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"createdAt"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("jira: pr: parse review state: %w", err)
	}
	rs := &PRReviewState{
		URL:            v.URL,
		Number:         v.Number,
		State:          v.State,
		ReviewDecision: v.ReviewDecision,
	}
	for _, r := range v.Reviews {
		rs.Reviews = append(rs.Reviews, Review{
			Author:    r.Author.Login,
			State:     r.State,
			Body:      r.Body,
			CreatedAt: r.CreatedAt,
		})
	}
	for _, c := range v.Comments {
		rs.Comments = append(rs.Comments, PRComment{
			Author:    c.Author.Login,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		})
	}
	return rs, nil
}

// prTitle formats the PR title for a ticket.
func prTitle(ticket Ticket) string {
	return fmt.Sprintf("[%s] %s", ticket.Key, ticket.Summary)
}

// findExistingPR checks whether a PR already exists for the given branch.
// Returns nil if none is found.
func findExistingPR(ctx context.Context, repoPath, branch string) (*PRInfo, error) {
	out, err := ghExec(ctx, repoPath, "pr", "list",
		"--head", branch,
		"--state", "open",
		"--json", "number,url",
		"--limit", "1")
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	var prs []struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("parse pr list: %w", err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &PRInfo{Number: prs[0].Number, URL: prs[0].URL}, nil
}

// fetchPRInfo retrieves number and URL for a PR by its URL.
func fetchPRInfo(ctx context.Context, repoPath, prURL string) (*PRInfo, error) {
	out, err := ghExec(ctx, repoPath, "pr", "view", prURL, "--json", "number,url")
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}
	var v struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return nil, fmt.Errorf("parse pr view: %w", err)
	}
	return &PRInfo{Number: v.Number, URL: v.URL}, nil
}

// ghExec runs a gh command in repoPath and returns trimmed combined output.
// It is a variable so tests can substitute a fake implementation.
var ghExec = func(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		subcommand := strings.Join(args, " ")
		if trimmed != "" {
			return "", fmt.Errorf("gh %s failed: %s: %w", subcommand, trimmed, err)
		}
		return "", fmt.Errorf("gh %s failed: %w", subcommand, err)
	}
	return trimmed, nil
}

// lastLine returns the last non-empty line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return s
}
