// Package jira provides configuration types for Jira integration.
package jira

import (
	"fmt"
	"os"
	"path/filepath"
)

// JiraConfig holds all Jira-related configuration.
type JiraConfig struct {
	// Connection
	Site     string `mapstructure:"site"`      // e.g., "mysite" (becomes mysite.atlassian.net)
	Email    string `mapstructure:"email"`     // Jira user email for auth
	TokenEnv string `mapstructure:"token_env"` // env var name holding API token (default: JIRA_API_TOKEN)

	// Project
	Project string `mapstructure:"project"` // Jira project key (e.g., "PROJ")
	Label   string `mapstructure:"label"`   // label filter (e.g., "nightshift")

	// Repos (multi-repo support)
	Repos []RepoConfig `mapstructure:"repos"`

	// Per-phase provider selection
	Validation PhaseConfig `mapstructure:"validation"`
	Plan       PhaseConfig `mapstructure:"plan"`
	Implement  PhaseConfig `mapstructure:"implement"`
	ReviewFix  PhaseConfig `mapstructure:"review_fix"`

	// Workspace
	WorkspaceRoot    string `mapstructure:"workspace_root"`     // default: ~/.local/share/nightshift/workspaces
	CleanupAfterDays int    `mapstructure:"cleanup_after_days"` // default: 30

	// Budget
	BudgetEnabled bool    `mapstructure:"budget_enabled"`  // default: true
	MaxCostPerRun float64 `mapstructure:"max_cost_per_run"` // optional cap per run (USD)

	// Behavior
	DryRun     bool `mapstructure:"dry_run"`     // log actions but don't execute
	MaxTickets int  `mapstructure:"max_tickets"` // max tickets per run (default: 10)
}

// RepoConfig defines a repository to work on.
type RepoConfig struct {
	Name       string `mapstructure:"name"`        // subdirectory name
	URL        string `mapstructure:"url"`         // git clone URL
	BaseBranch string `mapstructure:"base_branch"` // default: "main"
}

// PhaseConfig specifies provider/model for a specific phase.
type PhaseConfig struct {
	Provider string `mapstructure:"provider"` // claude, codex, copilot
	Model    string `mapstructure:"model"`    // model name
	Timeout  string `mapstructure:"timeout"`  // e.g., "30m", "2m"
}

// Validate checks that required config fields are set.
func (c *JiraConfig) Validate() error {
	if c.Site == "" {
		return fmt.Errorf("jira.site is required")
	}
	if c.Email == "" {
		return fmt.Errorf("jira.email is required")
	}
	if c.Project == "" {
		return fmt.Errorf("jira.project is required")
	}
	if len(c.Repos) == 0 {
		return fmt.Errorf("jira.repos: at least one repo is required")
	}
	for i, r := range c.Repos {
		if r.Name == "" {
			return fmt.Errorf("jira.repos[%d].name is required", i)
		}
		if r.URL == "" {
			return fmt.Errorf("jira.repos[%d].url is required", i)
		}
	}
	return nil
}

// Defaults fills in zero-value fields with sensible defaults.
func (c *JiraConfig) Defaults() {
	if c.TokenEnv == "" {
		c.TokenEnv = "JIRA_API_TOKEN"
	}
	if c.Label == "" {
		c.Label = "nightshift"
	}
	if c.WorkspaceRoot == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			c.WorkspaceRoot = filepath.Join(home, ".local", "share", "nightshift", "workspaces")
		} else {
			c.WorkspaceRoot = "~/.local/share/nightshift/workspaces"
		}
	}
	if c.CleanupAfterDays == 0 {
		c.CleanupAfterDays = 30
	}
	if c.MaxTickets == 0 {
		c.MaxTickets = 10
	}
	for i := range c.Repos {
		if c.Repos[i].BaseBranch == "" {
			c.Repos[i].BaseBranch = "main"
		}
	}
	// Phase defaults
	if c.Validation.Provider == "" {
		c.Validation.Provider = "claude"
	}
	if c.Validation.Model == "" {
		c.Validation.Model = "claude-haiku-4.5"
	}
	if c.Validation.Timeout == "" {
		c.Validation.Timeout = "2m"
	}
	if c.Plan.Provider == "" {
		c.Plan.Provider = "claude"
	}
	if c.Plan.Model == "" {
		c.Plan.Model = "claude-sonnet-4.5"
	}
	if c.Plan.Timeout == "" {
		c.Plan.Timeout = "5m"
	}
	if c.Implement.Provider == "" {
		c.Implement.Provider = "claude"
	}
	if c.Implement.Model == "" {
		c.Implement.Model = "claude-sonnet-4.5"
	}
	if c.Implement.Timeout == "" {
		c.Implement.Timeout = "30m"
	}
	if c.ReviewFix.Provider == "" {
		c.ReviewFix.Provider = "claude"
	}
	if c.ReviewFix.Model == "" {
		c.ReviewFix.Model = "claude-sonnet-4.5"
	}
	if c.ReviewFix.Timeout == "" {
		c.ReviewFix.Timeout = "20m"
	}
}
