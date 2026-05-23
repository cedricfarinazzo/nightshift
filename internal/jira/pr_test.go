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

// fakeGHRunner is a function-typed adapter satisfying GHRunner, for use in tests.
type fakeGHRunner func(ctx context.Context, repoPath string, args ...string) (string, error)

func (f fakeGHRunner) Run(ctx context.Context, repoPath string, args ...string) (string, error) {
	return f(ctx, repoPath, args...)
}

// ── findExistingPR ────────────────────────────────────────────────────────────

func TestFindExistingPR_OpenPR(t *testing.T) {
	t.Parallel()
	pc := NewPRClient(WithGHRunner(fakeGHRunner(func(_ context.Context, _ string, _ ...string) (string, error) {
		return `[{"number":42,"url":"https://github.com/org/repo/pull/42"}]`, nil
	})))

	pr, err := pc.findExistingPR(context.Background(), "/repo", "feature/VC-44")
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
	t.Parallel()
	pc := NewPRClient(WithGHRunner(fakeGHRunner(func(_ context.Context, _ string, _ ...string) (string, error) {
		return `[]`, nil
	})))

	pr, err := pc.findExistingPR(context.Background(), "/repo", "feature/VC-44")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr != nil {
		t.Errorf("expected nil for empty list, got %+v", pr)
	}
}

func TestFindExistingPR_StateOpenFlagPassed(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	pc := NewPRClient(WithGHRunner(fakeGHRunner(func(_ context.Context, _ string, args ...string) (string, error) {
		capturedArgs = args
		return `[]`, nil
	})))

	_, _ = pc.findExistingPR(context.Background(), "/repo", "feature/VC-44")

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
	t.Parallel()

	prViewJSON := `{
		"url": "https://github.com/org/repo/pull/7",
		"state": "OPEN",
		"reviewDecision": "",
		"number": 7,
		"headRefOid": "abc123def456",
		"reviews": [],
		"comments": [
			{"author": {"login": "alice"}, "body": "top-level comment", "createdAt": "2026-04-07T10:00:00Z"}
		]
	}`

	call := 0
	pc := NewPRClient(WithGHRunner(fakeGHRunner(func(_ context.Context, _ string, args ...string) (string, error) {
		call++
		if call == 1 {
			return prViewJSON, nil
		}
		return "", fmt.Errorf("graphql unavailable")
	})))

	// Capture stderr so we can assert the warning is logged.
	// logging.Get() with no global logger creates a default logger writing to os.Stderr.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	rs, fetchErr := pc.FetchPRReviewComments(context.Background(), "/repo", "https://github.com/org/repo/pull/7")

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

// ── parseCheckRuns ────────────────────────────────────────────────────────────

func TestParseCheckRuns_ValidFailing(t *testing.T) {
	raw := `{
		"check_runs": [
			{"name": "go-lint", "status": "completed", "conclusion": "failure", "details_url": "https://github.com/org/repo/runs/1"},
			{"name": "go-test", "status": "completed", "conclusion": "timed_out", "details_url": "https://github.com/org/repo/runs/2"},
			{"name": "security-scan", "status": "completed", "conclusion": "action_required", "details_url": "https://github.com/org/repo/runs/3"}
		]
	}`
	runs, err := parseCheckRuns(raw)
	if err != nil {
		t.Fatalf("parseCheckRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("len(runs) = %d, want 3", len(runs))
	}
	if runs[0].Name != "go-lint" || runs[0].Conclusion != "failure" {
		t.Errorf("runs[0] = {%q, %q}, want {go-lint, failure}", runs[0].Name, runs[0].Conclusion)
	}
	if runs[1].Conclusion != "timed_out" {
		t.Errorf("runs[1].Conclusion = %q, want timed_out", runs[1].Conclusion)
	}
	if runs[2].Conclusion != "action_required" {
		t.Errorf("runs[2].Conclusion = %q, want action_required", runs[2].Conclusion)
	}
}

func TestParseCheckRuns_SkipNonFailing(t *testing.T) {
	raw := `{
		"check_runs": [
			{"name": "lint", "status": "completed", "conclusion": "success", "details_url": "https://github.com/org/repo/runs/1"},
			{"name": "test", "status": "completed", "conclusion": "neutral", "details_url": "https://github.com/org/repo/runs/2"},
			{"name": "skip", "status": "completed", "conclusion": "skipped", "details_url": "https://github.com/org/repo/runs/3"}
		]
	}`
	runs, err := parseCheckRuns(raw)
	if err != nil {
		t.Fatalf("parseCheckRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("len(runs) = %d, want 0 (success/neutral/skipped should be filtered)", len(runs))
	}
}

func TestParseCheckRuns_SkipIncompleted(t *testing.T) {
	raw := `{
		"check_runs": [
			{"name": "pending", "status": "in_progress", "conclusion": "failure", "details_url": "https://github.com/org/repo/runs/1"},
			{"name": "queued", "status": "queued", "conclusion": "", "details_url": "https://github.com/org/repo/runs/2"},
			{"name": "completed-fail", "status": "completed", "conclusion": "failure", "details_url": "https://github.com/org/repo/runs/3"}
		]
	}`
	runs, err := parseCheckRuns(raw)
	if err != nil {
		t.Fatalf("parseCheckRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("len(runs) = %d, want 1 (only completed+failing counted)", len(runs))
	}
	if runs[0].Name != "completed-fail" {
		t.Errorf("runs[0].Name = %q, want completed-fail", runs[0].Name)
	}
}

func TestParseCheckRuns_MixedResults(t *testing.T) {
	raw := `{
		"check_runs": [
			{"name": "success", "status": "completed", "conclusion": "success", "details_url": "https://github.com/org/repo/runs/1"},
			{"name": "fail", "status": "completed", "conclusion": "failure", "details_url": "https://github.com/org/repo/runs/2"},
			{"name": "timeout", "status": "completed", "conclusion": "timed_out", "details_url": "https://github.com/org/repo/runs/3"},
			{"name": "pending", "status": "in_progress", "conclusion": "failure", "details_url": "https://github.com/org/repo/runs/4"},
			{"name": "cancel", "status": "completed", "conclusion": "cancelled", "details_url": "https://github.com/org/repo/runs/5"}
		]
	}`
	runs, err := parseCheckRuns(raw)
	if err != nil {
		t.Fatalf("parseCheckRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Errorf("len(runs) = %d, want 3 (fail, timeout, cancel)", len(runs))
	}
	conclusions := make([]string, len(runs))
	for i, r := range runs {
		conclusions[i] = r.Conclusion
	}
	expected := []string{"failure", "timed_out", "cancelled"}
	for i, exp := range expected {
		if i >= len(conclusions) || conclusions[i] != exp {
			t.Errorf("conclusions[%d] = %q, want %q", i, conclusions[i], exp)
		}
	}
}

func TestParseCheckRuns_Empty(t *testing.T) {
	raw := `{"check_runs": []}`
	runs, err := parseCheckRuns(raw)
	if err != nil {
		t.Fatalf("parseCheckRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("len(runs) = %d, want 0", len(runs))
	}
	if runs == nil {
		t.Error("expected empty slice, got nil")
	}
}

func TestParseCheckRuns_InvalidJSON(t *testing.T) {
	raw := `not json`
	_, err := parseCheckRuns(raw)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestParseCheckRuns_MissingFields(t *testing.T) {
	// Minimal valid JSON — missing optional fields should not error.
	raw := `{"check_runs": [{"name": "test"}]}`
	runs, err := parseCheckRuns(raw)
	if err != nil {
		t.Fatalf("parseCheckRuns with minimal JSON: %v", err)
	}
	// Missing conclusion and status means it won't be included (status not "completed")
	if len(runs) != 0 {
		t.Errorf("len(runs) = %d, want 0 (missing status/conclusion)", len(runs))
	}
}

func TestParseCheckRuns_PreservesURLs(t *testing.T) {
	raw := `{
		"check_runs": [
			{"name": "ci", "status": "completed", "conclusion": "failure", "details_url": "https://example.com/run/123"}
		]
	}`
	runs, err := parseCheckRuns(raw)
	if err != nil {
		t.Fatalf("parseCheckRuns: %v", err)
	}
	if runs[0].LogsURL != "https://example.com/run/123" {
		t.Errorf("LogsURL = %q, want https://example.com/run/123", runs[0].LogsURL)
	}
}

// ── HasMergeConflict ──────────────────────────────────────────────────────────

func TestHasMergeConflict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ghOutput string
		ghErr    error
		wantBool bool
		wantErr  bool
	}{
		{"conflicting", "CONFLICTING", nil, true, false},
		{"mergeable", "MERGEABLE", nil, false, false},
		{"unknown", "UNKNOWN", nil, false, false},
		{"whitespace trimmed", "CONFLICTING\n", nil, true, false},
		{"api error", "", fmt.Errorf("gh failed"), false, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pc := NewPRClient(WithGHRunner(fakeGHRunner(func(_ context.Context, _ string, _ ...string) (string, error) {
				return tt.ghOutput, tt.ghErr
			})))

			got, err := pc.HasMergeConflict(context.Background(), "/repo", 42)
			if (err != nil) != tt.wantErr {
				t.Fatalf("HasMergeConflict() err=%v, wantErr=%v", err, tt.wantErr)
			}
			if err == nil && got != tt.wantBool {
				t.Errorf("HasMergeConflict() = %v, want %v", got, tt.wantBool)
			}
		})
	}
}

func TestFetchPRReviewComments_SetsHasConflict(t *testing.T) {
	t.Parallel()
	pc := NewPRClient(WithGHRunner(fakeGHRunner(func(_ context.Context, _ string, args ...string) (string, error) {
		// GraphQL review threads call
		if contains(args, "graphql") {
			return `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`, nil
		}
		// check-runs API call
		if len(args) > 1 && args[0] == "api" && strings.HasPrefix(args[1], "repos/") {
			return `{"check_runs":[]}`, nil
		}
		// mergeable call: gh pr view <N> --json mergeable --jq .mergeable
		if contains(args, "--jq") {
			return "CONFLICTING", nil
		}
		// First call: gh pr view <url> --json url,state,...
		if contains(args, "--json") {
			return `{"url":"https://github.com/org/repo/pull/1","number":1,"state":"OPEN","reviewDecision":"","headRefOid":"abc123","reviews":[],"comments":[]}`, nil
		}
		return "", fmt.Errorf("unexpected gh call: %v", args)
	})))

	rs, err := pc.FetchPRReviewComments(context.Background(), "/repo", "https://github.com/org/repo/pull/1")
	if err != nil {
		t.Fatalf("FetchPRReviewComments: %v", err)
	}
	if !rs.HasConflict {
		t.Error("HasConflict should be true when gh returns CONFLICTING")
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
