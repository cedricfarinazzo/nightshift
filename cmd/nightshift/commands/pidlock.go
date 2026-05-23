package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/logging"
)

// ErrLockHeld is returned by acquirePidLock when a live process holds the lock.
var ErrLockHeld = errors.New("lock held by another process")

// PidLock represents an acquired PID lock file.
type PidLock struct {
	path  string
	pid   int
	start time.Time
}

// Release removes the lock file. Safe to call on a nil receiver.
func (l *PidLock) Release() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}

// acquirePidLock atomically creates path as a PID lock file using O_CREATE|O_EXCL.
// name describes what is being locked (e.g. "jira run", "daemon") and appears
// in the ErrLockHeld error message.
// Returns ErrLockHeld if a live process already holds the lock.
// Stale lock files (dead PID) are logged, removed, and retried once.
func acquirePidLock(path, name string) (*PidLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	return tryAcquireLock(path, name, time.Now())
}

func tryAcquireLock(path, name string, now time.Time) (*PidLock, error) {
	pid := os.Getpid()
	content := fmt.Sprintf("%d\n%s\n", pid, now.UTC().Format(time.RFC3339))

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err == nil {
		_, werr := f.Write([]byte(content))
		_ = f.Close()
		if werr != nil {
			_ = os.Remove(path)
			return nil, fmt.Errorf("write lock file: %w", werr)
		}
		return &PidLock{path: path, pid: pid, start: now}, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create lock file: %w", err)
	}

	// File exists — check if the recorded PID is alive.
	existingPID, existingStart, readErr := readPidLock(path)
	if readErr != nil {
		// Unreadable / partial content — another acquirer wrote the file with
		// O_EXCL but has not yet finished writing the PID line. Treating this
		// as stale would clobber the winner's lock. Surface as held instead.
		return nil, fmt.Errorf("%w: another %s acquiring lock (file content unreadable: %v)",
			ErrLockHeld, name, readErr)
	}
	if isProcessRunning(existingPID) {
		if existingStart.IsZero() {
			return nil, fmt.Errorf("%w: another %s in progress, PID %d",
				ErrLockHeld, name, existingPID)
		}
		return nil, fmt.Errorf("%w: another %s in progress, PID %d, started at %s",
			ErrLockHeld, name, existingPID, existingStart.Local().Format(time.RFC3339))
	}

	// Stale lock: remove and retry once.
	logging.Component("pidlock").Warnf("reclaiming stale lock (pid %d dead): %s", existingPID, path)
	_ = os.Remove(path)

	f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("create lock file after stale removal: %w", err)
	}
	_, werr := f.Write([]byte(content))
	_ = f.Close()
	if werr != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write lock file: %w", werr)
	}
	return &PidLock{path: path, pid: pid, start: now}, nil
}

// acquirePidLockWait loops acquirePidLock with exponential backoff until
// success, context cancellation, or timeout expiry.
// timeout==0 behaves identically to acquirePidLock (single attempt).
// name is passed through to acquirePidLock for error messages.
func acquirePidLockWait(ctx context.Context, path string, timeout time.Duration, name string) (*PidLock, error) {
	if timeout == 0 {
		return acquirePidLock(path, name)
	}

	deadline := time.Now().Add(timeout)
	delay := 500 * time.Millisecond
	const maxDelay = 10 * time.Second

	for {
		lock, err := acquirePidLock(path, name)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrLockHeld) {
			return nil, err
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, err
		}

		sleep := delay
		if sleep > remaining {
			sleep = remaining
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(sleep):
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

// readPidLock reads pid and start time from a lock file.
// Tolerates legacy files that contain only a PID with no timestamp line.
func readPidLock(path string) (pid int, start time.Time, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, time.Time{}, err
	}
	lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
	pid, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("parse pid: %w", err)
	}
	if len(lines) > 1 && strings.TrimSpace(lines[1]) != "" {
		start, _ = time.Parse(time.RFC3339, strings.TrimSpace(lines[1]))
	}
	return pid, start, nil
}

// isProcessRunning returns true if the process with the given PID is alive.
// On Unix, os.FindProcess always succeeds; signal 0 checks liveness.
// ESRCH means no such process (dead); EPERM means process exists but we
// lack permission to signal it — still running, so we return true.
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	sigErr := process.Signal(syscall.Signal(0))
	if sigErr == nil {
		return true
	}
	return errors.Is(sigErr, syscall.EPERM)
}
