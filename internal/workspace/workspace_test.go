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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	root := t.TempDir()

	writeMetaFile := func(t *testing.T, dir string, createdAt time.Time) {
		t.Helper()
		meta := workspaceMeta{CreatedAt: createdAt, RunID: "test", URL: "git@github.com:org/repo.git"}
		data, _ := json.Marshal(meta)
		p := filepath.Join(dir, ".nightshift-workspace.json")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatalf("writeMetaFile: WriteFile: %v", err)
		}
		// Set file mtime to match createdAt so WalkDir sees consistent timestamps.
		if err := os.Chtimes(p, createdAt, createdAt); err != nil {
			t.Fatalf("writeMetaFile: Chtimes meta: %v", err)
		}
		if err := os.Chtimes(dir, createdAt, createdAt); err != nil {
			t.Fatalf("writeMetaFile: Chtimes dir: %v", err)
		}
	}

	staleTime := time.Now().AddDate(0, 0, -10)
	freshTime := time.Now().AddDate(0, 0, -1)

	// Create stale workspace (10 days ago)
	staleDir := filepath.Join(root, "repo_stale")
	_ = os.Mkdir(staleDir, 0o755)
	writeMetaFile(t, staleDir, staleTime)

	// Create fresh workspace (1 day ago)
	freshDir := filepath.Join(root, "repo_fresh")
	_ = os.Mkdir(freshDir, 0o755)
	writeMetaFile(t, freshDir, freshTime)

	// Create workspace without metadata (falls back to mtime; we set via Chtimes)
	noMetaDir := filepath.Join(root, "repo_nometa")
	_ = os.Mkdir(noMetaDir, 0o755)
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
	t.Parallel()
	cfg := Config{Root: "/tmp/nightshift-test-nonexistent-12345", TTLDays: 7}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("expected no error for missing root, got %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 removed, got %d", n)
	}
}

// TestCleanupStaleWorkspaces_RecentFileActivity verifies that a workspace whose
// directory entry mtime is old (> TTL) but has a recently modified file inside
// is NOT reaped. This is the VC-83 regression case.
func TestCleanupStaleWorkspaces_RecentFileActivity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wsDir := filepath.Join(root, "repo_active")
	if err := os.Mkdir(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write meta file with stale CreatedAt.
	staleTime := time.Now().AddDate(0, 0, -10)
	meta := workspaceMeta{CreatedAt: staleTime, RunID: "test", URL: "git@github.com:org/repo.git"}
	data, _ := json.Marshal(meta)
	metaPath := filepath.Join(wsDir, ".nightshift-workspace.json")
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Make both dir and meta file appear old.
	if err := os.Chtimes(metaPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(wsDir, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	// Write a fresh file inside (simulates recent git activity).
	freshFile := filepath.Join(wsDir, "recent.txt")
	if err := os.WriteFile(freshFile, []byte("activity"), 0o644); err != nil {
		t.Fatal(err)
	}
	// (os.WriteFile bumps wsDir mtime — but we already set it stale before writing,
	// so the dir entry mtime is now fresh from this WriteFile. Re-apply stale on dir
	// to simulate the Linux case where git updates files but not dir entry mtime.)
	if err := os.Chtimes(wsDir, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Root: root, TTLDays: 7}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("CleanupStaleWorkspaces: %v", err)
	}
	if n != 0 {
		t.Errorf("removed %d workspaces, want 0 (active workspace should be kept)", n)
	}
	if _, err := os.Stat(wsDir); os.IsNotExist(err) {
		t.Error("active workspace with recent file was incorrectly reaped")
	}
}

// TestCleanupStaleWorkspaces_AllOld verifies that a workspace where ALL entries
// (dir, meta, files) have old mtimes IS reaped past TTL.
func TestCleanupStaleWorkspaces_AllOld(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wsDir := filepath.Join(root, "repo_stale")
	if err := os.Mkdir(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	staleTime := time.Now().AddDate(0, 0, -10)

	// Write meta with stale CreatedAt and force mtime old.
	meta := workspaceMeta{CreatedAt: staleTime, RunID: "test", URL: "git@github.com:org/repo.git"}
	data, _ := json.Marshal(meta)
	metaPath := filepath.Join(wsDir, ".nightshift-workspace.json")
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(metaPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	// Write an inner file and make it stale too.
	innerFile := filepath.Join(wsDir, "old.txt")
	if err := os.WriteFile(innerFile, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(innerFile, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	// Make dir entry stale last (after all writes).
	if err := os.Chtimes(wsDir, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Root: root, TTLDays: 7}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("CleanupStaleWorkspaces: %v", err)
	}
	if n != 1 {
		t.Errorf("removed %d workspaces, want 1", n)
	}
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Error("stale workspace was not removed")
	}
}

type fakeGitRunner struct {
	clonedURLs []string
}

func (f *fakeGitRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "clone" {
		f.clonedURLs = append(f.clonedURLs, args[1])
	}
	return "", nil
}

func TestSetupWorkspace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	fake := &fakeGitRunner{}

	cfg := Config{
		Root: root,
		Repos: []RepoConfig{
			{URL: "git@github.com:org/repo-a.git", Name: "repo-a"},
			{URL: "git@github.com:org/repo-b.git", Name: "repo-b"},
		},
		TTLDays: 7,
	}

	ws, err := SetupWorkspace(context.Background(), cfg, "abc123", WithGitRunner(fake))
	if err != nil {
		t.Fatalf("SetupWorkspace: %v", err)
	}

	if ws.ID != "abc123" {
		t.Errorf("ID = %q, want abc123", ws.ID)
	}
	if len(ws.Repos) != 2 {
		t.Fatalf("len(Repos) = %d, want 2", len(ws.Repos))
	}
	if len(fake.clonedURLs) != 2 {
		t.Fatalf("clone calls = %d, want 2", len(fake.clonedURLs))
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
