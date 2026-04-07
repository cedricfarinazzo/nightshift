package jira

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ── filterNewComments ─────────────────────────────────────────────────────────

func TestFilterNewComments(t *testing.T) {
	now := time.Now()
	old := PRComment{Body: "old", CreatedAt: now.Add(-2 * time.Hour)}
	mid := PRComment{Body: "mid", CreatedAt: now.Add(-30 * time.Minute)}
	fresh := PRComment{Body: "fresh", CreatedAt: now.Add(-5 * time.Minute)}
	all := []PRComment{old, mid, fresh}

	tests := []struct {
		name     string
		lastSeen time.Time
		want     int
	}{
		{"zero time returns all", time.Time{}, 3},
		{"cutoff before all returns all", now.Add(-3 * time.Hour), 3},
		{"cutoff between old and mid returns 2", now.Add(-1 * time.Hour), 2},
		{"cutoff just before fresh returns 1", now.Add(-10 * time.Minute), 1},
		{"future cutoff returns none", now.Add(time.Hour), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterNewComments(all, tt.lastSeen)
			if len(got) != tt.want {
				t.Errorf("got %d comments, want %d", len(got), tt.want)
			}
		})
	}
}

// ── buildReworkPrompt ─────────────────────────────────────────────────────────

func TestBuildReworkPrompt(t *testing.T) {
	ticket := Ticket{Key: "VC-10", Summary: "feedback loop"}
	review := &PRReviewState{
		URL:            "https://github.com/org/repo/pull/42",
		ReviewDecision: "CHANGES_REQUESTED",
		Reviews: []Review{
			{Author: "alice", State: "CHANGES_REQUESTED", Body: "fix the nil check"},
			{Author: "bob", State: "APPROVED", Body: "looks good"},
			{Author: "carol", State: "COMMENTED", Body: "consider renaming"},
		},
		Comments: []PRComment{
			{Path: "main.go", Line: 42, Author: "alice", Body: "add error handling here"},
			{Author: "dave", Body: "general comment no path"}, // no path — should be omitted
		},
	}
	repo := RepoWorkspace{Name: "nightshift", Branch: "feature/VC-10"}

	prompt := buildReworkPrompt(ticket, review, repo)

	mustContain := []string{
		"VC-10",
		"https://github.com/org/repo/pull/42",
		"nightshift",
		"feature/VC-10",
		"fix the nil check",          // CHANGES_REQUESTED review included
		"consider renaming",          // COMMENTED review included
		"main.go:42",                 // inline comment path:line
		"add error handling here",    // inline comment body
		"Address ALL reviewer feedback",
	}
	for _, want := range mustContain {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}

	// APPROVED review body must NOT appear.
	if strings.Contains(prompt, "looks good") {
		t.Error("prompt should not include APPROVED review body")
	}
	// General comment without path must NOT appear as inline.
	if strings.Contains(prompt, "general comment no path") {
		t.Error("prompt should not include general comments in inline section")
	}
}

// ── buildPRReworkComment ──────────────────────────────────────────────────────

func TestBuildPRReworkComment(t *testing.T) {
	output := "Fixed nil dereference in handler.go"
	comment := buildPRReworkComment(output)

	for _, want := range []string{
		"🤖",
		"Nightshift",
		output,
		"Nightshift._",
	} {
		if !strings.Contains(comment, want) {
			t.Errorf("comment missing %q", want)
		}
	}
}

func TestBuildPRReworkComment_EmptyOutput(t *testing.T) {
	comment := buildPRReworkComment("")
	if !strings.Contains(comment, "Nightshift") {
		t.Error("comment missing Nightshift attribution")
	}
	if strings.Contains(comment, "### Summary") {
		t.Error("empty output should not produce a Summary section")
	}
}

// ── ProcessFeedback ───────────────────────────────────────────────────────────

// newFeedbackOrchestrator builds an Orchestrator wired for feedback tests.
// All injectable functions are replaced with no-ops or stubs.
func newFeedbackOrchestrator(
	sc *stubJiraClient,
	reviewAgent *stubAgent,
	fnFindPR func(context.Context, string, string) (*PRInfo, error),
	fnFetchReviews func(context.Context, string, string) (*PRReviewState, error),
	fnCommitAndPush func(context.Context, string, string) error,
	fnPostPRComment func(context.Context, string, string, string) error,
) *Orchestrator {
	o := &Orchestrator{
		client:         sc,
		cfg:            JiraConfig{},
		reviewFixAgent: reviewAgent,
		fnHasChanges: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
		fnCommitAndPush: fnCommitAndPush,
		fnCreatePR: func(_ context.Context, _ RepoWorkspace, _ Ticket, _ string) (*PRInfo, error) {
			return nil, nil
		},
		fnFindPR:        fnFindPR,
		fnFetchReviews:  fnFetchReviews,
		fnPostPRComment: fnPostPRComment,
	}
	return o
}

func noPRComment(_ context.Context, _, _, _ string) error { return nil }
func noCommit(_ context.Context, _, _ string) error       { return nil }

func TestProcessFeedback_NilWorkspace(t *testing.T) {
	sc := &stubJiraClient{}
	ra := &stubAgent{name: "fix", output: "fixed"}
	o := newFeedbackOrchestrator(sc, ra, nil, nil, noCommit, noPRComment)

	result, err := o.ProcessFeedback(context.Background(), Ticket{Key: "X-1"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FixesMade != 0 {
		t.Errorf("FixesMade = %d, want 0", result.FixesMade)
	}
	if len(sc.postCommentCalls) != 0 {
		t.Errorf("expected no Jira comment, got %d", len(sc.postCommentCalls))
	}
}

func TestProcessFeedback_NoAgent(t *testing.T) {
	sc := &stubJiraClient{}
	o := &Orchestrator{
		client: sc,
		cfg:    JiraConfig{},
		// both reviewFixAgent and implAgent are nil
	}

	_, err := o.ProcessFeedback(context.Background(), Ticket{Key: "X-1"}, &Workspace{})
	if err == nil {
		t.Fatal("expected error when no agent is available")
	}
}

func TestProcessFeedback_NoPR(t *testing.T) {
	sc := &stubJiraClient{}
	ra := &stubAgent{name: "fix", output: "fixed"}
	agentCalled := false
	ra.err = nil

	fnFindPR := func(_ context.Context, _, _ string) (*PRInfo, error) { return nil, nil }
	fnFetchReviews := func(_ context.Context, _, _ string) (*PRReviewState, error) {
		return nil, errors.New("should not be called")
	}
	o := newFeedbackOrchestrator(sc, &stubAgent{name: "fix"}, fnFindPR, fnFetchReviews, noCommit, noPRComment)
	_ = agentCalled

	ws := &Workspace{
		TicketKey: "X-1",
		Repos:     []RepoWorkspace{{Name: "repo", Path: "/tmp/repo", Branch: "feature/X-1"}},
	}
	result, err := o.ProcessFeedback(context.Background(), Ticket{Key: "X-1"}, ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FixesMade != 0 {
		t.Errorf("FixesMade = %d, want 0", result.FixesMade)
	}
}

func TestProcessFeedback_NoChangesRequested(t *testing.T) {
	sc := &stubJiraClient{}
	ra := &stubAgent{name: "fix", output: "fixed"}

	fnFindPR := func(_ context.Context, _, _ string) (*PRInfo, error) {
		return &PRInfo{URL: "https://github.com/org/repo/pull/1", Number: 1}, nil
	}
	fnFetchReviews := func(_ context.Context, _, _ string) (*PRReviewState, error) {
		return &PRReviewState{
			URL:            "https://github.com/org/repo/pull/1",
			ReviewDecision: "APPROVED",
			Reviews:        []Review{{Author: "alice", State: "APPROVED", Body: "lgtm"}},
		}, nil
	}
	commitCalled := false
	fnCommit := func(_ context.Context, _, _ string) error {
		commitCalled = true
		return nil
	}

	o := newFeedbackOrchestrator(sc, ra, fnFindPR, fnFetchReviews, fnCommit, noPRComment)

	ws := &Workspace{
		TicketKey: "X-1",
		Repos:     []RepoWorkspace{{Name: "repo", Path: "/tmp/repo", Branch: "feature/X-1"}},
	}
	result, err := o.ProcessFeedback(context.Background(), Ticket{Key: "X-1"}, ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FixesMade != 0 {
		t.Errorf("FixesMade = %d, want 0 (no CHANGES_REQUESTED)", result.FixesMade)
	}
	if commitCalled {
		t.Error("commit should not be called when review is APPROVED")
	}
	if len(sc.postCommentCalls) != 0 {
		t.Error("no Jira comment should be posted when no fixes made")
	}
}

func TestProcessFeedback_ChangesRequested(t *testing.T) {
	sc := &stubJiraClient{}
	ra := &stubAgent{name: "fix", output: "addressed nil check in handler"}

	fnFindPR := func(_ context.Context, _, _ string) (*PRInfo, error) {
		return &PRInfo{URL: "https://github.com/org/repo/pull/5", Number: 5}, nil
	}
	fnFetchReviews := func(_ context.Context, _, _ string) (*PRReviewState, error) {
		return &PRReviewState{
			URL:            "https://github.com/org/repo/pull/5",
			ReviewDecision: "CHANGES_REQUESTED",
			Reviews: []Review{
				{Author: "alice", State: "CHANGES_REQUESTED", Body: "fix the nil check"},
			},
			Comments: []PRComment{
				{Author: "alice", Body: "see handler.go", CreatedAt: time.Now()},
			},
		}, nil
	}

	commitCalled := false
	fnCommit := func(_ context.Context, _, msg string) error {
		commitCalled = true
		if !strings.Contains(msg, "X-1") {
			return errors.New("commit message should contain ticket key")
		}
		return nil
	}

	prCommentPosted := false
	fnPostPR := func(_ context.Context, _, _, body string) error {
		prCommentPosted = true
		if !strings.Contains(body, "Nightshift") {
			return errors.New("PR comment missing Nightshift attribution")
		}
		return nil
	}

	o := newFeedbackOrchestrator(sc, ra, fnFindPR, fnFetchReviews, fnCommit, fnPostPR)

	ws := &Workspace{
		TicketKey: "X-1",
		Repos:     []RepoWorkspace{{Name: "repo", Path: "/tmp/repo", Branch: "feature/X-1"}},
	}
	result, err := o.ProcessFeedback(context.Background(), Ticket{Key: "X-1"}, ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FixesMade != 1 {
		t.Errorf("FixesMade = %d, want 1", result.FixesMade)
	}
	if result.PushedCommits != 1 {
		t.Errorf("PushedCommits = %d, want 1", result.PushedCommits)
	}
	if result.ReviewsFound == 0 {
		t.Error("ReviewsFound should be > 0")
	}
	if !commitCalled {
		t.Error("expected commit to be called")
	}
	if !prCommentPosted {
		t.Error("expected PR comment to be posted")
	}
	if len(sc.postCommentCalls) != 1 {
		t.Errorf("expected 1 Jira comment, got %d", len(sc.postCommentCalls))
	}
	if sc.postCommentCalls[0].Type != CommentRework {
		t.Errorf("Jira comment type = %q, want %q", sc.postCommentCalls[0].Type, CommentRework)
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}
}

func TestProcessFeedback_FetchReviewsError(t *testing.T) {
	sc := &stubJiraClient{}
	ra := &stubAgent{name: "fix"}

	fnFindPR := func(_ context.Context, _, _ string) (*PRInfo, error) {
		return &PRInfo{URL: "https://github.com/org/repo/pull/1", Number: 1}, nil
	}
	fnFetchReviews := func(_ context.Context, _, _ string) (*PRReviewState, error) {
		return nil, errors.New("gh: network error")
	}

	o := newFeedbackOrchestrator(sc, ra, fnFindPR, fnFetchReviews, noCommit, noPRComment)

	ws := &Workspace{
		TicketKey: "X-1",
		Repos:     []RepoWorkspace{{Name: "repo", Path: "/tmp/repo", Branch: "feature/X-1"}},
	}
	_, err := o.ProcessFeedback(context.Background(), Ticket{Key: "X-1"}, ws)
	if err == nil {
		t.Fatal("expected error from fnFetchReviews failure")
	}
	if !strings.Contains(err.Error(), "fetch reviews") {
		t.Errorf("error should mention 'fetch reviews', got: %v", err)
	}
}

func TestProcessFeedback_AgentError(t *testing.T) {
	sc := &stubJiraClient{}
	ra := &stubAgent{name: "fix", err: errors.New("LLM timeout")}

	fnFindPR := func(_ context.Context, _, _ string) (*PRInfo, error) {
		return &PRInfo{URL: "https://github.com/org/repo/pull/1", Number: 1}, nil
	}
	fnFetchReviews := func(_ context.Context, _, _ string) (*PRReviewState, error) {
		return &PRReviewState{
			ReviewDecision: "CHANGES_REQUESTED",
			Reviews:        []Review{{Author: "alice", State: "CHANGES_REQUESTED", Body: "fix it"}},
		}, nil
	}

	o := newFeedbackOrchestrator(sc, ra, fnFindPR, fnFetchReviews, noCommit, noPRComment)

	ws := &Workspace{
		TicketKey: "X-1",
		Repos:     []RepoWorkspace{{Name: "repo", Path: "/tmp/repo", Branch: "feature/X-1"}},
	}
	_, err := o.ProcessFeedback(context.Background(), Ticket{Key: "X-1"}, ws)
	if err == nil {
		t.Fatal("expected error from agent failure")
	}
	if !strings.Contains(err.Error(), "rework agent") {
		t.Errorf("error should mention 'rework agent', got: %v", err)
	}
}

func TestProcessFeedback_FindPRError(t *testing.T) {
	sc := &stubJiraClient{}
	ra := &stubAgent{name: "fix"}

	fnFindPR := func(_ context.Context, _, _ string) (*PRInfo, error) {
		return nil, errors.New("gh: auth failed")
	}

	o := newFeedbackOrchestrator(sc, ra, fnFindPR, nil, noCommit, noPRComment)

	ws := &Workspace{
		TicketKey: "X-1",
		Repos:     []RepoWorkspace{{Name: "repo", Path: "/tmp/repo", Branch: "feature/X-1"}},
	}
	_, err := o.ProcessFeedback(context.Background(), Ticket{Key: "X-1"}, ws)
	if err == nil {
		t.Fatal("expected error from fnFindPR failure")
	}
	if !strings.Contains(err.Error(), "find pr") {
		t.Errorf("error should mention 'find pr', got: %v", err)
	}
}
