package jira

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	tests := []struct {
		input   string
		wantSfx string // expected suffix relative to home (empty = want exact home)
		wantRaw string // expected exact result when not home-relative
	}{
		{"~", "", ""},
		{"~/foo/bar", "foo/bar", ""},
		{"~/" + filepath.Join("a", "b"), filepath.Join("a", "b"), ""},
		{"/absolute/path", "", "/absolute/path"},
		{"relative/path", "", "relative/path"},
		{"", "", ""},
		{"~notslash", "", "~notslash"}, // ~user style — not expanded
	}
	for _, tt := range tests {
		got, err := expandHome(tt.input)
		if err != nil {
			t.Errorf("expandHome(%q) error: %v", tt.input, err)
			continue
		}
		if tt.wantRaw != "" {
			if got != tt.wantRaw {
				t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.wantRaw)
			}
			continue
		}
		if tt.input == "~" {
			if got != home {
				t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, home)
			}
			continue
		}
		if tt.wantSfx != "" {
			want := filepath.Join(home, tt.wantSfx)
			if got != want {
				t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, want)
			}
		}
	}
}

func TestExpandHome_NoDoubleSeparator(t *testing.T) {
	// Regression: path[1:] starting with "/" must not discard home.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	got, err := expandHome("~/.local/share/nightshift/workspaces")
	if err != nil {
		t.Fatalf("expandHome: %v", err)
	}
	if !strings.HasPrefix(got, home) {
		t.Errorf("expandHome result %q does not start with home %q", got, home)
	}
	if strings.Contains(got, "//") {
		t.Errorf("expandHome result %q contains double separator", got)
	}
}

func TestCleanupStaleWorkspaces_NoneStale(t *testing.T) {
	dir := t.TempDir()
	// Create a fresh subdir (mtime = now)
	if err := os.MkdirAll(filepath.Join(dir, "PROJ-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := JiraConfig{WorkspaceRoot: dir, CleanupAfterDays: 30}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("CleanupStaleWorkspaces: %v", err)
	}
	if n != 0 {
		t.Errorf("removed %d workspaces, want 0", n)
	}
}

func TestCleanupStaleWorkspaces_RemovesStale(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "PROJ-1")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	// Set mtime to 31 days ago.
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	cfg := JiraConfig{WorkspaceRoot: dir, CleanupAfterDays: 30}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("CleanupStaleWorkspaces: %v", err)
	}
	if n != 1 {
		t.Errorf("removed %d workspaces, want 1", n)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale workspace still exists after cleanup")
	}
}

func TestCleanupStaleWorkspaces_ZeroDays_NoOp(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "PROJ-1")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-100 * 24 * time.Hour)
	_ = os.Chtimes(sub, old, old)

	cfg := JiraConfig{WorkspaceRoot: dir, CleanupAfterDays: 0}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("CleanupStaleWorkspaces: %v", err)
	}
	if n != 0 {
		t.Errorf("CleanupAfterDays=0 should be a no-op, removed %d", n)
	}
}

func TestCleanupStaleWorkspaces_NonexistentRoot(t *testing.T) {
	cfg := JiraConfig{WorkspaceRoot: "/nonexistent/path/that/does/not/exist", CleanupAfterDays: 30}
	n, err := CleanupStaleWorkspaces(cfg)
	if err != nil {
		t.Fatalf("expected no error for missing root, got: %v", err)
	}
	if n != 0 {
		t.Errorf("removed %d, want 0", n)
	}
}

func TestSetupWorkspace_InvalidTicketKey(t *testing.T) {
	cfg := JiraConfig{
		WorkspaceRoot:    t.TempDir(),
		CleanupAfterDays: 30,
		Repos:            []RepoConfig{{Name: "repo", URL: "git@github.com:org/repo.git", BaseBranch: "main"}},
	}
	ctx := context.Background()
	for _, bad := range []string{"../escape", "proj-1", "PROJ", "PROJ-", "PROJ-abc", ""} {
		_, err := SetupWorkspace(ctx, cfg, bad)
		if err == nil {
			t.Errorf("SetupWorkspace with key %q should fail", bad)
		}
	}
}

func TestSetupWorkspace_InvalidRepoName(t *testing.T) {
	cfg := JiraConfig{
		WorkspaceRoot:    t.TempDir(),
		CleanupAfterDays: 30,
		Repos:            []RepoConfig{{Name: "../evil", URL: "git@github.com:org/repo.git", BaseBranch: "main"}},
	}
	_, err := SetupWorkspace(context.Background(), cfg, "PROJ-1")
	if err == nil {
		t.Error("SetupWorkspace with repo name '../evil' should fail")
	}
}
