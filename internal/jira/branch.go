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

// setupBranch checks out branchName in repoPath, creating it from origin/baseBranch if needed.
// Returns isNew=true when the branch was freshly created.
func setupBranch(ctx context.Context, repoPath, branchName, baseBranch string) (isNew bool, err error) {
	if _, err = gitExec(ctx, repoPath, "checkout", branchName); err == nil {
		return false, nil
	}
	// Fetch so origin/<baseBranch> is available even on a fresh clone.
	if _, err = gitExec(ctx, repoPath, "fetch", "origin", baseBranch); err != nil {
		return false, fmt.Errorf("git fetch base branch %s: %w", baseBranch, err)
	}
	if _, err = gitExec(ctx, repoPath, "checkout", "-b", branchName, "origin/"+baseBranch); err != nil {
		return false, fmt.Errorf("git create branch %s from origin/%s: %w", branchName, baseBranch, err)
	}
	return true, nil
}

// gitExec runs a git command in repoPath and returns trimmed combined output.
func gitExec(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		subcommand := strings.Join(args, " ")
		if trimmed != "" {
			return "", fmt.Errorf("git %s failed: %s: %w", subcommand, trimmed, err)
		}
		return "", fmt.Errorf("git %s failed: %w", subcommand, err)
	}
	return trimmed, nil
}
