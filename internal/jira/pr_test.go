package jira

import (
	"strings"
	"testing"
	"time"
)

func TestBuildPRBody(t *testing.T) {
	ticket := Ticket{
		Key:                "VC-9",
		Summary:            "GitHub PR Lifecycle Management",
		Description:        "Manage GitHub PR creation and updates.",
		AcceptanceCriteria: "PRs must include Jira link.",
	}
	body := buildPRBody(ticket)

	if !strings.Contains(body, "atlassian.net/browse/VC-9") {
		t.Error("PR body must contain Jira browse URL")
	}
	if !strings.Contains(body, "Nightshift") {
		t.Error("PR body must contain Nightshift attribution")
	}
	if !strings.Contains(body, ticket.Summary) {
		t.Error("PR body must contain ticket summary")
	}
	if !strings.Contains(body, ticket.Description) {
		t.Error("PR body must contain ticket description")
	}
	if !strings.Contains(body, ticket.AcceptanceCriteria) {
		t.Error("PR body must contain acceptance criteria")
	}
}

func TestBuildPRBody_NoDescription(t *testing.T) {
	ticket := Ticket{Key: "VC-1", Summary: "Minimal ticket"}
	body := buildPRBody(ticket)
	if !strings.Contains(body, "atlassian.net/browse/VC-1") {
		t.Error("PR body must contain Jira browse URL even without description")
	}
}

func TestPRTitle(t *testing.T) {
	tests := []struct {
		key, summary, want string
	}{
		{"VC-9", "GitHub PR Lifecycle Management", "[VC-9] GitHub PR Lifecycle Management"},
		{"VC-1", "Init", "[VC-1] Init"},
	}
	for _, tt := range tests {
		ticket := Ticket{Key: tt.key, Summary: tt.summary}
		got := prTitle(ticket)
		if got != tt.want {
			t.Errorf("prTitle(%q, %q) = %q, want %q", tt.key, tt.summary, got, tt.want)
		}
	}
}

func TestParsePRReviewState(t *testing.T) {
	raw := `{
		"url": "https://github.com/org/repo/pull/42",
		"state": "OPEN",
		"reviewDecision": "APPROVED",
		"reviews": [
			{
				"author": {"login": "alice"},
				"state": "APPROVED",
				"body": "LGTM",
				"submittedAt": "2026-04-07T10:00:00Z"
			}
		],
		"comments": [
			{
				"author": {"login": "bob"},
				"body": "Nice work",
				"createdAt": "2026-04-07T11:00:00Z"
			}
		]
	}`

	rs, err := parsePRReviewState(raw)
	if err != nil {
		t.Fatalf("parsePRReviewState: %v", err)
	}

	if rs.URL != "https://github.com/org/repo/pull/42" {
		t.Errorf("URL = %q, want https://github.com/org/repo/pull/42", rs.URL)
	}
	if rs.State != "OPEN" {
		t.Errorf("State = %q, want OPEN", rs.State)
	}
	if rs.ReviewDecision != "APPROVED" {
		t.Errorf("ReviewDecision = %q, want APPROVED", rs.ReviewDecision)
	}
	if len(rs.Reviews) != 1 {
		t.Fatalf("len(Reviews) = %d, want 1", len(rs.Reviews))
	}
	if rs.Reviews[0].Author != "alice" {
		t.Errorf("Reviews[0].Author = %q, want alice", rs.Reviews[0].Author)
	}
	if rs.Reviews[0].State != "APPROVED" {
		t.Errorf("Reviews[0].State = %q, want APPROVED", rs.Reviews[0].State)
	}
	wantTime := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	if !rs.Reviews[0].CreatedAt.Equal(wantTime) {
		t.Errorf("Reviews[0].CreatedAt = %v, want %v", rs.Reviews[0].CreatedAt, wantTime)
	}
	if len(rs.Comments) != 1 {
		t.Fatalf("len(Comments) = %d, want 1", len(rs.Comments))
	}
	if rs.Comments[0].Author != "bob" {
		t.Errorf("Comments[0].Author = %q, want bob", rs.Comments[0].Author)
	}
}

func TestParsePRReviewState_Empty(t *testing.T) {
	raw := `{"url":"","state":"","reviewDecision":"","reviews":[],"comments":[]}`
	rs, err := parsePRReviewState(raw)
	if err != nil {
		t.Fatalf("parsePRReviewState: %v", err)
	}
	if rs.Reviews != nil {
		t.Errorf("expected nil Reviews slice, got %v", rs.Reviews)
	}
	if rs.Comments != nil {
		t.Errorf("expected nil Comments slice, got %v", rs.Comments)
	}
}

func TestParsePRReviewState_InvalidJSON(t *testing.T) {
	_, err := parsePRReviewState("not json")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
