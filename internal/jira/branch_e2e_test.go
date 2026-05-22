package jira

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/security"
)

// TestE2E_SetupBranch_FirstPush verifies that setupBranch returns nil when
// a local branch exists but has never been pushed (remote ref missing).
// Gated on NIGHTSHIFT_JIRA_TOKEN for consistency with other e2e tests.
func TestE2E_SetupBranch_FirstPush(t *testing.T) {
	if security.GetJiraToken() == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set up a local git repo with a remote so fetch succeeds.
	workDir := setupRemoteRepoWithMain(t)

	// Use a timestamp-based suffix so the name is deterministic and git-safe
	// regardless of the USER env var (which may be empty or contain spaces).
	branchName := fmt.Sprintf("feature/e2e-first-push-%d", time.Now().UnixNano())
	runGit(t, workDir, "checkout", "-b", branchName)

	// setupBranch: branch exists locally (Case 1), pull --rebase will get
	// "couldn't find remote ref" → should return isNew=false, err=nil.
	isNew, err := setupBranch(ctx, workDir, branchName, "main")
	if err != nil {
		t.Fatalf("setupBranch first-push case returned error: %v", err)
	}
	if isNew {
		t.Fatal("setupBranch first-push case: expected isNew=false (branch existed locally)")
	}
}
