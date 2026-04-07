package commands

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/agents"
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
	phase := jira.PhaseConfig{Provider: "claude", Model: "claude-haiku-4.5", Timeout: "2m"}
	a := createJiraAgent(phase)
	if _, ok := a.(*agents.ClaudeAgent); !ok {
		t.Errorf("expected *agents.ClaudeAgent, got %T", a)
	}
	if a.Name() != "claude" {
		t.Errorf("expected name claude, got %s", a.Name())
	}
}

func TestCreateJiraAgent_Codex(t *testing.T) {
	phase := jira.PhaseConfig{Provider: "codex", Model: "o3", Timeout: "30m"}
	a := createJiraAgent(phase)
	if _, ok := a.(*agents.CodexAgent); !ok {
		t.Errorf("expected *agents.CodexAgent, got %T", a)
	}
}

func TestCreateJiraAgent_Copilot(t *testing.T) {
	phase := jira.PhaseConfig{Provider: "copilot", Model: "", Timeout: ""}
	a := createJiraAgent(phase)
	if _, ok := a.(*agents.CopilotAgent); !ok {
		t.Errorf("expected *agents.CopilotAgent, got %T", a)
	}
}

func TestCreateJiraAgent_Default(t *testing.T) {
	// Empty provider should default to Claude.
	phase := jira.PhaseConfig{Provider: "", Model: ""}
	a := createJiraAgent(phase)
	if _, ok := a.(*agents.ClaudeAgent); !ok {
		t.Errorf("expected *agents.ClaudeAgent for empty provider, got %T", a)
	}
}

func TestCreateJiraAgent_UnknownProvider(t *testing.T) {
	phase := jira.PhaseConfig{Provider: "unknown-llm", Model: ""}
	a := createJiraAgent(phase)
	// Unknown provider falls back to Claude.
	if _, ok := a.(*agents.ClaudeAgent); !ok {
		t.Errorf("expected *agents.ClaudeAgent fallback for unknown provider, got %T", a)
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
