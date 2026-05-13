package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/nightshift/internal/config"
)

func fakeExecCommand(_ string, _ ...string) error {
	return nil // pretend every command succeeds
}

func TestGenerateSystemdJiraService(t *testing.T) {
	svc := generateSystemdJiraService()

	checks := []string{
		"%h/.local/bin/nightshift jira run",
		"Restart=on-failure",
		"RestartSec=60s",
		"EnvironmentFile=-%h/.config/nightshift/env",
		"SyslogIdentifier=nightshift-jira",
		"After=network-online.target",
	}
	for _, check := range checks {
		if !strings.Contains(svc, check) {
			t.Errorf("service file missing %q", check)
		}
	}
	// Must NOT contain a hardcoded home path.
	home, _ := os.UserHomeDir()
	if strings.Contains(svc, home) {
		t.Errorf("service file must use %%h, not hardcoded home %q", home)
	}
}

func TestGenerateSystemdJiraTimer(t *testing.T) {
	onCalendar := "*-*-* 22:00:00"
	timer := generateSystemdJiraTimer(onCalendar)

	if !strings.Contains(timer, onCalendar) {
		t.Errorf("timer file missing OnCalendar value %q", onCalendar)
	}
	if !strings.Contains(timer, "Persistent=true") {
		t.Errorf("timer file missing Persistent=true")
	}
	if !strings.Contains(timer, "WantedBy=timers.target") {
		t.Errorf("timer file missing WantedBy=timers.target")
	}
}

func TestInstallSystemdJira_writesFiles(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cfg := &config.Config{
		Systemd: config.SystemdConfig{
			Enabled:    true,
			Mode:       "schedule",
			OnCalendar: "*-*-* 22:00:00",
		},
	}
	if err := installSystemdJiraWithExec(cfg, fakeExecCommand); err != nil {
		t.Fatalf("installSystemdJira: %v", err)
	}

	systemdDir := filepath.Join(tmpHome, ".config", "systemd", "user")
	for _, name := range []string{systemdJiraServiceName, systemdJiraTimerName} {
		if _, err := os.Stat(filepath.Join(systemdDir, name)); err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
		}
	}
}

func TestInstallSystemdJira_continuousMode_noTimer(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cfg := &config.Config{
		Systemd: config.SystemdConfig{
			Enabled: true,
			Mode:    "continuous",
		},
	}
	if err := installSystemdJiraWithExec(cfg, fakeExecCommand); err != nil {
		t.Fatalf("installSystemdJira: %v", err)
	}

	systemdDir := filepath.Join(tmpHome, ".config", "systemd", "user")
	if _, err := os.Stat(filepath.Join(systemdDir, systemdJiraServiceName)); err != nil {
		t.Errorf("expected service file to exist")
	}
	if _, err := os.Stat(filepath.Join(systemdDir, systemdJiraTimerName)); err == nil {
		t.Errorf("timer file must NOT exist in continuous mode")
	}
}
