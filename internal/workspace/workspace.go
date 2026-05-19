// Package workspace manages isolated clone-based working directories for task runs.
package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config holds workspace configuration.
type Config struct {
	Root    string
	Repos   []RepoConfig
	TTLDays int // default 7
}

// RepoConfig defines a repository to clone for each workspace.
type RepoConfig struct {
	URL  string
	Name string // defaults to repo basename without .git
}

// Workspace is an isolated working directory created for a single run.
type Workspace struct {
	ID    string
	Root  string
	Repos []RepoWorkspace
}

// RepoWorkspace holds the state of one cloned repository inside a Workspace.
type RepoWorkspace struct {
	Name string
	Path string
	URL  string
}

// workspaceMeta is stored as .nightshift-workspace.json inside each workspace dir.
type workspaceMeta struct {
	CreatedAt time.Time `json:"created_at"`
	RunID     string    `json:"run_id"`
	URL       string    `json:"url"`
}

// gitExecFn is the function used to run git commands. Replaced in tests.
var gitExecFn = func(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		sub := strings.Join(args, " ")
		if trimmed != "" {
			return "", fmt.Errorf("git %s: %s: %w", sub, trimmed, err)
		}
		return "", fmt.Errorf("git %s: %w", sub, err)
	}
	return trimmed, nil
}

// SetupWorkspace creates a fresh isolated workspace for runID.
// Each repo is cloned into <root>/<name>_<runID>/ and a metadata file is written.
func SetupWorkspace(ctx context.Context, cfg Config, runID string) (*Workspace, error) {
	root, err := expandHome(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir workspace root: %w", err)
	}

	now := time.Now()
	repos := make([]RepoWorkspace, 0, len(cfg.Repos))
	for _, r := range cfg.Repos {
		dirName := r.Name + "_" + runID
		wsPath := filepath.Join(root, dirName)
		if err := os.MkdirAll(wsPath, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir workspace %s: %w", dirName, err)
		}
		if _, err := gitExecFn(ctx, wsPath, "clone", r.URL, "."); err != nil {
			return nil, fmt.Errorf("clone %s: %w", r.Name, err)
		}
		meta := workspaceMeta{CreatedAt: now, RunID: runID, URL: r.URL}
		if err := writeMeta(wsPath, meta); err != nil {
			return nil, fmt.Errorf("write metadata for %s: %w", r.Name, err)
		}
		repos = append(repos, RepoWorkspace{Name: r.Name, Path: wsPath, URL: r.URL})
	}

	return &Workspace{
		ID:    runID,
		Root:  root,
		Repos: repos,
	}, nil
}

// CleanupStaleWorkspaces removes workspace directories under cfg.Root that are
// older than cfg.TTLDays. Returns the count of removed directories.
func CleanupStaleWorkspaces(cfg Config) (int, error) {
	root, err := expandHome(cfg.Root)
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

	ttl := cfg.TTLDays
	if ttl <= 0 {
		ttl = 7
	}
	cutoff := time.Now().AddDate(0, 0, -ttl)
	removed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(root, e.Name())
		age, err := workspaceAge(dirPath, e)
		if err != nil {
			continue
		}
		if age.Before(cutoff) {
			if err := os.RemoveAll(dirPath); err != nil {
				return removed, fmt.Errorf("remove workspace %s: %w", e.Name(), err)
			}
			removed++
		}
	}
	return removed, nil
}

// ValidateConfig checks workspace config for common errors.
func ValidateConfig(cfg Config) error {
	for i, r := range cfg.Repos {
		if !strings.HasPrefix(r.URL, "git@") {
			return fmt.Errorf("workspace.repos[%d].url must start with git@ (SSH URL)", i)
		}
		if strings.ContainsAny(r.Name, `/\`) || strings.Contains(r.Name, "..") {
			return fmt.Errorf("workspace.repos[%d].name %q contains path separators or '..'", i, r.Name)
		}
	}
	return nil
}

// NormalizeName derives the repo name from its URL when Name is empty.
// Example: "git@github.com:org/repo.git" → "repo"
func NormalizeName(url string) string {
	base := filepath.Base(url)
	return strings.TrimSuffix(base, ".git")
}

func workspaceAge(dirPath string, e os.DirEntry) (time.Time, error) {
	metaPath := filepath.Join(dirPath, ".nightshift-workspace.json")
	data, err := os.ReadFile(metaPath)
	if err == nil {
		var meta workspaceMeta
		if json.Unmarshal(data, &meta) == nil && !meta.CreatedAt.IsZero() {
			return meta.CreatedAt, nil
		}
	}
	info, err := e.Info()
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

func writeMeta(wsPath string, meta workspaceMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(wsPath, ".nightshift-workspace.json"), data, 0o644)
}

func expandHome(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	sep := string(filepath.Separator)
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~"+sep) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
