package jira

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchName(t *testing.T) {
	tests := []struct {
		ticketKey string
		want      string
	}{
		{"PROJ-123", "feature/PROJ-123"},
		{"VC-7", "feature/VC-7"},
		{"VC-1", "feature/VC-1"},
	}
	for _, tt := range tests {
		got := BranchName(tt.ticketKey)
		if got != tt.want {
			t.Errorf("BranchName(%q) = %q, want %q", tt.ticketKey, got, tt.want)
		}
	}
}

func TestCommitMessage(t *testing.T) {
	tests := []struct {
		ticketKey   string
		issueType   string
		scope       string
		description string
		want        string
	}{
		{"VC-7", "Story", "api", "add login endpoint", "feat(api): VC-7: add login endpoint"},
		{"VC-7", "Story", "", "add login endpoint", "feat: VC-7: add login endpoint"},
		{"PROJ-123", "Story", "auth", "implement OAuth", "feat(auth): PROJ-123: implement OAuth"},
		{"VC-42", "Bug", "", "fix nil pointer", "fix: VC-42: fix nil pointer"},
		{"VC-42", "Bug", "api", "fix nil pointer", "fix(api): VC-42: fix nil pointer"},
		{"VC-42", "bug", "", "case insensitive", "fix: VC-42: case insensitive"},
		{"VC-10", "Chore", "", "update deps", "chore: VC-10: update deps"},
		{"VC-10", "", "", "unknown type defaults", "feat: VC-10: unknown type defaults"},
	}
	for _, tt := range tests {
		got := CommitMessage(tt.ticketKey, tt.issueType, tt.scope, tt.description)
		if got != tt.want {
			t.Errorf("CommitMessage(%q, %q, %q, %q) = %q, want %q",
				tt.ticketKey, tt.issueType, tt.scope, tt.description, got, tt.want)
		}
	}
}

func TestBranchAheadOfBase(t *testing.T) {
	t.Run("missing remote branch is not ahead", func(t *testing.T) {
		workDir := setupRemoteRepoWithMain(t)

		ahead, err := BranchAheadOfBase(context.Background(), workDir, "feature/VC-49", "main")
		if err != nil {
			t.Fatalf("BranchAheadOfBase returned error: %v", err)
		}
		if ahead {
			t.Fatal("BranchAheadOfBase returned true for a missing remote branch")
		}
	})

	t.Run("pushed branch ahead of base", func(t *testing.T) {
		workDir := setupRemoteRepoWithMain(t)
		runGit(t, workDir, "checkout", "-b", "feature/VC-49")
		writeFile(t, workDir, "feature.txt", "feature commit\n")
		runGit(t, workDir, "add", "feature.txt")
		runGit(t, workDir, "commit", "-m", "feature commit")
		runGit(t, workDir, "push", "-u", "origin", "feature/VC-49")

		ahead, err := BranchAheadOfBase(context.Background(), workDir, "feature/VC-49", "main")
		if err != nil {
			t.Fatalf("BranchAheadOfBase returned error: %v", err)
		}
		if !ahead {
			t.Fatal("BranchAheadOfBase returned false for a pushed branch ahead of base")
		}
	})
}

func TestBranchAheadOfBase_MissingBaseRefErrors(t *testing.T) {
	remoteDir := t.TempDir()
	runGit(t, remoteDir, "init", "--bare")

	workDir := t.TempDir()
	runGit(t, workDir, "init")
	runGit(t, workDir, "config", "user.email", "test@example.com")
	runGit(t, workDir, "config", "user.name", "Test User")
	runGit(t, workDir, "remote", "add", "origin", remoteDir)
	runGit(t, workDir, "checkout", "-b", "feature/VC-49")
	writeFile(t, workDir, "feature.txt", "feature commit\n")
	runGit(t, workDir, "add", "feature.txt")
	runGit(t, workDir, "commit", "-m", "feature commit")
	runGit(t, workDir, "push", "-u", "origin", "feature/VC-49")

	ahead, err := BranchAheadOfBase(context.Background(), workDir, "feature/VC-49", "main")
	if err == nil {
		t.Fatal("BranchAheadOfBase returned nil error for a missing base ref")
	}
	if ahead {
		t.Fatal("BranchAheadOfBase returned true when the base ref was missing")
	}
}

func TestCommitAndPush_FirstPush(t *testing.T) {
	workDir := setupRemoteRepoWithMain(t)
	runGit(t, workDir, "checkout", "-b", "feature/VC-99")
	writeFile(t, workDir, "change.txt", "hello\n")

	err := CommitAndPush(context.Background(), workDir, "feat: VC-99: first commit")
	if err != nil {
		t.Fatalf("CommitAndPush (first push) failed: %v", err)
	}

	// Verify remote branch now exists.
	runGit(t, workDir, "fetch", "origin")
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/feature/VC-99")
	cmd.Dir = workDir
	if out, err2 := cmd.CombinedOutput(); err2 != nil {
		t.Fatalf("remote branch not found after first push: %v\n%s", err2, out)
	}
}

func TestCommitAndPush_SubsequentPush(t *testing.T) {
	workDir := setupRemoteRepoWithMain(t)
	runGit(t, workDir, "checkout", "-b", "feature/VC-99")
	writeFile(t, workDir, "change.txt", "hello\n")
	runGit(t, workDir, "add", "change.txt")
	runGit(t, workDir, "commit", "-m", "initial commit")
	runGit(t, workDir, "push", "-u", "origin", "feature/VC-99")

	writeFile(t, workDir, "change.txt", "updated\n")

	err := CommitAndPush(context.Background(), workDir, "feat: VC-99: second commit")
	if err != nil {
		t.Fatalf("CommitAndPush (subsequent push) failed: %v", err)
	}
}

func setupRemoteRepoWithMain(t *testing.T) string {
	t.Helper()

	remoteDir := t.TempDir()
	runGit(t, remoteDir, "init", "--bare")

	workDir := t.TempDir()
	runGit(t, workDir, "init")
	runGit(t, workDir, "config", "user.email", "test@example.com")
	runGit(t, workDir, "config", "user.name", "Test User")
	runGit(t, workDir, "remote", "add", "origin", remoteDir)
	runGit(t, workDir, "checkout", "-b", "main")
	writeFile(t, workDir, "README.md", "base commit\n")
	runGit(t, workDir, "add", "README.md")
	runGit(t, workDir, "commit", "-m", "base commit")
	runGit(t, workDir, "push", "-u", "origin", "main")

	return workDir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

func writeFile(t *testing.T, dir, name, contents string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatalf("write file %s: %v", name, err)
	}
}
