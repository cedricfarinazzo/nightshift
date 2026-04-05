package jira

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// BranchName returns the feature branch name for a Jira ticket.
func BranchName(ticketKey string) string {
	return "feature/" + ticketKey
}

// CommitMessage formats a conventional commit message for a Jira ticket.
// If scope is empty the format is "feat: ticketKey: description".
func CommitMessage(ticketKey, scope, description string) string {
	if scope != "" {
		return fmt.Sprintf("feat(%s): %s: %s", scope, ticketKey, description)
	}
	return fmt.Sprintf("feat: %s: %s", ticketKey, description)
}

// PRTitle returns a pull request title for a Jira ticket (same format as CommitMessage).
func PRTitle(ticketKey, scope, description string) string {
	return CommitMessage(ticketKey, scope, description)
}

// HasChanges reports whether the repo at repoPath has uncommitted changes.
func HasChanges(ctx context.Context, repoPath string) (bool, error) {
	out, err := gitExec(ctx, repoPath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return len(out) > 0, nil
}

// CommitAndPush stages all changes, commits with message, and pushes to origin.
func CommitAndPush(ctx context.Context, repoPath, message string) error {
	if _, err := gitExec(ctx, repoPath, "add", "-A"); err != nil {
		return err
	}
	if _, err := gitExec(ctx, repoPath, "commit", "-m", message); err != nil {
		return err
	}
	branch, err := gitExec(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if _, err := gitExec(ctx, repoPath, "push", "origin", branch); err != nil {
		return err
	}
	return nil
}

// setupBranch checks out branchName in repoPath, creating it from baseBranch if needed.
// Returns isNew=true when the branch was freshly created.
func setupBranch(ctx context.Context, repoPath, branchName, baseBranch string) (isNew bool, err error) {
	if _, err = gitExec(ctx, repoPath, "checkout", branchName); err == nil {
		return false, nil
	}
	if _, err = gitExec(ctx, repoPath, "checkout", "-b", branchName, baseBranch); err != nil {
		return false, fmt.Errorf("git create branch %s: %w", branchName, err)
	}
	return true, nil
}

// gitExec runs a git command in repoPath and returns trimmed stdout.
func gitExec(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}
