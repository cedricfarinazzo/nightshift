package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid SSH URLs",
			cfg: Config{
				Root: "~/.nightshift/workspaces",
				Repos: []RepoConfig{
					{URL: "git@github.com:org/repo.git", Name: "repo"},
				},
			},
		},
		{
			name: "HTTPS URL rejected",
			cfg: Config{
				Repos: []RepoConfig{
					{URL: "https://github.com/org/repo.git", Name: "repo"},
				},
			},
			wantErr: true,
			errMsg:  "must start with git@",
		},
		{
			name: "name with path separator rejected",
			cfg: Config{
				Repos: []RepoConfig{
					{URL: "git@github.com:org/repo.git", Name: "org/repo"},
				},
			},
			wantErr: true,
			errMsg:  "path separators",
		},
		{
			name: "name with .. rejected",
			cfg: Config{
				Repos: []RepoConfig{
					{URL: "git@github.com:org/repo.git", Name: ".."},
				},
			},
			wantErr: true,
			errMsg:  "path separators or '..'",
		},
		{
			name: "multiple valid repos",
			cfg: Config{
				Repos: []RepoConfig{
					{URL: "git@github.com:org/repo-a.git", Name: "repo-a"},
					{URL: "git@github.com:org/repo-b.git", Name: "repo-b"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:org/repo.git", "repo"},
		{"git@github.com:org/repo-custom.git", "repo-custom"},
		{"git@github.com:org/repo", "repo"},
	}
	for _, tt := range tests {
		got := NormalizeName(tt.url)
		if got != tt.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestCleanupStaleWorkspaces(t *testing.T) {
	root := t.TempDir()

	writeMetaFile := func(dir string, createdAt time.Time) {
		meta := workspaceMeta{CreatedAt: createdAt, RunID: "test", URL: "git@github.com:org/repo.git"}
		data, _ := json.Marshal(meta)
		_ = os.WriteFile(filepath.Join(dir, ".nightshift-workspace.json"), data, 0o644)
	}

	// Create stale workspace (10 days ago)
	staleDir := filepath.Join(root, "repo_stale")
	_ = os.Mkdir(staleDir, 0o755)
	writeMetaFile(staleDir, time.Now().AddDate(0, 0, -10))

	// Create fresh workspace (1 day ago)
	freshDir := filepath.Join(root, "repo_fresh")
	_ = os.Mkdir(freshDir, 0o755)
	writeMetaFile(freshDir, time.Now().AddDate(0, 0, -1))

	// Create workspace without metadata (falls back to mtime; we set via Chtimes)
	noMetaDir := filepath.Join(root, "repo_nometa")
	_ = os.Mkdir(noMetaDir, 0o755)
	staleTime := time.Now().AddDate(0, 0, -10)
	_ = os.Chtimes(noMetaDir, staleTime, staleTime)

	cfg := Config{Root: root, TTLDays: 7}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("CleanupStaleWorkspaces: %v", err)
	}
	// stale + nometa removed; fresh kept
	if n != 2 {
		t.Errorf("removed %d workspaces, want 2", n)
	}
	if _, err := os.Stat(freshDir); os.IsNotExist(err) {
		t.Error("fresh workspace was incorrectly removed")
	}
	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Error("stale workspace was not removed")
	}
}

func TestCleanupStaleWorkspaces_NonexistentRoot(t *testing.T) {
	cfg := Config{Root: "/tmp/nightshift-test-nonexistent-12345", TTLDays: 7}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("expected no error for missing root, got %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 removed, got %d", n)
	}
}

func TestSetupWorkspace(t *testing.T) {
	root := t.TempDir()

	var clonedURLs []string
	orig := gitExecFn
	defer func() { gitExecFn = orig }()
	gitExecFn = func(_ context.Context, dir string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "clone" {
			clonedURLs = append(clonedURLs, args[1])
		}
		return "", nil
	}

	cfg := Config{
		Root: root,
		Repos: []RepoConfig{
			{URL: "git@github.com:org/repo-a.git", Name: "repo-a"},
			{URL: "git@github.com:org/repo-b.git", Name: "repo-b"},
		},
		TTLDays: 7,
	}

	ws, err := SetupWorkspace(context.Background(), cfg, "abc123")
	if err != nil {
		t.Fatalf("SetupWorkspace: %v", err)
	}

	if ws.ID != "abc123" {
		t.Errorf("ID = %q, want abc123", ws.ID)
	}
	if len(ws.Repos) != 2 {
		t.Fatalf("len(Repos) = %d, want 2", len(ws.Repos))
	}
	if len(clonedURLs) != 2 {
		t.Fatalf("clone calls = %d, want 2", len(clonedURLs))
	}

	// Verify workspace paths contain runID
	for _, rw := range ws.Repos {
		if !strings.Contains(filepath.Base(rw.Path), "abc123") {
			t.Errorf("path %q does not contain runID", rw.Path)
		}
		// Verify metadata file was written
		metaPath := filepath.Join(rw.Path, ".nightshift-workspace.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			t.Errorf("metadata not written for %s: %v", rw.Name, err)
			continue
		}
		var meta workspaceMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			t.Errorf("metadata malformed for %s: %v", rw.Name, err)
		}
		if meta.RunID != "abc123" {
			t.Errorf("meta.RunID = %q, want abc123", meta.RunID)
		}
		if meta.URL != rw.URL {
			t.Errorf("meta.URL = %q, want %q", meta.URL, rw.URL)
		}
	}
}
