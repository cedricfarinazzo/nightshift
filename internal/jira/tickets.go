package jira

import (
	"context"
	"fmt"
	"strings"
	"time"

	model "github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"
)

// Ticket represents a Jira issue with all fields needed for agent context.
type Ticket struct {
	Key                string
	Summary            string
	Description        string
	Comments           []Comment
	AcceptanceCriteria string
	Labels             []string
	Status             Status
	IssueType          string // e.g. "Bug", "Story", "Task"
	IssueLinks         []IssueLink
	Reporter           string
	Assignee           string
	ParentKey          string
	ParentSummary      string
	ParentDescription  string
}

// Comment represents a single comment on a Jira issue.
type Comment struct {
	ID      string
	Author  string
	Body    string
	Created time.Time
	Updated time.Time
}

// IssueLink represents a link between two Jira issues.
type IssueLink struct {
	Type       string // e.g. "Blocks"
	InwardKey  string // the issue that blocks (inward side)
	OutwardKey string // the issue that is being blocked (outward side)
	Direction  string // "inward" or "outward" relative to this ticket
}

const searchPageSize = 50

// FetchTodoTickets fetches issues in the "To Do" status category filtered by the configured label.
func (c *Client) FetchTodoTickets(ctx context.Context) ([]Ticket, error) {
	jql := fmt.Sprintf(
		`project = "%s" AND statusCategory = "To Do" AND labels = "%s" ORDER BY created ASC`,
		c.cfg.Project, c.cfg.Label,
	)
	tickets, err := c.fetchTickets(ctx, jql)
	if err != nil {
		return nil, err
	}
	return c.fetchParentDescriptions(ctx, tickets), nil
}

// FetchReviewTickets fetches issues that are in a review status, filtered by the configured label.
func (c *Client) FetchReviewTickets(ctx context.Context, statusMap *StatusMap) ([]Ticket, error) {
	if statusMap == nil || len(statusMap.ReviewStatuses) == 0 {
		return nil, nil
	}
	names := make([]string, len(statusMap.ReviewStatuses))
	for i, s := range statusMap.ReviewStatuses {
		names[i] = fmt.Sprintf(`"%s"`, s.Name)
	}
	jql := fmt.Sprintf(
		`project = "%s" AND status in (%s) AND labels = "%s" ORDER BY created ASC`,
		c.cfg.Project, strings.Join(names, ", "), c.cfg.Label,
	)
	tickets, err := c.fetchTickets(ctx, jql)
	if err != nil {
		return nil, err
	}
	return c.fetchParentDescriptions(ctx, tickets), nil
}

// fetchTickets executes a JQL query with cursor-based pagination and returns all matching tickets.
func (c *Client) fetchTickets(ctx context.Context, jql string) ([]Ticket, error) {
	var tickets []Ticket
	fields := []string{
		"summary", "description", "comment", "labels", "status",
		"issuelinks", "reporter", "assignee", "issuetype", "parent",
	}
	nextPageToken := ""
	for {
		page, _, err := c.jira.Issue.Search.SearchJQL(ctx, jql, fields, nil, searchPageSize, nextPageToken)
		if err != nil {
			return nil, fmt.Errorf("jira: searching issues: %w", err)
		}
		for _, issue := range page.Issues {
			tickets = append(tickets, issueToTicket(issue))
		}
		nextPageToken = page.NextPageToken
		if nextPageToken == "" || len(page.Issues) == 0 {
			break
		}
	}
	return tickets, nil
}

// issueToTicket maps a go-atlassian IssueScheme to a Ticket.
func issueToTicket(issue *model.IssueScheme) Ticket {
	if issue == nil {
		return Ticket{}
	}
	t := Ticket{Key: issue.Key}
	if f := issue.Fields; f != nil {
		t.Summary = f.Summary
		t.Labels = f.Labels

		if f.Description != nil {
			t.Description = extractText(f.Description)
			t.AcceptanceCriteria = extractAcceptanceCriteria(t.Description)
		}
		if f.Status != nil {
			t.Status = Status{
				ID:   f.Status.ID,
				Name: f.Status.Name,
			}
			if f.Status.StatusCategory != nil {
				t.Status.CategoryKey = f.Status.StatusCategory.Key
			}
		}
		if f.IssueType != nil {
			t.IssueType = f.IssueType.Name
		}
		if f.Reporter != nil {
			t.Reporter = f.Reporter.DisplayName
		}
		if f.Assignee != nil {
			t.Assignee = f.Assignee.DisplayName
		}
		if f.Comment != nil {
			for _, c := range f.Comment.Comments {
				t.Comments = append(t.Comments, commentToComment(c))
			}
		}
		for _, link := range f.IssueLinks {
			t.IssueLinks = append(t.IssueLinks, issueLinkToLink(issue.Key, link))
		}
		if f.Parent != nil {
			t.ParentKey = f.Parent.Key
			if f.Parent.Fields != nil {
				t.ParentSummary = f.Parent.Fields.Summary
			}
		}
	}
	return t
}

// fetchParentDescriptions fetches the description for each unique parent key and
// populates ParentDescription on the tickets that reference it. Failures are
// non-fatal: the ticket is still usable, just without parent description.
func (c *Client) fetchParentDescriptions(ctx context.Context, tickets []Ticket) []Ticket {
	// Deduplicate parent keys.
	parentDescs := make(map[string]string)
	for _, t := range tickets {
		if t.ParentKey != "" {
			parentDescs[t.ParentKey] = ""
		}
	}
	for key := range parentDescs {
		issue, _, err := c.jira.Issue.Get(ctx, key, []string{"description"}, nil)
		if err != nil {
			c.log.Errorf("fetch parent %s description: %v", key, err)
			continue
		}
		if issue != nil && issue.Fields != nil && issue.Fields.Description != nil {
			parentDescs[key] = extractText(issue.Fields.Description)
		}
	}
	for i, t := range tickets {
		if t.ParentKey != "" {
			tickets[i].ParentDescription = parentDescs[t.ParentKey]
		}
	}
	return tickets
}

// commentToComment maps an IssueCommentScheme to a Comment.
func commentToComment(c *model.IssueCommentScheme) Comment {
	if c == nil {
		return Comment{}
	}
	cm := Comment{ID: c.ID}
	if c.Author != nil {
		cm.Author = c.Author.DisplayName
	}
	if c.Body != nil {
		cm.Body = extractText(c.Body)
	}
	cm.Created = parseJiraTime(c.Created)
	cm.Updated = parseJiraTime(c.Updated)
	return cm
}

// issueLinkToLink maps an IssueLinkScheme to an IssueLink relative to the given issue key.
func issueLinkToLink(selfKey string, link *model.IssueLinkScheme) IssueLink {
	if link == nil {
		return IssueLink{}
	}
	il := IssueLink{}
	if link.Type != nil {
		il.Type = link.Type.Name
	}
	if link.InwardIssue != nil {
		il.InwardKey = link.InwardIssue.Key
	}
	if link.OutwardIssue != nil {
		il.OutwardKey = link.OutwardIssue.Key
	}
	// Direction: outward means selfKey is the blocker (selfKey blocks OutwardKey)
	//            inward means selfKey is being blocked (InwardKey blocks selfKey)
	switch {
	case il.OutwardKey != "" && il.InwardKey == "":
		il.InwardKey = selfKey
		il.Direction = "outward"
	case il.InwardKey != "" && il.OutwardKey == "":
		il.OutwardKey = selfKey
		il.Direction = "inward"
	}
	return il
}

// parseJiraTime parses Jira timestamp strings, which use a non-RFC3339 format
// like "2006-01-02T15:04:05.000+0200" (offset without colon). Falls back to
// RFC3339 and then to zero time.
func parseJiraTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// Jira format: "2006-01-02T15:04:05.000-0700" (no colon in offset)
	if t, err := time.Parse("2006-01-02T15:04:05.000-0700", s); err == nil {
		return t
	}
	// RFC3339 with sub-seconds
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	// RFC3339 plain
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func extractText(n *model.CommentNodeScheme) string {
	if n == nil {
		return ""
	}
	if n.Text != "" {
		return n.Text
	}
	var sb strings.Builder
	for i, child := range n.Content {
		sb.WriteString(extractText(child))
		// Add newline between block-level nodes
		if i < len(n.Content)-1 && isBlockNode(child) {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func isBlockNode(n *model.CommentNodeScheme) bool {
	if n == nil {
		return false
	}
	switch n.Type {
	case "paragraph", "heading", "bulletList", "orderedList", "listItem",
		"blockquote", "codeBlock", "rule", "panel":
		return true
	}
	return false
}

// extractAcceptanceCriteria extracts the acceptance criteria section from a description string.
// It looks for a heading line that starts with "acceptance criteria" (case-insensitive) and
// returns all text until the next heading or end of string.
func extractAcceptanceCriteria(description string) string {
	const acKeyword = "acceptance criteria"
	for start := 0; start <= len(description); {
		end := strings.IndexByte(description[start:], '\n')
		line := description[start:]
		nextStart := len(description)
		if end >= 0 {
			line = description[start : start+end]
			nextStart = start + end + 1
		}

		body, ok := acceptanceCriteriaBodyFromLine(line)
		if ok {
			rest := body
			if nextStart <= len(description) && nextStart < len(description) {
				tail := description[nextStart:]
				if tail != "" {
					if rest != "" {
						rest += "\n" + tail
					} else {
						rest = tail
					}
				}
			}
			// Stop at the next section: a non-empty line that ends with ':' and contains no spaces
			// (matches "SectionName:" style headings produced by extractText).
			lines := strings.Split(rest, "\n")
			var out []string
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if len(out) > 0 && trimmed != "" && strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
					break
				}
				out = append(out, line)
			}
			return strings.TrimSpace(strings.Join(out, "\n"))
		}

		if end < 0 {
			break
		}
		start = nextStart
	}
	return ""
}

func acceptanceCriteriaBodyFromLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}

	for strings.HasPrefix(trimmed, "#") {
		trimmed = strings.TrimLeft(trimmed, "#")
		trimmed = strings.TrimLeft(trimmed, " \t")
	}

	if len(trimmed) < len("acceptance criteria") || !strings.EqualFold(trimmed[:len("acceptance criteria")], "acceptance criteria") {
		return "", false
	}

	rest := strings.TrimLeft(trimmed[len("acceptance criteria"):], ": \t")
	if rest == "" {
		return "", true
	}
	return rest, true
}
