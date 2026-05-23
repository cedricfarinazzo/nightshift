package jira

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cedricfarinazzo/nightshift/internal/logging"
)

// gitExecFn executes a git command and returns trimmed combined output. Replaced in tests.
var gitExecFn = func(ctx context.Context, repoPath string, args ...string) (string, error) {
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

// gitExecRawFn executes a git command and returns combined output + exit code even on failure.
// Replaced in tests.
var gitExecRawFn = func(ctx context.Context, repoPath string, args ...string) (out string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	combined, runErr := cmd.CombinedOutput()
	out = strings.TrimSpace(string(combined))
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return out, exitErr.ExitCode(), runErr
		}
		return out, -1, runErr
	}
	return out, 0, nil
}

// pullErrClass classifies git pull --rebase failure modes.
type pullErrClass int

const (
	pullErrNewBranch pullErrClass = iota // remote ref does not exist yet (first push)
	pullErrConflict                      // rebase conflict
	pullErrOther                         // network/auth/unknown
)

// classifyPullError inspects combined git output to determine which failure class applies.
func classifyPullError(out string) pullErrClass {
	lower := strings.ToLower(out)
	if strings.Contains(lower, "couldn't find remote ref") {
		return pullErrNewBranch
	}
	if strings.Contains(out, "CONFLICT") || strings.Contains(lower, "could not apply") || strings.Contains(lower, "merge conflict") {
		return pullErrConflict
	}
	return pullErrOther
}

// parseConflictPaths extracts conflicting file paths from git rebase output.
// Matches lines of the form "CONFLICT (...): ... in <path>".
func parseConflictPaths(out string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "CONFLICT") {
			continue
		}
		idx := strings.LastIndex(line, " in ")
		if idx < 0 {
			continue
		}
		path := strings.TrimSpace(line[idx+4:])
		if path != "" && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

// tailLines returns the last n lines of out joined by newline.
func tailLines(out string, n int) string {
	lines := strings.Split(out, "\n")
	if len(lines) <= n {
		return out
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// BranchName returns the feature branch name for a Jira ticket.
func BranchName(ticketKey string) string {
	return "feature/" + ticketKey
}

// issueTypeToCC maps a Jira issue type to a Conventional Commits prefix.
// Matching is case-insensitive; unknown types default to "feat".
func issueTypeToCC(issueType string) string {
	switch strings.ToLower(issueType) {
	case "bug":
		return "fix"
	case "chore", "maintenance":
		return "chore"
	default:
		return "feat"
	}
}

// CommitMessage formats a conventional commit message for a Jira ticket.
// issueType is mapped to a Conventional Commits prefix (e.g. "Bug" → "fix:").
// If scope is empty the format is "<prefix>: ticketKey: description".
func CommitMessage(ticketKey, issueType, scope, description string) string {
	prefix := issueTypeToCC(issueType)
	if scope != "" {
		return fmt.Sprintf("%s(%s): %s: %s", prefix, scope, ticketKey, description)
	}
	return fmt.Sprintf("%s: %s: %s", prefix, ticketKey, description)
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
	// Sync with remote before pushing to avoid non-fast-forward rejection.
	// Skip only when the remote branch genuinely does not exist yet (first push).
	exists, err := remoteRefExists(ctx, repoPath, "refs/remotes/origin/"+branch)
	if err != nil {
		return fmt.Errorf("check remote ref before pull --rebase: %w", err)
	}
	if exists {
		if _, pullErr := gitExec(ctx, repoPath, "pull", "--rebase", "origin", branch); pullErr != nil {
			return fmt.Errorf("pull --rebase before push: %w", pullErr)
		}
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
		pullOut, exitCode, pullErr := gitExecRawFn(ctx, repoPath, "pull", "--rebase", "origin", branchName)
		if pullErr != nil {
			switch classifyPullError(pullOut) {
			case pullErrNewBranch:
				logging.Component("jira").Debugf("setupBranch %s: remote branch missing, skipping pull", branchName)
			case pullErrConflict:
				paths := parseConflictPaths(pullOut)
				if _, abortErr := gitExec(ctx, repoPath, "rebase", "--abort"); abortErr != nil {
					logging.Component("jira").Debugf("setupBranch %s: rebase --abort: %v", branchName, abortErr)
				}
				return false, fmt.Errorf("git pull --rebase %s: conflicts in %v: %w", branchName, paths, pullErr)
			default:
				return false, fmt.Errorf("git pull --rebase %s (exit %d): %s: %w", branchName, exitCode, tailLines(pullOut, 5), pullErr)
			}
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

// PushBranch pushes the current local branch to origin without committing.
func PushBranch(ctx context.Context, repoPath string) error {
	branch, err := gitExec(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if _, err := gitExec(ctx, repoPath, "push", "origin", branch); err != nil {
		return err
	}
	return nil
}

// LocalBranchAheadOfBase reports whether the local HEAD has commits ahead of origin/base.
// This detects the case where an agent committed locally but never pushed.
// Returns (false, nil) when origin/base does not exist.
func LocalBranchAheadOfBase(ctx context.Context, repoPath, base string) (bool, error) {
	baseRef := "refs/remotes/origin/" + base
	baseExists, err := remoteRefExists(ctx, repoPath, baseRef)
	if err != nil {
		return false, err
	}
	if !baseExists {
		return false, nil
	}
	out, err := gitExec(ctx, repoPath, "rev-list", "--count", baseRef+"..HEAD")
	if err != nil {
		return false, err
	}
	count, err := strconv.Atoi(out)
	if err != nil {
		return false, fmt.Errorf("parse git rev-list count %q: %w", out, err)
	}
	return count > 0, nil
}

// BranchAheadOfBase reports whether branch has commits ahead of origin/base on the remote.
// Returns (false, nil) when the remote ref for branch does not exist (branch not yet pushed)
// or when there are no commits ahead. Returns an error for missing base refs or unexpected
// git failures.
func BranchAheadOfBase(ctx context.Context, repoPath, branch, base string) (bool, error) {
	branchRef := "refs/remotes/origin/" + branch
	baseRef := "refs/remotes/origin/" + base

	branchExists, err := remoteRefExists(ctx, repoPath, branchRef)
	if err != nil {
		return false, err
	}
	if !branchExists {
		return false, nil
	}

	baseExists, err := remoteRefExists(ctx, repoPath, baseRef)
	if err != nil {
		return false, err
	}
	if !baseExists {
		return false, fmt.Errorf("git remote ref %s not found", baseRef)
	}

	out, err := gitExec(ctx, repoPath, "rev-list", "--count", baseRef+".."+branchRef)
	if err != nil {
		return false, err
	}
	count, err := strconv.Atoi(out)
	if err != nil {
		return false, fmt.Errorf("parse git rev-list count %q: %w", out, err)
	}
	return count > 0, nil
}

func remoteRefExists(ctx context.Context, repoPath, ref string) (bool, error) {
	_, err := gitExec(ctx, repoPath, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// gitExec runs a git command in repoPath and returns trimmed combined output.
func gitExec(ctx context.Context, repoPath string, args ...string) (string, error) {
	return gitExecFn(ctx, repoPath, args...)
}
