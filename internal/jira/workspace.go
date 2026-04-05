package jira

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Workspace represents an isolated working directory for a single Jira ticket.
type Workspace struct {
	TicketKey string
	Root      string
	Repos     []RepoWorkspace
}

// RepoWorkspace holds the state of one repository inside a Workspace.
type RepoWorkspace struct {
	Name       string
	Path       string
	URL        string
	Branch     string
	BaseBranch string
	IsNew      bool
}

// SetupWorkspace creates (or reuses) an isolated workspace for ticketKey.
// Each configured repo is cloned into {WorkspaceRoot}/{ticketKey}/{repo.Name}
// and checked out on feature/{ticketKey}.
func SetupWorkspace(ctx context.Context, cfg JiraConfig, ticketKey string) (*Workspace, error) {
	root, err := expandHome(cfg.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	wsRoot := filepath.Join(root, ticketKey)
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir workspace: %w", err)
	}

	branch := BranchName(ticketKey)
	repos := make([]RepoWorkspace, 0, len(cfg.Repos))
	for _, r := range cfg.Repos {
		repoPath := filepath.Join(wsRoot, r.Name)
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
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
			Name:       r.Name,
			Path:       repoPath,
			URL:        r.URL,
			Branch:     branch,
			BaseBranch: baseBranch,
			IsNew:      isNew,
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

// expandHome replaces a leading "~" with the user's home directory.
func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, path[1:]), nil
}
