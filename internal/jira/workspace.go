package jira

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var validTicketKey = regexp.MustCompile(`^[A-Z][A-Z0-9]+-\d+$`)

// Workspace represents an isolated working directory for a single Jira ticket.
type Workspace struct {
	TicketKey string
	Root      string
	Repos     []RepoWorkspace
}

// RepoWorkspace holds the state of one repository inside a Workspace.
type RepoWorkspace struct {
	Name        string
	Path        string
	URL         string
	Branch      string
	BaseBranch  string
	IsNew       bool
	LintCommand string // empty means auto-discover
	TestCommand string // empty means auto-discover
}

// SetupWorkspace creates (or reuses) an isolated workspace for ticketKey.
// Each repo configured in proj is cloned into {WorkspaceRoot}/{ticketKey}/{repo.Name}
// and checked out on feature/{ticketKey}.
func SetupWorkspace(ctx context.Context, cfg JiraConfig, proj ProjectConfig, ticketKey string) (*Workspace, error) {
	if !validTicketKey.MatchString(ticketKey) {
		return nil, fmt.Errorf("invalid ticket key %q: must match ^[A-Z][A-Z0-9]+-\\d+$", ticketKey)
	}
	for _, r := range proj.Repos {
		if strings.ContainsAny(r.Name, `/\`) || strings.Contains(r.Name, "..") {
			return nil, fmt.Errorf("repo name %q contains path separators or '..'", r.Name)
		}
	}
	root, err := expandHome(cfg.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	wsRoot := filepath.Join(root, ticketKey)
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir workspace: %w", err)
	}

	branch := BranchName(ticketKey)
	repos := make([]RepoWorkspace, 0, len(proj.Repos))
	for _, r := range proj.Repos {
		repoPath := filepath.Join(wsRoot, r.Name)
		if _, err := os.Stat(repoPath); err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("stat repo path %s: %w", repoPath, err)
			}
			if _, err := gitExec(ctx, wsRoot, "clone", r.URL, r.Name); err != nil {
				return nil, fmt.Errorf("clone %s: %w", r.Name, err)
			}
		}
		baseBranch := r.BaseBranch
		if baseBranch == "" {
			baseBranch = "main"
		}
		isNew, err := setupBranch(ctx, repoPath, branch, baseBranch)
		if err != nil {
			return nil, fmt.Errorf("branch %s in %s: %w", branch, r.Name, err)
		}
		repos = append(repos, RepoWorkspace{
			Name:        r.Name,
			Path:        repoPath,
			URL:         r.URL,
			Branch:      branch,
			BaseBranch:  baseBranch,
			IsNew:       isNew,
			LintCommand: r.LintCommand,
			TestCommand: r.TestCommand,
		})
	}
	return &Workspace{TicketKey: ticketKey, Root: wsRoot, Repos: repos}, nil
}

// CleanupStaleWorkspaces removes workspace directories under cfg.WorkspaceRoot
// that have not been modified in cfg.CleanupAfterDays days.
// Returns the number of workspaces removed.
func CleanupStaleWorkspaces(cfg JiraConfig) (int, error) {
	root, err := expandHome(cfg.WorkspaceRoot)
	if err != nil {
		return 0, fmt.Errorf("workspace root: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read workspace dir: %w", err)
	}
	if cfg.CleanupAfterDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -cfg.CleanupAfterDays)
	removed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
				return removed, fmt.Errorf("remove workspace %s: %w", e.Name(), err)
			}
			removed++
		}
	}
	return removed, nil
}

// expandHome replaces "~" or a leading "~/" prefix with the user's home directory.
// It does not expand "~user" style paths.
func expandHome(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	sep := string(filepath.Separator)
	if !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, "~"+sep) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, path[2:]), nil
}
