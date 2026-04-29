package jira

import (
	"strings"
	"testing"
	"time"

	model "github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"
)

// ── extractText ───────────────────────────────────────────────────────────────

func TestExtractText_Nil(t *testing.T) {
	if got := extractText(nil); got != "" {
		t.Errorf("extractText(nil) = %q, want empty", got)
	}
}

func TestExtractText_TextNode(t *testing.T) {
	n := &model.CommentNodeScheme{Text: "hello"}
	if got := extractText(n); got != "hello" {
		t.Errorf("extractText = %q, want %q", got, "hello")
	}
}

func TestExtractText_NestedContent(t *testing.T) {
	n := &model.CommentNodeScheme{
		Content: []*model.CommentNodeScheme{
			{Text: "foo"},
			{Text: "bar"},
		},
	}
	got := extractText(n)
	if !strings.Contains(got, "foo") || !strings.Contains(got, "bar") {
		t.Errorf("extractText = %q, should contain foo and bar", got)
	}
}

func TestExtractText_BlockNodeAddsNewline(t *testing.T) {
	// Two paragraph children → newline between them
	n := &model.CommentNodeScheme{
		Content: []*model.CommentNodeScheme{
			{Type: "paragraph", Text: "para1"},
			{Type: "paragraph", Text: "para2"},
		},
	}
	got := extractText(n)
	if !strings.Contains(got, "\n") {
		t.Errorf("extractText with block nodes should contain newline, got %q", got)
	}
}

func TestExtractText_EmptyNode(t *testing.T) {
	n := &model.CommentNodeScheme{}
	if got := extractText(n); got != "" {
		t.Errorf("extractText(empty) = %q, want empty", got)
	}
}

// ── isBlockNode ───────────────────────────────────────────────────────────────

func TestIsBlockNode_Nil(t *testing.T) {
	if isBlockNode(nil) {
		t.Error("isBlockNode(nil) should return false")
	}
}

func TestIsBlockNode_Types(t *testing.T) {
	blockTypes := []string{"paragraph", "heading", "bulletList", "orderedList", "listItem", "blockquote", "codeBlock", "rule", "panel"}
	for _, typ := range blockTypes {
		n := &model.CommentNodeScheme{Type: typ}
		if !isBlockNode(n) {
			t.Errorf("isBlockNode(%q) = false, want true", typ)
		}
	}
	inlineTypes := []string{"text", "hardBreak", "mention", "emoji", ""}
	for _, typ := range inlineTypes {
		n := &model.CommentNodeScheme{Type: typ}
		if isBlockNode(n) {
			t.Errorf("isBlockNode(%q) = true, want false", typ)
		}
	}
}

// ── extractAcceptanceCriteria ─────────────────────────────────────────────────

func TestExtractAcceptanceCriteria_NotPresent(t *testing.T) {
	got := extractAcceptanceCriteria("No AC section here")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractAcceptanceCriteria_Present(t *testing.T) {
	desc := "## Objective\nDo the thing.\n\nAcceptance Criteria\n- Item 1\n- Item 2\n"
	got := extractAcceptanceCriteria(desc)
	if !strings.Contains(got, "Item 1") || !strings.Contains(got, "Item 2") {
		t.Errorf("extractAcceptanceCriteria = %q, want items", got)
	}
}

func TestExtractAcceptanceCriteria_StopsAtNextSection(t *testing.T) {
	desc := "Acceptance Criteria\n- must pass\n\nNextSection:\nother content"
	got := extractAcceptanceCriteria(desc)
	if strings.Contains(got, "other content") {
		t.Errorf("extractAcceptanceCriteria should stop at next section, got %q", got)
	}
	if !strings.Contains(got, "must pass") {
		t.Errorf("extractAcceptanceCriteria should contain 'must pass', got %q", got)
	}
}

func TestExtractAcceptanceCriteria_NoNewlineAfterHeading(t *testing.T) {
	// "acceptance criteria" keyword only, no content after → still empty
	got := extractAcceptanceCriteria("acceptance criteria")
	if got != "" {
		t.Errorf("expected empty when heading only with no content, got %q", got)
	}
}

func TestExtractAcceptanceCriteria_ContentAfterHeadingNoNewline(t *testing.T) {
	// Heading with inline content and no trailing newline — this was the silent-drop bug
	desc := "Acceptance Criteria: must pass all checks"
	got := extractAcceptanceCriteria(desc)
	if !strings.Contains(got, "must pass all checks") {
		t.Errorf("expected body content, got %q", got)
	}
}

func TestExtractAcceptanceCriteria_HeadingAtEndNoTrailingNewline(t *testing.T) {
	// Heading followed by items but no trailing newline — regression guard
	desc := "Do the thing.\n\nAcceptance Criteria\n- item1\n- item2"
	got := extractAcceptanceCriteria(desc)
	if !strings.Contains(got, "item1") || !strings.Contains(got, "item2") {
		t.Errorf("expected items, got %q", got)
	}
}

// ── commentToComment ──────────────────────────────────────────────────────────

func TestCommentToComment_Nil(t *testing.T) {
	got := commentToComment(nil)
	if got.ID != "" || got.Author != "" || got.Body != "" {
		t.Errorf("commentToComment(nil) = %+v, want zero value", got)
	}
}

func TestCommentToComment_Full(t *testing.T) {
	c := &model.IssueCommentScheme{
		ID:      "42",
		Author:  &model.UserScheme{DisplayName: "Alice"},
		Body:    &model.CommentNodeScheme{Text: "great work"},
		Created: "2026-01-15T10:00:00Z",
		Updated: "2026-01-15T11:00:00Z",
	}
	got := commentToComment(c)
	if got.ID != "42" {
		t.Errorf("ID = %q, want 42", got.ID)
	}
	if got.Author != "Alice" {
		t.Errorf("Author = %q, want Alice", got.Author)
	}
	if got.Body != "great work" {
		t.Errorf("Body = %q, want 'great work'", got.Body)
	}
	if got.Created.IsZero() {
		t.Error("Created should be parsed")
	}
}

func TestCommentToComment_NilAuthorAndBody(t *testing.T) {
	c := &model.IssueCommentScheme{ID: "1"}
	got := commentToComment(c)
	if got.Author != "" {
		t.Errorf("Author = %q, want empty for nil author", got.Author)
	}
	if got.Body != "" {
		t.Errorf("Body = %q, want empty for nil body", got.Body)
	}
}

func TestCommentToComment_InvalidTimestamp(t *testing.T) {
	c := &model.IssueCommentScheme{ID: "2", Created: "not-a-date", Updated: "also-bad"}
	got := commentToComment(c)
	// Should not panic; Created/Updated stay zero.
	if !got.Created.Equal(time.Time{}) {
		t.Errorf("Created should be zero for invalid timestamp, got %v", got.Created)
	}
}

// ── issueLinkToLink ───────────────────────────────────────────────────────────

func TestIssueLinkToLink_Nil(t *testing.T) {
	got := issueLinkToLink("PROJ-1", nil)
	if got.Type != "" || got.InwardKey != "" || got.OutwardKey != "" {
		t.Errorf("issueLinkToLink(nil) = %+v, want zero", got)
	}
}

func TestIssueLinkToLink_OutwardDirection(t *testing.T) {
	link := &model.IssueLinkScheme{
		Type:         &model.LinkTypeScheme{Name: "Blocks"},
		OutwardIssue: &model.LinkedIssueScheme{Key: "PROJ-2"},
	}
	got := issueLinkToLink("PROJ-1", link)
	if got.Type != "Blocks" {
		t.Errorf("Type = %q, want Blocks", got.Type)
	}
	if got.OutwardKey != "PROJ-2" {
		t.Errorf("OutwardKey = %q, want PROJ-2", got.OutwardKey)
	}
	if got.InwardKey != "PROJ-1" {
		t.Errorf("InwardKey = %q, want PROJ-1 (selfKey)", got.InwardKey)
	}
	if got.Direction != "outward" {
		t.Errorf("Direction = %q, want outward", got.Direction)
	}
}

func TestIssueLinkToLink_InwardDirection(t *testing.T) {
	link := &model.IssueLinkScheme{
		Type:        &model.LinkTypeScheme{Name: "Blocks"},
		InwardIssue: &model.LinkedIssueScheme{Key: "PROJ-0"},
	}
	got := issueLinkToLink("PROJ-1", link)
	if got.InwardKey != "PROJ-0" {
		t.Errorf("InwardKey = %q, want PROJ-0", got.InwardKey)
	}
	if got.OutwardKey != "PROJ-1" {
		t.Errorf("OutwardKey = %q, want PROJ-1 (selfKey)", got.OutwardKey)
	}
	if got.Direction != "inward" {
		t.Errorf("Direction = %q, want inward", got.Direction)
	}
}

func TestIssueLinkToLink_NilType(t *testing.T) {
	link := &model.IssueLinkScheme{
		OutwardIssue: &model.LinkedIssueScheme{Key: "PROJ-2"},
	}
	got := issueLinkToLink("PROJ-1", link)
	if got.Type != "" {
		t.Errorf("Type = %q, want empty for nil type", got.Type)
	}
}

// ── issueToTicket ─────────────────────────────────────────────────────────────

func TestIssueToTicket_Nil(t *testing.T) {
	got := issueToTicket(nil)
	if got.Key != "" {
		t.Errorf("issueToTicket(nil) = %+v, want zero Ticket", got)
	}
}

func TestIssueToTicket_MinimalFields(t *testing.T) {
	issue := &model.IssueScheme{Key: "PROJ-1"}
	got := issueToTicket(issue)
	if got.Key != "PROJ-1" {
		t.Errorf("Key = %q, want PROJ-1", got.Key)
	}
}

func TestIssueToTicket_FullFields(t *testing.T) {
	issue := &model.IssueScheme{
		Key: "PROJ-42",
		Fields: &model.IssueFieldsScheme{
			Summary: "Do the thing",
			Labels:  []string{"nightshift", "backend"},
			Description: &model.CommentNodeScheme{
				Content: []*model.CommentNodeScheme{
					{Type: "paragraph", Text: "Implement the feature."},
				},
			},
			Status: &model.StatusScheme{
				ID:   "10001",
				Name: "In Progress",
				StatusCategory: &model.StatusCategoryScheme{
					Key: "indeterminate",
				},
			},
			Reporter: &model.UserScheme{DisplayName: "Alice"},
			Assignee: &model.UserScheme{DisplayName: "Bob"},
		},
	}
	got := issueToTicket(issue)
	if got.Key != "PROJ-42" {
		t.Errorf("Key = %q", got.Key)
	}
	if got.Summary != "Do the thing" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if len(got.Labels) != 2 {
		t.Errorf("Labels = %v, want 2", got.Labels)
	}
	if got.Description == "" {
		t.Error("Description should not be empty")
	}
	if got.Status.Name != "In Progress" {
		t.Errorf("Status.Name = %q, want In Progress", got.Status.Name)
	}
	if got.Status.CategoryKey != "indeterminate" {
		t.Errorf("Status.CategoryKey = %q, want indeterminate", got.Status.CategoryKey)
	}
	if got.Reporter != "Alice" {
		t.Errorf("Reporter = %q, want Alice", got.Reporter)
	}
	if got.Assignee != "Bob" {
		t.Errorf("Assignee = %q, want Bob", got.Assignee)
	}
}

func TestIssueToTicket_WithComments(t *testing.T) {
	issue := &model.IssueScheme{
		Key: "PROJ-5",
		Fields: &model.IssueFieldsScheme{
			Comment: &model.IssueCommentPageScheme{
				Comments: []*model.IssueCommentScheme{
					{ID: "1", Author: &model.UserScheme{DisplayName: "Eve"}, Body: &model.CommentNodeScheme{Text: "LGTM"}},
					{ID: "2", Author: &model.UserScheme{DisplayName: "Frank"}, Body: &model.CommentNodeScheme{Text: "Please fix"}},
				},
			},
		},
	}
	got := issueToTicket(issue)
	if len(got.Comments) != 2 {
		t.Fatalf("Comments = %d, want 2", len(got.Comments))
	}
	if got.Comments[0].Author != "Eve" {
		t.Errorf("Comments[0].Author = %q, want Eve", got.Comments[0].Author)
	}
	if got.Comments[1].Body != "Please fix" {
		t.Errorf("Comments[1].Body = %q", got.Comments[1].Body)
	}
}

func TestIssueToTicket_WithIssueLinks(t *testing.T) {
	issue := &model.IssueScheme{
		Key: "PROJ-6",
		Fields: &model.IssueFieldsScheme{
			IssueLinks: []*model.IssueLinkScheme{
				{
					Type:         &model.LinkTypeScheme{Name: "Blocks"},
					OutwardIssue: &model.LinkedIssueScheme{Key: "PROJ-7"},
				},
			},
		},
	}
	got := issueToTicket(issue)
	if len(got.IssueLinks) != 1 {
		t.Fatalf("IssueLinks = %d, want 1", len(got.IssueLinks))
	}
	if got.IssueLinks[0].Type != "Blocks" {
		t.Errorf("IssueLinks[0].Type = %q, want Blocks", got.IssueLinks[0].Type)
	}
}

func TestIssueToTicket_WithAcceptanceCriteria(t *testing.T) {
	issue := &model.IssueScheme{
		Key: "PROJ-7",
		Fields: &model.IssueFieldsScheme{
			Description: &model.CommentNodeScheme{
				Text: "## Objective\nDo work.\n\nAcceptance Criteria\n- Must pass tests\n",
			},
		},
	}
	got := issueToTicket(issue)
	if got.AcceptanceCriteria == "" {
		t.Error("AcceptanceCriteria should be extracted from description")
	}
}

func TestIssueToTicket_StatusWithoutCategory(t *testing.T) {
	issue := &model.IssueScheme{
		Key: "PROJ-8",
		Fields: &model.IssueFieldsScheme{
			Status: &model.StatusScheme{ID: "10000", Name: "Open"},
		},
	}
	got := issueToTicket(issue)
	if got.Status.Name != "Open" {
		t.Errorf("Status.Name = %q, want Open", got.Status.Name)
	}
	if got.Status.CategoryKey != "" {
		t.Errorf("CategoryKey = %q, want empty when StatusCategory is nil", got.Status.CategoryKey)
	}
}

func TestIssueLinkToLink_BothKeys(t *testing.T) {
	link := &model.IssueLinkScheme{
		Type:         &model.LinkTypeScheme{Name: "Relates"},
		InwardIssue:  &model.LinkedIssueScheme{Key: "PROJ-0"},
		OutwardIssue: &model.LinkedIssueScheme{Key: "PROJ-2"},
	}
	got := issueLinkToLink("PROJ-1", link)
	if got.InwardKey != "PROJ-0" || got.OutwardKey != "PROJ-2" {
		t.Errorf("got %+v, want InwardKey=PROJ-0 OutwardKey=PROJ-2", got)
	}
	if got.Direction != "" {
		t.Errorf("Direction = %q, want empty when both keys set", got.Direction)
	}
}
