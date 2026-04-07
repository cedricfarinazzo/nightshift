//go:build e2e

package commands

import (
	"context"
	"os"
	"testing"

	"github.com/marcus/nightshift/internal/jira"
)

// e2eJiraClient returns a real Jira client configured for the sedinfra/VC project.
// Skips the test if NIGHTSHIFT_JIRA_TOKEN is not set.
func e2eJiraClient(t *testing.T) (*jira.Client, jira.JiraConfig) {
	t.Helper()
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set")
	}
	cfg := jira.JiraConfig{
		Site:     "sedinfra",
		Email:    "cedric.farinazzo@gmail.com",
		TokenEnv: "NIGHTSHIFT_JIRA_TOKEN",
		Project:  "VC",
		Label:    "nightshift",
	}
	cfg.Defaults()
	client, err := jira.NewClient(cfg)
	if err != nil {
		t.Fatalf("jira.NewClient: %v", err)
	}
	return client, cfg
}

// TestJiraRun_E2E_ConfigAndConnect verifies that Ping succeeds with valid config.
// Read-only: no ticket mutations.
func TestJiraRun_E2E_ConfigAndConnect(t *testing.T) {
	client, _ := e2eJiraClient(t)
	ctx := context.Background()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

// TestJiraRun_E2E_DiscoverStatuses verifies status discovery returns a non-empty map.
// Read-only: no ticket mutations.
func TestJiraRun_E2E_DiscoverStatuses(t *testing.T) {
	client, _ := e2eJiraClient(t)
	ctx := context.Background()
	statusMap, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("DiscoverStatuses: %v", err)
	}
	if statusMap == nil {
		t.Fatal("statusMap is nil")
	}
	total := len(statusMap.TodoStatuses) + len(statusMap.InProgressStatuses) +
		len(statusMap.ReviewStatuses) + len(statusMap.DoneStatuses)
	if total == 0 {
		t.Error("no statuses discovered — check project config")
	}
	t.Logf("statuses: todo=%d inprogress=%d review=%d done=%d",
		len(statusMap.TodoStatuses), len(statusMap.InProgressStatuses),
		len(statusMap.ReviewStatuses), len(statusMap.DoneStatuses))
}

// TestJiraRun_E2E_FetchTickets verifies that ticket fetching works without mutations.
// Read-only: no ticket mutations.
func TestJiraRun_E2E_FetchTickets(t *testing.T) {
	client, _ := e2eJiraClient(t)
	ctx := context.Background()

	statusMap, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("DiscoverStatuses: %v", err)
	}

	todoTickets, err := client.FetchTodoTickets(ctx)
	if err != nil {
		t.Fatalf("FetchTodoTickets: %v", err)
	}
	t.Logf("todo tickets: %d", len(todoTickets))

	reviewTickets, err := client.FetchReviewTickets(ctx, statusMap)
	if err != nil {
		t.Fatalf("FetchReviewTickets: %v", err)
	}
	t.Logf("review tickets: %d", len(reviewTickets))
}
