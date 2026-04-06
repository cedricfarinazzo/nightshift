package jira

import (
	"context"
	"os"
	"testing"
	"time"
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

func statusNames(ss []Status) []string {
	names := make([]string, len(ss))
	for i, s := range ss {
		names[i] = s.Name
	}
	return names
}
