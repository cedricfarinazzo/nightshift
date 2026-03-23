package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/nightshift/internal/config"
)

// writeJiraConfig writes a temporary nightshift config file with Jira settings
// and returns the path to the directory containing it.
func writeJiraConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

func TestJiraConfig_ValidLoads(t *testing.T) {
	path := writeJiraConfig(t, `
jira:
  url: "https://mycompany.atlassian.net"
  email: "user@company.com"
  api_token_env: "JIRA_API_TOKEN"
  project_keys:
    - "PROJ"
  label: "nightshift"
  workspace_path: "/tmp/workspace"
  max_tickets: 5
  concurrency: 2
  phases:
    validation:
      provider: "claude"
    implementation:
      provider: "claude"
    review:
      provider: "codex"
`)

	cfg, err := config.LoadFromPaths("", path)
	if err != nil {
		t.Fatalf("expected valid config to load, got error: %v", err)
	}

	j := cfg.Jira
	if j.URL != "https://mycompany.atlassian.net" {
		t.Errorf("unexpected url: %q", j.URL)
	}
	if j.Email != "user@company.com" {
		t.Errorf("unexpected email: %q", j.Email)
	}
	if j.APITokenEnv != "JIRA_API_TOKEN" {
		t.Errorf("unexpected api_token_env: %q", j.APITokenEnv)
	}
	if len(j.ProjectKeys) != 1 || j.ProjectKeys[0] != "PROJ" {
		t.Errorf("unexpected project_keys: %v", j.ProjectKeys)
	}
	if j.Label != "nightshift" {
		t.Errorf("unexpected label: %q", j.Label)
	}
	if j.MaxTickets != 5 {
		t.Errorf("unexpected max_tickets: %d", j.MaxTickets)
	}
	if j.Concurrency != 2 {
		t.Errorf("unexpected concurrency: %d", j.Concurrency)
	}
	if j.Phases.Validation.Provider != "claude" {
		t.Errorf("unexpected validation provider: %q", j.Phases.Validation.Provider)
	}
	if j.Phases.Review.Provider != "codex" {
		t.Errorf("unexpected review provider: %q", j.Phases.Review.Provider)
	}
}

func TestJiraConfig_DefaultsApplied(t *testing.T) {
	// A config without a jira section should still have sensible defaults
	// once the section is populated by the user (URL triggers validation).
	// Without a URL, the jira block is a no-op — just verify defaults exist.
	path := writeJiraConfig(t, `
logging:
  level: info
`)

	cfg, err := config.LoadFromPaths("", path)
	if err != nil {
		t.Fatalf("config without jira block should load cleanly: %v", err)
	}

	// Default label and api_token_env should be set.
	if cfg.Jira.Label != config.DefaultJiraLabel {
		t.Errorf("expected default label %q, got %q", config.DefaultJiraLabel, cfg.Jira.Label)
	}
	if cfg.Jira.APITokenEnv != config.DefaultJiraAPITokenEnv {
		t.Errorf("expected default api_token_env %q, got %q", config.DefaultJiraAPITokenEnv, cfg.Jira.APITokenEnv)
	}
	if cfg.Jira.Concurrency != config.DefaultJiraConcurrency {
		t.Errorf("expected default concurrency %d, got %d", config.DefaultJiraConcurrency, cfg.Jira.Concurrency)
	}
}

func TestJiraConfig_MissingEmail(t *testing.T) {
	path := writeJiraConfig(t, `
jira:
  url: "https://mycompany.atlassian.net"
`)
	_, err := config.LoadFromPaths("", path)
	if err == nil {
		t.Fatal("expected error for missing email, got nil")
	}
}

func TestJiraConfig_InvalidConcurrency(t *testing.T) {
	path := writeJiraConfig(t, `
jira:
  url: "https://mycompany.atlassian.net"
  email: "user@company.com"
  concurrency: 0
`)
	_, err := config.LoadFromPaths("", path)
	if err == nil {
		t.Fatal("expected error for concurrency=0, got nil")
	}
}

func TestJiraConfig_InvalidProvider(t *testing.T) {
	path := writeJiraConfig(t, `
jira:
  url: "https://mycompany.atlassian.net"
  email: "user@company.com"
  phases:
    validation:
      provider: "gpt-unknown"
`)
	_, err := config.LoadFromPaths("", path)
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

func TestJiraCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "jira" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'jira' command to be registered on rootCmd")
	}
}
