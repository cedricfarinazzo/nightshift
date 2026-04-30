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

// setupBranch checks out branchName in repoPath, syncing with origin.
// Priority order:
//  1. Local branch exists → checkout + pull --rebase to sync with remote.
//  2. Remote branch exists (origin/branchName) → create local tracking branch.
//  3. Neither → create fresh from origin/baseBranch (isNew=true).
func setupBranch(ctx context.Context, repoPath, branchName, baseBranch string) (isNew bool, err error) {
	// Always fetch so remote refs are current.
	if _, err = gitExec(ctx, repoPath, "fetch", "origin"); err != nil {
		return false, fmt.Errorf("git fetch origin: %w", err)
	}

	// Case 1: local branch already exists — checkout and sync.
	if _, err = gitExec(ctx, repoPath, "checkout", branchName); err == nil {
		// Pull remote changes so we don't push-reject on the next commit.
		if _, pullErr := gitExec(ctx, repoPath, "pull", "--rebase", "origin", branchName); pullErr != nil {
			// No-op if remote branch doesn't exist yet (first run, not yet pushed).
			_ = pullErr
		}
		return false, nil
	}

	// Case 2: remote branch exists — track it instead of branching from base.
	if _, err = gitExec(ctx, repoPath, "rev-parse", "--verify", "origin/"+branchName); err == nil {
		if _, err = gitExec(ctx, repoPath, "checkout", "-b", branchName, "--track", "origin/"+branchName); err != nil {
			return false, fmt.Errorf("git checkout tracking branch %s: %w", branchName, err)
		}
		return false, nil
	}

	// Case 3: branch is brand new — create from base branch.
	if _, err = gitExec(ctx, repoPath, "checkout", "-b", branchName, "origin/"+baseBranch); err != nil {
		return false, fmt.Errorf("git create branch %s from origin/%s: %w", branchName, baseBranch, err)
	}
	return true, nil
}

// BranchAheadOfBase reports whether branch has commits ahead of origin/base on the remote.
// Returns (false, nil) when the remote ref for branch does not exist (branch not yet pushed)
// or when there are no commits ahead. Only returns an error for unexpected git failures.
func BranchAheadOfBase(ctx context.Context, repoPath, branch, base string) (bool, error) {
	out, err := gitExec(ctx, repoPath, "log", "origin/"+base+"..origin/"+branch, "--oneline")
	if err != nil {
		msg := err.Error()
		// Treat a missing remote ref as "not ahead" rather than an error.
		if strings.Contains(msg, "unknown revision") || strings.Contains(msg, "bad revision") ||
			strings.Contains(msg, "ambiguous argument") {
			return false, nil
		}
		return false, err
	}
	return len(out) > 0, nil
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
