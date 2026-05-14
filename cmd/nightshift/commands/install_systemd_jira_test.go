package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/nightshift/internal/config"
)

func fakeExecCommand(_ string, _ ...string) error {
	return nil // pretend every command succeeds
}

// trackingExecCommand records commands executed and returns success
type trackingExecCommand struct {
	commands [][]string
}

func (t *trackingExecCommand) run(name string, args ...string) error {
	t.commands = append(t.commands, append([]string{name}, args...))
	return nil
}

func (t *trackingExecCommand) wasCalled(cmd string, args ...string) bool {
	for _, executed := range t.commands {
		if executed[0] != cmd {
			continue
		}
		if len(args) == 0 {
			return true
		}
		if len(executed)-1 != len(args) {
			continue
		}
		match := true
		for i, arg := range args {
			if executed[i+1] != arg {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestGenerateSystemdJiraService(t *testing.T) {
	svc := generateSystemdJiraService()

	// ExecStart should contain either %h expansion or an absolute path with "jira run"
	hasExpansion := strings.Contains(svc, "ExecStart=%h/.local/bin/nightshift jira run")
	hasAbsPath := strings.Contains(svc, "ExecStart=/") && strings.Contains(svc, " jira run")

	if !hasExpansion && !hasAbsPath {
		t.Errorf("service file missing valid ExecStart directive. Got:\n%s", svc)
	}

	checks := []string{
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
	// If using %h expansion, must NOT contain a hardcoded home path in ExecStart
	if hasExpansion {
		home, _ := os.UserHomeDir()
		// Check only the ExecStart line for hardcoded paths
		lines := strings.Split(svc, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "ExecStart=") && strings.Contains(line, home) {
				t.Errorf("service file must use %%h, not hardcoded home %q", home)
			}
		}
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
			OnCalendar: "*-*-* 22:00:00",
		},
	}
	tracker := &trackingExecCommand{}
	if err := installSystemdJiraWithExec(cfg, tracker.run, io.Discard); err != nil {
		t.Fatalf("installSystemdJira: %v", err)
	}

	systemdDir := filepath.Join(tmpHome, ".config", "systemd", "user")
	for _, name := range []string{systemdJiraServiceName, systemdJiraTimerName} {
		if _, err := os.Stat(filepath.Join(systemdDir, name)); err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
		}
	}

	// Assert daemon-reload was called
	if !tracker.wasCalled("systemctl", "--user", "daemon-reload") {
		t.Errorf("expected systemctl --user daemon-reload to be called")
	}
}

func TestInstallSystemdJira_alwaysWritesTimer(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cfg := &config.Config{
		Systemd: config.SystemdConfig{
			Enabled:    true,
			OnCalendar: "*-*-* 23:00:00",
		},
	}
	if err := installSystemdJiraWithExec(cfg, fakeExecCommand, io.Discard); err != nil {
		t.Fatalf("installSystemdJira: %v", err)
	}

	systemdDir := filepath.Join(tmpHome, ".config", "systemd", "user")
	if _, err := os.Stat(filepath.Join(systemdDir, systemdJiraServiceName)); err != nil {
		t.Errorf("expected service file to exist")
	}
	if _, err := os.Stat(filepath.Join(systemdDir, systemdJiraTimerName)); err != nil {
		t.Errorf("expected timer file to exist")
	}
}
