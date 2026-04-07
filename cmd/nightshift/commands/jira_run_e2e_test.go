//go:build e2e

package commands

import (
	"context"
	"os"
	"testing"

	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/jira"
)

// loadTestJiraClient loads config, applies defaults, and creates a Jira client.
// Skips the test if NIGHTSHIFT_JIRA_TOKEN is absent or config is invalid.
func loadTestJiraClient(t *testing.T) (*jira.Client, jira.JiraConfig) {
	t.Helper()
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("config load failed: %v", err)
	}
	cfg.Jira.Defaults()
	if err := cfg.Jira.Validate(); err != nil {
		t.Skipf("jira config invalid: %v", err)
	}
	client, err := jira.NewClient(cfg.Jira)
	if err != nil {
		t.Fatalf("jira.NewClient: %v", err)
	}
	return client, cfg.Jira
}

// TestJiraRun_E2E_ConfigAndConnect verifies that Ping succeeds with valid config.
// Read-only: no ticket mutations.
func TestJiraRun_E2E_ConfigAndConnect(t *testing.T) {
	client, _ := loadTestJiraClient(t)
	ctx := context.Background()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

// TestJiraRun_E2E_DiscoverStatuses verifies status discovery returns a non-empty map.
// Read-only: no ticket mutations.
func TestJiraRun_E2E_DiscoverStatuses(t *testing.T) {
	client, _ := loadTestJiraClient(t)
	ctx := context.Background()
	statusMap, err := client.DiscoverStatuses(ctx)
	if err != nil {
		t.Fatalf("DiscoverStatuses: %v", err)
	}
	if statusMap == nil {
		t.Fatal("statusMap is nil")
	}
	// At least one status category must be populated.
	total := len(statusMap.TodoStatuses) + len(statusMap.InProgressStatuses) +
		len(statusMap.ReviewStatuses) + len(statusMap.DoneStatuses)
	if total == 0 {
		t.Error("no statuses discovered — check project config")
	}
}

// TestJiraRun_E2E_FetchTickets verifies that ticket fetching works without mutations.
// Read-only: no ticket mutations.
func TestJiraRun_E2E_FetchTickets(t *testing.T) {
	client, _ := loadTestJiraClient(t)
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
