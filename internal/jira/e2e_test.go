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

func statusNames(ss []Status) []string {
	names := make([]string, len(ss))
	for i, s := range ss {
		names[i] = s.Name
	}
	return names
}
