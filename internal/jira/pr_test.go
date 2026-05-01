package jira

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// ── jiraBrowseURL ────────────────────────────────────────────────────────────

func TestJiraBrowseURL(t *testing.T) {
	tests := []struct {
		site, key, want string
	}{
		{"sedinfra", "VC-9", "https://sedinfra.atlassian.net/browse/VC-9"},
		{"sedinfra.atlassian.net", "VC-1", "https://sedinfra.atlassian.net/browse/VC-1"},
		{"https://sedinfra.atlassian.net", "VC-2", "https://sedinfra.atlassian.net/browse/VC-2"},
		{"https://sedinfra.atlassian.net/", "VC-3", "https://sedinfra.atlassian.net/browse/VC-3"},
		{"http://self-hosted.example.com", "PROJ-1", "http://self-hosted.example.com/browse/PROJ-1"},
		{"", "VC-9", ""},
		{"   ", "VC-9", ""},
	}
	for _, tt := range tests {
		got := jiraBrowseURL(tt.site, tt.key)
		if got != tt.want {
			t.Errorf("jiraBrowseURL(%q, %q) = %q, want %q", tt.site, tt.key, got, tt.want)
		}
	}
}

// ── buildPRBody ───────────────────────────────────────────────────────────────

func TestBuildPRBody(t *testing.T) {
	ticket := Ticket{
		Key:                "VC-9",
		Summary:            "GitHub PR Lifecycle Management",
		Description:        "Manage GitHub PR creation and updates.",
		AcceptanceCriteria: "PRs must include Jira link.",
	}
	body := buildPRBody(ticket, "sedinfra")

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

func TestBuildPRBody_Sections(t *testing.T) {
	tests := []struct {
		name               string
		description        string
		acceptanceCriteria string
		wantDesc           bool
		wantAC             bool
	}{
		{"both", "Some description", "Some AC", true, true},
		{"no description", "", "Some AC", false, true},
		{"no AC", "Some description", "", true, false},
		{"neither", "", "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ticket := Ticket{Key: "VC-1", Summary: "Test", Description: tt.description, AcceptanceCriteria: tt.acceptanceCriteria}
			body := buildPRBody(ticket, "sedinfra")
			hasDesc := strings.Contains(body, "### Description")
			hasAC := strings.Contains(body, "### Acceptance Criteria")
			if hasDesc != tt.wantDesc {
				t.Errorf("description section present=%v, want %v", hasDesc, tt.wantDesc)
			}
			if hasAC != tt.wantAC {
				t.Errorf("acceptance criteria section present=%v, want %v", hasAC, tt.wantAC)
			}
		})
	}
}

func TestBuildPRBody_AlwaysHasJiraLinkAndAttribution(t *testing.T) {
	keys := []string{"VC-1", "VC-9", "PROJ-123"}
	for _, key := range keys {
		body := buildPRBody(Ticket{Key: key, Summary: "s"}, "sedinfra")
		if !strings.Contains(body, "atlassian.net/browse/"+key) {
			t.Errorf("body missing Jira link for key %s", key)
		}
		if !strings.Contains(body, "Nightshift") {
			t.Errorf("body missing Nightshift attribution for key %s", key)
		}
	}
}

// ── prTitle ───────────────────────────────────────────────────────────────────

func TestPRTitle(t *testing.T) {
	tests := []struct {
		key, summary, want string
	}{
		{"VC-9", "GitHub PR Lifecycle Management", "[VC-9] GitHub PR Lifecycle Management"},
		{"VC-1", "Init", "[VC-1] Init"},
		{"PROJ-42", "Fix the bug", "[PROJ-42] Fix the bug"},
	}
	for _, tt := range tests {
		ticket := Ticket{Key: tt.key, Summary: tt.summary}
		got := prTitle(ticket)
		if got != tt.want {
			t.Errorf("prTitle(%q, %q) = %q, want %q", tt.key, tt.summary, got, tt.want)
		}
	}
}

// ── lastLine ─────────────────────────────────────────────────────────────────

func TestLastLine(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"https://github.com/org/repo/pull/42", "https://github.com/org/repo/pull/42"},
		{"some output\nhttps://github.com/org/repo/pull/42", "https://github.com/org/repo/pull/42"},
		{"line1\nline2\nline3\n", "line3"},
		{"only\n\n\n", "only"},
		{"", ""},
		{"\n\n\n", "\n\n\n"}, // all-whitespace input falls back to returning s
	}
	for _, tt := range tests {
		got := lastLine(tt.input)
		if got != tt.want {
			t.Errorf("lastLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── parsePRReviewState ────────────────────────────────────────────────────────

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
	if rs.Reviews[0].Body != "LGTM" {
		t.Errorf("Reviews[0].Body = %q, want LGTM", rs.Reviews[0].Body)
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
	if rs.Comments[0].Body != "Nice work" {
		t.Errorf("Comments[0].Body = %q, want 'Nice work'", rs.Comments[0].Body)
	}
}

func TestParsePRReviewState_MultipleReviews(t *testing.T) {
	raw := `{
		"url": "https://github.com/org/repo/pull/10",
		"state": "MERGED",
		"reviewDecision": "APPROVED",
		"reviews": [
			{"author": {"login": "alice"}, "state": "APPROVED", "body": "LGTM", "submittedAt": "2026-04-01T09:00:00Z"},
			{"author": {"login": "bob"},   "state": "CHANGES_REQUESTED", "body": "needs work", "submittedAt": "2026-04-01T10:00:00Z"},
			{"author": {"login": "carol"}, "state": "COMMENTED", "body": "minor nit", "submittedAt": "2026-04-01T11:00:00Z"}
		],
		"comments": []
	}`
	rs, err := parsePRReviewState(raw)
	if err != nil {
		t.Fatalf("parsePRReviewState: %v", err)
	}
	if len(rs.Reviews) != 3 {
		t.Fatalf("len(Reviews) = %d, want 3", len(rs.Reviews))
	}
	if rs.Reviews[1].State != "CHANGES_REQUESTED" {
		t.Errorf("Reviews[1].State = %q, want CHANGES_REQUESTED", rs.Reviews[1].State)
	}
	if rs.Reviews[2].Author != "carol" {
		t.Errorf("Reviews[2].Author = %q, want carol", rs.Reviews[2].Author)
	}
	if rs.State != "MERGED" {
		t.Errorf("State = %q, want MERGED", rs.State)
	}
}

func TestParsePRReviewState_States(t *testing.T) {
	states := []string{"OPEN", "CLOSED", "MERGED"}
	for _, state := range states {
		raw := `{"url":"u","state":"` + state + `","reviewDecision":"","reviews":[],"comments":[]}`
		rs, err := parsePRReviewState(raw)
		if err != nil {
			t.Fatalf("state %s: %v", state, err)
		}
		if rs.State != state {
			t.Errorf("State = %q, want %q", rs.State, state)
		}
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

func TestParsePRReviewState_MissingFields(t *testing.T) {
	// Minimal JSON — missing optional fields should not error.
	raw := `{"url": "https://github.com/org/repo/pull/1"}`
	rs, err := parsePRReviewState(raw)
	if err != nil {
		t.Fatalf("parsePRReviewState with minimal JSON: %v", err)
	}
	if rs.URL != "https://github.com/org/repo/pull/1" {
		t.Errorf("URL = %q", rs.URL)
	}
	if rs.Reviews != nil || rs.Comments != nil {
		t.Error("expected nil slices for missing fields")
	}
}

// ── findExistingPR ────────────────────────────────────────────────────────────

func TestFindExistingPR_OpenPR(t *testing.T) {
	orig := ghExec
	defer func() { ghExec = orig }()

	ghExec = func(_ context.Context, _ string, args ...string) (string, error) {
		return `[{"number":42,"url":"https://github.com/org/repo/pull/42"}]`, nil
	}

	pr, err := findExistingPR(context.Background(), "/repo", "feature/VC-44")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil {
		t.Fatal("expected PRInfo, got nil")
		return
	}
	if pr.Number != 42 {
		t.Errorf("Number = %d, want 42", pr.Number)
	}
	if pr.URL != "https://github.com/org/repo/pull/42" {
		t.Errorf("URL = %q", pr.URL)
	}
}

func TestFindExistingPR_NoPR(t *testing.T) {
	orig := ghExec
	defer func() { ghExec = orig }()

	ghExec = func(_ context.Context, _ string, args ...string) (string, error) {
		return `[]`, nil
	}

	pr, err := findExistingPR(context.Background(), "/repo", "feature/VC-44")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr != nil {
		t.Errorf("expected nil for empty list, got %+v", pr)
	}
}

func TestFindExistingPR_StateOpenFlagPassed(t *testing.T) {
	orig := ghExec
	defer func() { ghExec = orig }()

	var capturedArgs []string
	ghExec = func(_ context.Context, _ string, args ...string) (string, error) {
		capturedArgs = args
		return `[]`, nil
	}

	_, _ = findExistingPR(context.Background(), "/repo", "feature/VC-44")

	found := false
	for i, a := range capturedArgs {
		if a == "--state" && i+1 < len(capturedArgs) && capturedArgs[i+1] == "open" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --state open in gh pr list args, got: %v", capturedArgs)
	}
}

// ── FetchPRReviewComments ─────────────────────────────────────────────────────

func TestFetchPRReviewComments_ReviewThreadsError(t *testing.T) {
	orig := ghExec
	defer func() { ghExec = orig }()

	prViewJSON := `{
		"url": "https://github.com/org/repo/pull/7",
		"state": "OPEN",
		"reviewDecision": "",
		"number": 7,
		"reviews": [],
		"comments": [
			{"author": {"login": "alice"}, "body": "top-level comment", "createdAt": "2026-04-07T10:00:00Z"}
		]
	}`

	call := 0
	ghExec = func(_ context.Context, _ string, args ...string) (string, error) {
		call++
		if call == 1 {
			return prViewJSON, nil
		}
		return "", fmt.Errorf("graphql unavailable")
	}

	// Capture stderr so we can assert the warning is logged.
	// logging.Get() with no global logger creates a default logger writing to os.Stderr.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	rs, fetchErr := FetchPRReviewComments(context.Background(), "/repo", "https://github.com/org/repo/pull/7")

	_ = w.Close()
	os.Stderr = origStderr
	var logBuf bytes.Buffer
	if _, err := io.Copy(&logBuf, r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()

	if fetchErr != nil {
		t.Fatalf("FetchPRReviewComments should not return error on graphql failure, got: %v", fetchErr)
	}
	// The top-level comment from pr view should still be present.
	if len(rs.Comments) != 1 {
		t.Errorf("len(Comments) = %d, want 1 (inline threads must not be appended on error)", len(rs.Comments))
	}
	if rs.Comments[0].Author != "alice" {
		t.Errorf("Comments[0].Author = %q, want alice", rs.Comments[0].Author)
	}
	// Verify a warning was emitted with the error and PR identity.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "graphql unavailable") {
		t.Errorf("expected warning log containing error message, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "#7") {
		t.Errorf("expected warning log containing PR number, got: %s", logOutput)
	}
}
