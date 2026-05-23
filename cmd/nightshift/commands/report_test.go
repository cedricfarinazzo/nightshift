package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempCwd creates a temp directory, optionally writes nightshift.yaml into it,
// chdirs into it, and restores the original cwd on cleanup.
func withTempCwd(t *testing.T, configContent string) string {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if configContent != "" {
		if err := os.WriteFile(filepath.Join(dir, "nightshift.yaml"), []byte(configContent), 0o600); err != nil {
			t.Fatalf("writing config: %v", err)
		}
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return dir
}

func TestReportCmd_MalformedConfig(t *testing.T) {
	// Malformed YAML causes config.Load() to fail → RunE returns non-nil error.
	t.Setenv("HOME", t.TempDir())
	withTempCwd(t, "key: [unclosed bracket")

	err := reportCmd.RunE(reportCmd, nil)
	if err == nil {
		t.Fatal("expected error for malformed config, got nil")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("error should mention 'loading config', got: %v", err)
	}
}

func TestReportCmd_MissingConfig(t *testing.T) {
	// No config file → config.Load() returns defaults (no error) → RunE proceeds.
	t.Setenv("HOME", t.TempDir())
	withTempCwd(t, "")

	err := reportCmd.RunE(reportCmd, nil)
	// No reports exist so command prints "No run reports found" and returns nil.
	// Error must NOT be a config load failure.
	if err != nil && strings.Contains(err.Error(), "loading config") {
		t.Errorf("should not fail on config load with missing file, got: %v", err)
	}
}
