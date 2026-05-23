package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAcquirePidLock_Exclusive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	type result struct {
		lock *PidLock
		err  error
	}
	results := make(chan result, 2)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lock, err := acquirePidLock(path, "test")
			results <- result{lock, err}
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	for r := range results {
		if r.err == nil {
			successes++
			_ = r.lock.Release()
		} else if !errors.Is(r.err, ErrLockHeld) {
			t.Errorf("unexpected error: %v", r.err)
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d", successes)
	}
}

// TestAcquirePidLock_EmptyFileHeldNotStale guards against a race where one
// acquirer wins O_EXCL but hasn't yet written the PID line; a second acquirer
// reading mid-write must see ErrLockHeld (not "stale, reclaim"). Pre-fix this
// caused intermittent CI failures in TestAcquirePidLock_Exclusive (issue
// surfaced rebasing VC-103 onto VC-101).
func TestAcquirePidLock_EmptyFileHeldNotStale(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.lock")

	// Simulate the race: O_EXCL succeeded, file is empty, write has not yet
	// landed. acquirePidLock from another contender must NOT reclaim.
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := acquirePidLock(path, "test")
	if !errors.Is(err, ErrLockHeld) {
		t.Errorf("expected ErrLockHeld for empty lock file (acquire in progress), got: %v", err)
	}
	// Lock file must still exist — must not have been reclaimed.
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("lock file was reclaimed despite empty content: %v", statErr)
	}
}

func TestAcquirePidLock_StaleReclaim(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.lock")

	// Start a short-lived subprocess and wait for it to exit so we have a
	// guaranteed-dead PID (avoids flakiness from large PID ranges).
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("subprocess: %v", err)
	}
	deadPID := cmd.ProcessState.Pid()

	content := fmt.Sprintf("%d\n2020-01-01T00:00:00Z\n", deadPID)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lock, err := acquirePidLock(path, "test")
	if err != nil {
		t.Fatalf("expected stale reclaim to succeed, got: %v", err)
	}
	_ = lock.Release()
}

func TestAcquirePidLock_Release(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "release.lock")

	lock, err := acquirePidLock(path, "test")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("lock file should not exist after Release()")
	}

	// Re-acquire after release must succeed.
	lock2, err := acquirePidLock(path, "test")
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	_ = lock2.Release()
}

func TestAcquirePidLockWait_Timeout(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "wait_timeout.lock")

	lock, err := acquirePidLock(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release() //nolint:errcheck

	ctx := context.Background()
	start := time.Now()
	_, err = acquirePidLockWait(ctx, path, 200*time.Millisecond, "test")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrLockHeld) {
		t.Errorf("expected ErrLockHeld, got: %v", err)
	}
	if elapsed > 600*time.Millisecond {
		t.Errorf("wait took too long: %v (expected ≤600ms)", elapsed)
	}
}

func TestAcquirePidLockWait_Succeeds(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "wait_succeeds.lock")

	lock, err := acquirePidLock(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = lock.Release()
	}()

	ctx := context.Background()
	lock2, err := acquirePidLockWait(ctx, path, 2*time.Second, "test")
	if err != nil {
		t.Fatalf("expected success after lock release, got: %v", err)
	}
	_ = lock2.Release()
}

func TestReadPidLock_BackCompat(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "legacy.lock")

	// Legacy format: only the PID, no timestamp line.
	if err := os.WriteFile(path, []byte("12345"), 0644); err != nil {
		t.Fatal(err)
	}
	pid, start, err := readPidLock(path)
	if err != nil {
		t.Fatalf("readPidLock: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected pid 12345, got %d", pid)
	}
	if !start.IsZero() {
		t.Errorf("expected zero start time for legacy file, got %v", start)
	}
}
