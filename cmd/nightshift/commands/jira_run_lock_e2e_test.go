//go:build e2e

package commands

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestJiraRunLock_E2E tests single-instance enforcement by spawning the real
// nightshift binary against a pre-written lock file.
//
// Gated on NIGHTSHIFT_JIRA_TOKEN to match the project's e2e test convention.
func TestJiraRunLock_E2E(t *testing.T) {
	if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
		t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e lock test")
	}

	// Build binary into a temp dir.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "nightshift")
	moduleRoot := filepath.Join("..", "..", "..")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/nightshift")
	buildCmd.Dir = moduleRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	t.Run("fail_fast_when_locked", func(t *testing.T) {
		homeDir := t.TempDir()
		lockDir := filepath.Join(homeDir, ".local", "share", "nightshift")
		if err := os.MkdirAll(lockDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Write lock file with the current test process PID (it is alive).
		lockContent := fmt.Sprintf("%d\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
		lockPath := filepath.Join(lockDir, "jira-run.lock")
		if err := os.WriteFile(lockPath, []byte(lockContent), 0644); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		cmd := exec.Command(binPath, "jira", "run")
		cmd.Env = append(os.Environ(), "HOME="+homeDir)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err == nil {
			t.Fatal("expected non-zero exit, got success")
		}
		combined := stdout.String() + stderr.String()
		if !bytes.Contains([]byte(combined), []byte("another jira run in progress")) {
			t.Errorf("expected 'another jira run in progress' in output, got:\n%s", combined)
		}
	})

	t.Run("wait_succeeds_after_release", func(t *testing.T) {
		homeDir := t.TempDir()
		lockDir := filepath.Join(homeDir, ".local", "share", "nightshift")
		if err := os.MkdirAll(lockDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Write lock with current PID (alive) then release it after a short delay.
		lockPath := filepath.Join(lockDir, "jira-run.lock")
		lockContent := fmt.Sprintf("%d\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
		if err := os.WriteFile(lockPath, []byte(lockContent), 0644); err != nil {
			t.Fatal(err)
		}
		go func() {
			time.Sleep(400 * time.Millisecond)
			_ = os.Remove(lockPath)
		}()

		var stdout, stderr bytes.Buffer
		// Use --wait 3s so the binary polls until the lock is released.
		cmd := exec.Command(binPath, "jira", "run", "--wait", "3s")
		cmd.Env = append(os.Environ(), "HOME="+homeDir)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		// The binary will get past the lock, then fail on config validation
		// (no valid jira config in the temp HOME). That's acceptable — the lock
		// check is the only thing under test here.
		err := cmd.Run()
		combined := stdout.String() + stderr.String()
		// Must NOT fail with "another jira run in progress".
		if bytes.Contains([]byte(combined), []byte("another jira run in progress")) {
			t.Errorf("binary should have waited past the lock; output:\n%s", combined)
		}
		// Accept any exit code — config validation failure is expected after lock passes.
		_ = err
	})
}
