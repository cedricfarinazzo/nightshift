package commands

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/jira"
)

func TestPrintJiraPreflightSummary(t *testing.T) {
	cfg := jira.JiraConfig{
		Site:       "testsite",
		Project:    "PROJ",
		Label:      "nightshift",
		MaxTickets: 5,
		Validation: jira.PhaseConfig{Provider: "claude", Model: "claude-haiku-4.5"},
		Implement:  jira.PhaseConfig{Provider: "claude", Model: "claude-sonnet-4.5"},
		ReviewFix:  jira.PhaseConfig{Provider: "claude", Model: "claude-sonnet-4.5"},
	}

	out := captureStdout(t, func() {
		printJiraPreflightSummary(cfg, nil)
	})

	checks := []string{
		"testsite.atlassian.net",
		"PROJ",
		"nightshift",
		"claude-haiku-4.5",
		"claude-sonnet-4.5",
		"5",
	}
	for _, s := range checks {
		if !strings.Contains(out, s) {
			t.Errorf("preflight summary missing %q\nfull output:\n%s", s, out)
		}
	}
}

func TestPrintJiraRunSummary(t *testing.T) {
	results := []jira.TicketResult{
		{TicketKey: "P-1", Status: jira.TicketCompleted},
		{TicketKey: "P-2", Status: jira.TicketCompleted},
		{TicketKey: "P-3", Status: jira.TicketRejected},
		{TicketKey: "P-4", Status: jira.TicketFailed},
	}
	feedback := []jira.FeedbackResult{
		{TicketKey: "P-5", FixesMade: 2},
		{TicketKey: "P-6", FixesMade: 0},
	}

	out := captureStdout(t, func() {
		printJiraRunSummary(results, feedback, 45*time.Minute+30*time.Second)
	})

	checks := []string{"45m30s", "4 processed", "2", "1", "Reworked"}
	for _, s := range checks {
		if !strings.Contains(out, s) {
			t.Errorf("run summary missing %q\nfull output:\n%s", s, out)
		}
	}
}

func TestPrintJiraRunSummary_Skipped(t *testing.T) {
	results := []jira.TicketResult{
		{TicketKey: "P-1", Status: jira.TicketCompleted},
		{TicketKey: "P-2", Status: jira.TicketSkipped},
	}
	out := captureStdout(t, func() {
		printJiraRunSummary(results, nil, time.Second)
	})
	if !strings.Contains(out, "Skipped") {
		t.Errorf("run summary should show Skipped when tickets are skipped\nfull output:\n%s", out)
	}
}

func TestRunJira_FlagParsing(t *testing.T) {
	flags := []string{"max-tickets", "ticket", "skip-validation", "todo-only", "review-only"}
	for _, name := range flags {
		if jiraRunCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered on jira run command", name)
		}
	}
}

func TestRunJira_DryRunFlagAbsent(t *testing.T) {
	// dry-run is deferred to VC-28 and must NOT be present on this command.
	if jiraRunCmd.Flags().Lookup("dry-run") != nil {
		t.Error("--dry-run flag should not be registered (deferred to VC-28)")
	}
}

func TestCreateJiraAgent_Claude(t *testing.T) {
	cfg := &config.Config{}
	phase := jira.PhaseConfig{Provider: "claude", Model: "claude-haiku-4.5", Timeout: "2m"}
	a, err := createJiraAgent(cfg, phase)
	if err != nil {
		// claude binary may not be in PATH in CI; skip availability errors.
		if strings.Contains(err.Error(), "not found in PATH") {
			t.Skipf("claude CLI not available: %v", err)
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "claude" {
		t.Errorf("expected name claude, got %s", a.Name())
	}
}

func TestCreateJiraAgent_Codex(t *testing.T) {
	cfg := &config.Config{}
	phase := jira.PhaseConfig{Provider: "codex", Model: "o3", Timeout: "30m"}
	a, err := createJiraAgent(cfg, phase)
	if err != nil {
		if strings.Contains(err.Error(), "not found in PATH") {
			t.Skipf("codex CLI not available: %v", err)
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "codex" {
		t.Errorf("expected name codex, got %s", a.Name())
	}
}

func TestCreateJiraAgent_Copilot(t *testing.T) {
	cfg := &config.Config{}
	phase := jira.PhaseConfig{Provider: "copilot", Model: "", Timeout: ""}
	a, err := createJiraAgent(cfg, phase)
	if err != nil {
		if strings.Contains(err.Error(), "not found in PATH") {
			t.Skipf("copilot CLI not available: %v", err)
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "copilot" {
		t.Errorf("expected name copilot, got %s", a.Name())
	}
}

func TestCreateJiraAgent_Default(t *testing.T) {
	// Empty provider should default to Claude.
	cfg := &config.Config{}
	phase := jira.PhaseConfig{Provider: "", Model: ""}
	a, err := createJiraAgent(cfg, phase)
	if err != nil {
		if strings.Contains(err.Error(), "not found in PATH") {
			t.Skipf("claude CLI not available: %v", err)
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "claude" {
		t.Errorf("expected name claude for empty provider, got %s", a.Name())
	}
}

func TestCreateJiraAgent_UnknownProvider(t *testing.T) {
	// Unknown provider falls back to Claude.
	cfg := &config.Config{}
	phase := jira.PhaseConfig{Provider: "unknown-llm", Model: ""}
	a, err := createJiraAgent(cfg, phase)
	if err != nil {
		if strings.Contains(err.Error(), "not found in PATH") {
			t.Skipf("claude CLI not available: %v", err)
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "claude" {
		t.Errorf("expected claude fallback for unknown provider, got %s", a.Name())
	}
}

func TestCreateJiraAgent_InvalidTimeout(t *testing.T) {
	cfg := &config.Config{}
	phase := jira.PhaseConfig{Provider: "claude", Timeout: "notaduration"}
	_, err := createJiraAgent(cfg, phase)
	if err == nil {
		t.Error("expected error for invalid timeout, got nil")
	}
	if !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("expected 'invalid timeout' in error, got: %v", err)
	}
}

func TestCreateJiraAgent_CaseInsensitiveProvider(t *testing.T) {
	// Provider names should be normalized to lowercase.
	cfg := &config.Config{}
	for _, provider := range []string{"Claude", "CLAUDE", "claude"} {
		phase := jira.PhaseConfig{Provider: provider, Model: ""}
		a, err := createJiraAgent(cfg, phase)
		if err != nil {
			if strings.Contains(err.Error(), "not found in PATH") {
				t.Skipf("claude CLI not available: %v", err)
			}
			t.Fatalf("provider %q: unexpected error: %v", provider, err)
		}
		if a.Name() != "claude" {
			t.Errorf("provider %q: expected claude, got %s", provider, a.Name())
		}
	}
}

func TestRunJira_MissingConfig(t *testing.T) {
	// Validate() catches missing required fields.
	cfg := jira.JiraConfig{}
	cfg.Defaults()
	// Site is empty → Validate should fail.
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for empty JiraConfig, got nil")
	}
	expected := "jira.site"
	if !strings.Contains(fmt.Sprintf("%v", err), expected) {
		t.Errorf("expected error containing %q, got: %v", expected, err)
	}
}
