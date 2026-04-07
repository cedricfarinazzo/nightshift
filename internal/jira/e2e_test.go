package jira

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/agents"
)

// e2eClient returns a real Jira client configured for the sedinfra/VC project.
// Skips the test if NIGHTSHIFT_JIRA_TOKEN is not set.
func e2eClient(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e test")
	}
	cfg := JiraConfig{
		Site:     "sedinfra",
		Email:    "cedric.farinazzo@gmail.com",
		TokenEnv: "NIGHTSHIFT_JIRA_TOKEN",
		Project:  "VC",
		Label:    "nightshift",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestE2E_Ping(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestE2E_DiscoverStatuses(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sm, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("DiscoverStatuses: %v", err)
	}
	t.Logf("Todo statuses: %v", statusNames(sm.TodoStatuses))
	t.Logf("InProgress statuses: %v", statusNames(sm.InProgressStatuses))
	t.Logf("Review statuses: %v", statusNames(sm.ReviewStatuses))
	t.Logf("Done statuses: %v", statusNames(sm.DoneStatuses))

	if len(sm.TodoStatuses)+len(sm.InProgressStatuses)+len(sm.DoneStatuses) == 0 {
		t.Error("expected at least one status discovered")
	}
}

func TestE2E_FetchTodoTickets(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tickets, err := client.FetchTodoTickets(ctx)
	if err != nil {
		t.Fatalf("FetchTodoTickets: %v", err)
	}
	t.Logf("FetchTodoTickets returned %d tickets", len(tickets))
	for _, tk := range tickets {
		t.Logf("  %s: %s (status=%s labels=%v)", tk.Key, tk.Summary, tk.Status.Name, tk.Labels)
		if tk.Key == "" {
			t.Error("ticket has empty Key")
		}
		if tk.Status.CategoryKey != "new" {
			t.Errorf("ticket %s: expected statusCategory 'new', got %q", tk.Key, tk.Status.CategoryKey)
		}
	}
}

func TestE2E_FetchReviewTickets(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sm, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("DiscoverStatuses: %v", err)
	}
	tickets, err := client.FetchReviewTickets(ctx, sm)
	if err != nil {
		t.Fatalf("FetchReviewTickets: %v", err)
	}
	t.Logf("FetchReviewTickets returned %d tickets", len(tickets))
	for _, tk := range tickets {
		t.Logf("  %s: %s (status=%s)", tk.Key, tk.Summary, tk.Status.Name)
	}
}

func TestE2E_FetchReviewTickets_NilStatusMap(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// nil statusMap must not panic and must return (nil, nil)
	tickets, err := client.FetchReviewTickets(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error with nil statusMap: %v", err)
	}
	if tickets != nil {
		t.Errorf("expected nil tickets for nil statusMap, got %d", len(tickets))
	}
}

func TestE2E_DependencyGraph(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tickets, err := client.FetchTodoTickets(ctx)
	if err != nil {
		t.Fatalf("FetchTodoTickets: %v", err)
	}
	if len(tickets) == 0 {
		t.Skip("no todo tickets found; skipping dependency graph test")
	}

	g := BuildDependencyGraph(tickets)
	cycles := g.DetectCycles()
	t.Logf("DetectCycles: %d cycle(s)", len(cycles))
	for i, c := range cycles {
		t.Logf("  cycle %d: %v", i, c)
	}

	ready, blocked := g.ResolveOrder()
	t.Logf("ResolveOrder: %d ready, %d blocked", len(ready), len(blocked))
	for _, r := range ready {
		t.Logf("  ready: %s %s", r.Key, r.Summary)
	}
	for _, b := range blocked {
		t.Logf("  blocked: %s — %s (blockers: %v)", b.Ticket.Key, b.Reason, b.Blockers)
	}

	if len(ready)+len(blocked) != len(tickets) {
		t.Errorf("ready+blocked=%d != total tickets=%d", len(ready)+len(blocked), len(tickets))
	}
}

// ── VC-3: Client wrapper ────────────────────────────────────────────────────

func TestE2E_VC3_ClientAccessors(t *testing.T) {
	client := e2eClient(t)
	if got := client.ProjectKey(); got != "VC" {
		t.Errorf("ProjectKey() = %q, want %q", got, "VC")
	}
	if got := client.Label(); got != "nightshift" {
		t.Errorf("Label() = %q, want %q", got, "nightshift")
	}
	if client.Raw() == nil {
		t.Error("Raw() returned nil; expected underlying go-atlassian client")
	}
}

func TestE2E_VC3_NewClient_BadCredentials(t *testing.T) {
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e test")
	}
	// Use a wrong token — NewClient should succeed (it only validates config),
	// but Ping must fail with an auth error.
	t.Setenv("BAD_TOKEN_ENV", "not-a-real-token")
	cfg := JiraConfig{
		Site:     "sedinfra",
		Email:    "cedric.farinazzo@gmail.com",
		TokenEnv: "BAD_TOKEN_ENV",
		Project:  "VC",
		Label:    "nightshift",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient should succeed with any non-empty token; got: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err == nil {
		t.Error("Ping with bad token should fail, but it succeeded")
	}
}

// ── VC-4: Status auto-discovery & transition helpers ─────────────────────────

func TestE2E_VC4_DiscoverStatuses_Cached(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sm1, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("first DiscoverStatuses: %v", err)
	}
	sm2, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("second DiscoverStatuses: %v", err)
	}
	if sm1 != sm2 {
		t.Error("DiscoverStatuses should return the same cached pointer on repeated calls")
	}
}

func TestE2E_VC4_DiscoverStatuses_HasAllCategories(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sm, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("DiscoverStatuses: %v", err)
	}

	if len(sm.TodoStatuses) == 0 {
		t.Error("expected at least one Todo status")
	}
	if len(sm.InProgressStatuses) == 0 {
		t.Error("expected at least one InProgress status")
	}
	if len(sm.ReviewStatuses) == 0 {
		t.Error("expected at least one Review status (project uses 'Revue en cours')")
	}
	if len(sm.DoneStatuses) == 0 {
		t.Error("expected at least one Done status")
	}

	// All category keys must be consistent.
	for _, s := range sm.TodoStatuses {
		if s.CategoryKey != "new" {
			t.Errorf("TodoStatus %q has CategoryKey %q, want 'new'", s.Name, s.CategoryKey)
		}
	}
	for _, s := range append(sm.InProgressStatuses, sm.ReviewStatuses...) {
		if s.CategoryKey != "indeterminate" {
			t.Errorf("InProgress/Review status %q has CategoryKey %q, want 'indeterminate'", s.Name, s.CategoryKey)
		}
	}
	for _, s := range sm.DoneStatuses {
		if s.CategoryKey != "done" {
			t.Errorf("DoneStatus %q has CategoryKey %q, want 'done'", s.Name, s.CategoryKey)
		}
	}
}

func TestE2E_VC4_DiscoverStatuses_ReviewIsSubsetOfIndeterminate(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sm, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("DiscoverStatuses: %v", err)
	}

	// ReviewStatuses must all pass isReviewStatus heuristic.
	for _, s := range sm.ReviewStatuses {
		if !isReviewStatus(s.Name) {
			t.Errorf("ReviewStatus %q does not match review heuristic", s.Name)
		}
	}
	// InProgressStatuses must NOT pass isReviewStatus.
	for _, s := range sm.InProgressStatuses {
		if isReviewStatus(s.Name) {
			t.Errorf("InProgressStatus %q erroneously matches review heuristic", s.Name)
		}
	}
}

func TestE2E_VC4_FindTransition(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// VC-5 is currently in review; it should have transitions available.
	const issueKey = "VC-5"

	for _, category := range []string{"new", "indeterminate", "done"} {
		tid, err := client.FindTransition(ctx, issueKey, category)
		if err != nil {
			t.Logf("FindTransition(%s, %q): no transition available (%v)", issueKey, category, err)
			continue
		}
		if tid == "" {
			t.Errorf("FindTransition(%s, %q) returned empty transition ID", issueKey, category)
		}
		t.Logf("FindTransition(%s, %q) = %q", issueKey, category, tid)
	}
}

func TestE2E_VC4_FindTransition_InvalidCategory(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := client.FindTransition(ctx, "VC-5", "nonexistent-category")
	if err == nil {
		t.Error("expected error for nonexistent category, got nil")
	}
}

// ── VC-6: LLM-based ticket validation ────────────────────────────────────────

func TestE2E_VC6_AddComment(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Post a test comment on VC-6 itself.
	err := client.AddComment(ctx, "VC-6", "🤖 Nightshift e2e test: AddComment — automated test comment, safe to ignore.")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
}

func TestE2E_VC6_ValidateTicket_WithStubAgent(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch a real ticket from Jira to validate the full pipeline.
	tickets, err := client.FetchTodoTickets(ctx)
	if err != nil {
		t.Fatalf("FetchTodoTickets: %v", err)
	}
	if len(tickets) == 0 {
		t.Skip("no todo tickets available; skipping ValidateTicket e2e test")
	}
	ticket := tickets[0]

	// Use a stub agent so we don't require an LLM API key for e2e.
	agent := &stubAgent{
		name:   "stub-e2e",
		output: `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": []}`,
	}

	result, err := ValidateTicket(ctx, agent, ticket)
	if err != nil {
		t.Fatalf("ValidateTicket: %v", err)
	}
	if result.Score != 8 {
		t.Errorf("Score = %d, want 8", result.Score)
	}
	t.Logf("ValidateTicket(%s): valid=%v score=%d issues=%v", ticket.Key, result.Valid, result.Score, result.Issues)
}

func TestE2E_VC6_ValidateTicket_RejectedFlow(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tickets, err := client.FetchTodoTickets(ctx)
	if err != nil {
		t.Fatalf("FetchTodoTickets: %v", err)
	}
	if len(tickets) == 0 {
		t.Skip("no todo tickets available; skipping rejected flow e2e test")
	}
	ticket := tickets[0]

	// Stub agent returns a low-quality response.
	agent := &stubAgent{
		name:   "stub-e2e",
		output: `{"valid": false, "score": 3, "issues": ["synthetic test issue"], "missing": ["synthetic missing field"], "suggestions": ["synthetic suggestion"]}`,
	}

	result, err := ValidateTicket(ctx, agent, ticket)
	if err != nil {
		t.Fatalf("ValidateTicket: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false for stubbed low-score response")
	}
	if result.Score != 3 {
		t.Errorf("Score = %d, want 3", result.Score)
	}
	t.Logf("ValidateTicket(%s) correctly flagged as invalid: score=%d issues=%v", ticket.Key, result.Score, result.Issues)
}

// ── VC-7: Per-ticket workspace & branch management ───────────────────────────

func TestE2E_VC7_BranchName(t *testing.T) {
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e test")
	}
	got := BranchName("VC-7")
	if got != "feature/VC-7" {
		t.Errorf("BranchName(VC-7) = %q, want %q", got, "feature/VC-7")
	}
}

func TestE2E_VC7_CommitMessage(t *testing.T) {
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e test")
	}
	tests := []struct {
		scope string
		want  string
	}{
		{"api", "feat(api): VC-7: add workspace"},
		{"", "feat: VC-7: add workspace"},
	}
	for _, tt := range tests {
		got := CommitMessage("VC-7", tt.scope, "add workspace")
		if got != tt.want {
			t.Errorf("CommitMessage(VC-7, %q, ...) = %q, want %q", tt.scope, got, tt.want)
		}
	}
}

func TestE2E_VC7_SetupWorkspace_InvalidKey(t *testing.T) {
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e test")
	}
	cfg := JiraConfig{
		WorkspaceRoot:    t.TempDir(),
		CleanupAfterDays: 30,
		Repos:            []RepoConfig{{Name: "repo", URL: "git@github.com:org/repo.git", BaseBranch: "main"}},
	}
	_, err := SetupWorkspace(context.Background(), cfg, "invalid-key")
	if err == nil {
		t.Error("SetupWorkspace with invalid key should return error")
	}
}

func TestE2E_VC7_CleanupStaleWorkspaces_Empty(t *testing.T) {
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e test")
	}
	cfg := JiraConfig{WorkspaceRoot: t.TempDir(), CleanupAfterDays: 30}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("CleanupStaleWorkspaces: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 removed, got %d", n)
	}
}

// ── VC-11: Jira comment state management ────────────────────────────────────

func TestE2E_VC11_PostComment(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	comment := NightshiftComment{
		Type:      CommentValidation,
		Timestamp: time.Now().UTC(),
		Provider:  "claude",
		Model:     "claude-haiku-4-5",
		Duration:  5 * time.Second,
		Body:      "✅ e2e test: PostComment — automated test, safe to ignore.",
		Metadata:  map[string]string{"score": "9", "valid": "true"},
	}
	if err := client.PostComment(ctx, "VC-11", comment); err != nil {
		t.Fatalf("PostComment: %v", err)
	}
}

// ── VC-9: GitHub PR lifecycle management ────────────────────────────────────

// e2eGHAvailable skips the test if NIGHTSHIFT_JIRA_TOKEN is unset or the gh CLI is not
// authenticated. Returns true when both conditions are satisfied.
func e2eGHAvailable(t *testing.T) bool {
	t.Helper()
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e test")
	}
	// Verify gh CLI is present and authenticated.
	cmd := exec.CommandContext(context.Background(), "gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		t.Skip("gh CLI not available or not authenticated (gh auth status failed)")
	}
	return true
}

// e2eTestPRURL returns the PR URL to use for VC-9 e2e tests.
// Override via NIGHTSHIFT_TEST_PR_URL to avoid coupling tests to a specific PR number.
func e2eTestPRURL() string {
	if u := os.Getenv("NIGHTSHIFT_TEST_PR_URL"); u != "" {
		return u
	}
	return "https://github.com/cedricfarinazzo/nightshift/pull/40"
}

func TestE2E_VC9_FetchPRReviewComments(t *testing.T) {
	e2eGHAvailable(t)

	prURL := e2eTestPRURL()
	repoPath := "."

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rs, err := FetchPRReviewComments(ctx, repoPath, prURL)
	if err != nil {
		t.Logf("FetchPRReviewComments: %v (gh CLI may not be authenticated)", err)
		t.Skip("gh CLI not available or not authenticated")
	}
	if rs.URL == "" {
		t.Error("FetchPRReviewComments returned empty URL")
	}
	if rs.State == "" {
		t.Error("FetchPRReviewComments returned empty State")
	}
	t.Logf("PR %s: state=%s reviewDecision=%q reviews=%d comments=%d",
		prURL, rs.State, rs.ReviewDecision, len(rs.Reviews), len(rs.Comments))
}

func TestE2E_VC9_FetchPRReviewComments_InvalidURL(t *testing.T) {
	e2eGHAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := FetchPRReviewComments(ctx, ".", "https://github.com/cedricfarinazzo/nightshift/pull/999999")
	if err == nil {
		t.Error("expected error for non-existent PR, got nil")
	}
	t.Logf("expected error: %v", err)
}

func TestE2E_VC9_PRTitle_Format(t *testing.T) {
	e2eGHAvailable(t)

	// Verify that prTitle produces the expected bracket format used in PR creation.
	ticket := Ticket{Key: "VC-9", Summary: "GitHub PR Lifecycle Management"}
	got := prTitle(ticket)
	want := "[VC-9] GitHub PR Lifecycle Management"
	if got != want {
		t.Errorf("prTitle = %q, want %q", got, want)
	}
}

// ── VC-8: Jira orchestrator ──────────────────────────────────────────────────

// noMutationJiraClient wraps a real Client for read operations but stubs out all
// state-mutating calls (transitions, comments) so e2e tests do not alter real tickets.
type noMutationJiraClient struct {
	real *Client
}

func (n *noMutationJiraClient) PostComment(_ context.Context, _ string, _ NightshiftComment) error {
	return nil
}
func (n *noMutationJiraClient) HandleInvalidTicket(_ context.Context, _ string, _ *ValidationResult) error {
	return nil
}
func (n *noMutationJiraClient) TransitionToInProgress(_ context.Context, _ string) error { return nil }
func (n *noMutationJiraClient) TransitionToReview(_ context.Context, _ string) error    { return nil }

func TestE2E_VC8_ProcessTicket_WithStubAgents(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tickets, err := client.FetchTodoTickets(ctx)
	if err != nil {
		t.Fatalf("FetchTodoTickets: %v", err)
	}
	if len(tickets) == 0 {
		t.Skip("no todo tickets available; skipping orchestrator e2e test")
	}
	ticket := tickets[0]
	t.Logf("processing ticket %s: %s", ticket.Key, ticket.Summary)

	// Both agents return canned responses — no LLM key required.
	va := &stubAgent{
		name:   "e2e-validator",
		output: `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": []}`,
	}
	ia := &stubAgent{
		name:   "e2e-impl",
		output: "e2e stub plan/impl output",
	}

	// Use noMutationJiraClient so no transitions or comments are posted to real Jira.
	o := &Orchestrator{
		client:          &noMutationJiraClient{real: client},
		cfg:             client.cfg,
		validationAgent: va,
		implAgent:       ia,
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
	}

	// Empty workspace — skips commit/PR phases entirely.
	ws := &Workspace{TicketKey: ticket.Key}

	result, err := o.ProcessTicket(ctx, ticket, ws)
	if err != nil {
		t.Fatalf("ProcessTicket: %v", err)
	}
	t.Logf("result: status=%s phase=%s duration=%s error=%q", result.Status, result.Phase, result.Duration, result.Error)

	if result.Status != TicketCompleted {
		t.Errorf("Status = %q, want %q; error: %s", result.Status, TicketCompleted, result.Error)
	}
	if result.Plan == "" {
		t.Error("Plan should not be empty")
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}
}

func TestE2E_VC8_NewOrchestrator_Defaults(t *testing.T) {
	client := e2eClient(t)

	va := &stubAgent{name: "va"}
	ia := &stubAgent{name: "ia"}

	o := NewOrchestrator(client, client.cfg,
		WithValidationAgent(va),
		WithImplAgent(ia),
	)
	if o == nil {
		t.Fatal("NewOrchestrator returned nil")
	} else {
		if o.client == nil {
			t.Error("client should not be nil")
		}
		if o.validationAgent == nil {
			t.Error("validationAgent should not be nil")
		}
		if o.implAgent == nil {
			t.Error("implAgent should not be nil")
		}
	}
}

func statusNames(ss []Status) []string {
	names := make([]string, len(ss))
	for i, s := range ss {
		names[i] = s.Name
	}
	return names
}

// ── VC-10: PR Feedback Loop & Review Re-work ─────────────────────────────────

func TestE2E_VC10_ProcessFeedback_NoWorkspace(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ra := &stubAgent{name: "e2e-fix", output: "e2e stub rework output"}
	// Use noMutationJiraClient so no comments or transitions are posted to real Jira.
	o := &Orchestrator{
		client:          &noMutationJiraClient{real: client},
		cfg:             client.cfg,
		reviewFixAgent:  ra,
		validationAgent: ra,
		implAgent:       ra,
	}

	// Empty workspace — no repos to process. ProcessFeedback should return cleanly.
	ws := &Workspace{TicketKey: "VC-10"}
	result, err := o.ProcessFeedback(ctx, Ticket{Key: "VC-10", Summary: "feedback loop"}, ws)
	if err != nil {
		t.Fatalf("ProcessFeedback: %v", err)
	}
	if result.FixesMade != 0 {
		t.Errorf("FixesMade = %d, want 0 for empty workspace", result.FixesMade)
	}
	if result.PushedCommits != 0 {
		t.Errorf("PushedCommits = %d, want 0 for empty workspace", result.PushedCommits)
	}
	t.Logf("ProcessFeedback(VC-10, empty ws): reviewsFound=%d fixesMade=%d duration=%s",
		result.ReviewsFound, result.FixesMade, result.Duration)
}

func TestE2E_VC10_WithReviewFixAgent(t *testing.T) {
	client := e2eClient(t)

	ra := &stubAgent{name: "e2e-review-fix"}
	ia := &stubAgent{name: "e2e-impl"}
	// Construct directly so we can use noMutationJiraClient (NewOrchestrator requires *Client).
	o := &Orchestrator{
		client:          &noMutationJiraClient{real: client},
		cfg:             client.cfg,
		validationAgent: ia,
		implAgent:       ia,
		reviewFixAgent:  ra,
	}
	if o.reviewFixAgent == nil {
		t.Error("reviewFixAgent should not be nil after WithReviewFixAgent")
	}
	// Verify the correct agent was stored (interface comparison).
	var want agents.Agent = ra
	if o.reviewFixAgent != want {
		t.Error("reviewFixAgent is not the agent passed to WithReviewFixAgent")
	}
}

func TestE2E_VC10_FilterNewComments_LivePR(t *testing.T) {
	e2eGHAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rs, err := FetchPRReviewComments(ctx, ".", e2eTestPRURL())
	if err != nil {
		t.Skipf("FetchPRReviewComments: %v (gh CLI may not be authenticated)", err)
	}

	// Zero lastSeen: all comments returned.
	all := filterNewComments(rs.Comments, time.Time{})
	if len(all) != len(rs.Comments) {
		t.Errorf("zero lastSeen: got %d comments, want %d", len(all), len(rs.Comments))
	}

	// Future lastSeen: no comments returned.
	none := filterNewComments(rs.Comments, time.Now().Add(24*time.Hour))
	if len(none) != 0 {
		t.Errorf("future lastSeen: got %d comments, want 0", len(none))
	}

	t.Logf("PR %s: %d total comments, %d after zero cutoff, %d after future cutoff",
		e2eTestPRURL(), len(rs.Comments), len(all), len(none))
}

func TestE2E_VC10_BuildReworkPrompt_RealTicket(t *testing.T) {
	client := e2eClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sm, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("DiscoverStatuses: %v", err)
	}

	tickets, err := client.FetchReviewTickets(ctx, sm)
	if err != nil {
		t.Fatalf("FetchReviewTickets: %v", err)
	}
	if len(tickets) == 0 {
		t.Skip("no review tickets available; skipping buildReworkPrompt e2e test")
	}
	ticket := tickets[0]

	fakeReview := &PRReviewState{
		URL:            "https://github.com/cedricfarinazzo/nightshift/pull/99",
		ReviewDecision: "CHANGES_REQUESTED",
		Reviews: []Review{
			{Author: "reviewer", State: "CHANGES_REQUESTED", Body: "please add tests"},
		},
		Comments: []PRComment{
			{Path: "main.go", Line: 10, Author: "reviewer", Body: "nil check missing"},
		},
	}
	repo := RepoWorkspace{Name: "nightshift", Branch: BranchName(ticket.Key)}

	prompt := buildReworkPrompt(ticket, fakeReview, repo)

	if !strings.Contains(prompt, ticket.Key) {
		t.Errorf("prompt missing ticket key %q", ticket.Key)
	}
	if !strings.Contains(prompt, "please add tests") {
		t.Error("prompt missing reviewer comment body")
	}
	if !strings.Contains(prompt, "main.go:10") {
		t.Error("prompt missing inline comment path:line")
	}
	t.Logf("buildReworkPrompt(%s): prompt length=%d", ticket.Key, len(prompt))
}
