// Package jira provides configuration types for Jira integration.
package jira

import (
	"fmt"
	"os"
	"path/filepath"
)

// ProjectConfig defines a single Jira project with its own key, label, repos,
// and optional per-project phase overrides that take precedence over the global
// phase configs on JiraConfig.
type ProjectConfig struct {
	Key   string       `mapstructure:"key"`   // Jira project key (e.g., "PROJ")
	Label string       `mapstructure:"label"` // label filter (e.g., "nightshift")
	Repos []RepoConfig `mapstructure:"repos"`

	// Optional per-project phase overrides; zero-value means inherit global.
	Validation PhaseConfig `mapstructure:"validation"`
	Plan       PhaseConfig `mapstructure:"plan"`
	Implement  PhaseConfig `mapstructure:"implement"`
	ReviewFix  PhaseConfig `mapstructure:"review_fix"`
}

// JiraConfig holds all Jira-related configuration.
type JiraConfig struct {
	// Connection
	Site     string `mapstructure:"site"`      // e.g., "mysite" (becomes mysite.atlassian.net)
	Email    string `mapstructure:"email"`     // Jira user email for auth
	TokenEnv string `mapstructure:"token_env"` // env var name holding API token (default: JIRA_API_TOKEN)

	// Multi-project list (preferred). Each project has its own key, label, repos.
	Projects []ProjectConfig `mapstructure:"projects"`

	// Deprecated flat single-project fields — kept for backward compatibility.
	// When Projects is empty and Project is non-empty, Defaults() promotes them
	// to Projects[0] automatically.
	Project string       `mapstructure:"project"` // deprecated: use Projects[0].Key
	Label   string       `mapstructure:"label"`   // deprecated: use Projects[0].Label
	Repos   []RepoConfig `mapstructure:"repos"`   // deprecated: use Projects[0].Repos

	// Per-phase provider selection (global defaults, overridable per-project).
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
	Name        string `mapstructure:"name"`         // subdirectory name
	URL         string `mapstructure:"url"`          // git clone URL
	BaseBranch  string `mapstructure:"base_branch"`  // default: "main"
	LintCommand string `mapstructure:"lint_command"` // optional: command to run linter (e.g. "golangci-lint run ./...")
	TestCommand string `mapstructure:"test_command"` // optional: command to run tests (e.g. "go test ./...")
}

// PhaseConfig specifies provider/model for a specific phase.
type PhaseConfig struct {
	Provider string `mapstructure:"provider"` // claude, codex, copilot
	Model    string `mapstructure:"model"`    // model name
	Timeout  string `mapstructure:"timeout"`  // e.g., "30m", "2m"
}

// mergePhaseConfig returns the project-level override merged over the global default.
// A non-empty field in override takes precedence over the corresponding global field.
func mergePhaseConfig(global, override PhaseConfig) PhaseConfig {
	result := global
	if override.Provider != "" {
		result.Provider = override.Provider
	}
	if override.Model != "" {
		result.Model = override.Model
	}
	if override.Timeout != "" {
		result.Timeout = override.Timeout
	}
	return result
}

// EffectiveValidation returns the effective validation PhaseConfig for the given project.
// The project's override takes precedence over the global default when non-empty.
func (c *JiraConfig) EffectiveValidation(proj ProjectConfig) PhaseConfig {
	return mergePhaseConfig(c.Validation, proj.Validation)
}

// EffectivePlan returns the effective plan PhaseConfig for the given project.
func (c *JiraConfig) EffectivePlan(proj ProjectConfig) PhaseConfig {
	return mergePhaseConfig(c.Plan, proj.Plan)
}

// EffectiveImplement returns the effective implement PhaseConfig for the given project.
func (c *JiraConfig) EffectiveImplement(proj ProjectConfig) PhaseConfig {
	return mergePhaseConfig(c.Implement, proj.Implement)
}

// EffectiveReviewFix returns the effective review-fix PhaseConfig for the given project.
func (c *JiraConfig) EffectiveReviewFix(proj ProjectConfig) PhaseConfig {
	return mergePhaseConfig(c.ReviewFix, proj.ReviewFix)
}

// Validate checks that required config fields are set.
// Defaults() must be called before Validate() so that old flat fields are
// promoted to Projects when needed.
func (c *JiraConfig) Validate() error {
	if c.Site == "" {
		return fmt.Errorf("jira.site is required")
	}
	if c.Email == "" {
		return fmt.Errorf("jira.email is required")
	}
	if len(c.Projects) == 0 {
		return fmt.Errorf("jira.projects: at least one project is required")
	}
	for i, p := range c.Projects {
		if p.Key == "" {
			return fmt.Errorf("jira.projects[%d].key is required", i)
		}
		if len(p.Repos) == 0 {
			return fmt.Errorf("jira.projects[%d].repos: at least one repo is required", i)
		}
		for j, r := range p.Repos {
			if r.Name == "" {
				return fmt.Errorf("jira.projects[%d].repos[%d].name is required", i, j)
			}
			if r.URL == "" {
				return fmt.Errorf("jira.projects[%d].repos[%d].url is required", i, j)
			}
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
	// Apply base-branch defaults to the legacy flat repos list.
	for i := range c.Repos {
		if c.Repos[i].BaseBranch == "" {
			c.Repos[i].BaseBranch = "main"
		}
	}
	// Phase defaults (global)
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

	// Backward compat: if no projects are configured but the old flat fields are
	// present, auto-promote them to Projects[0].
	if len(c.Projects) == 0 && c.Project != "" {
		label := c.Label
		if label == "" {
			label = "nightshift"
		}
		repos := make([]RepoConfig, len(c.Repos))
		copy(repos, c.Repos)
		c.Projects = []ProjectConfig{{
			Key:   c.Project,
			Label: label,
			Repos: repos,
		}}
	}

	// Apply defaults to each project's repos and label.
	for i := range c.Projects {
		if c.Projects[i].Label == "" {
			c.Projects[i].Label = "nightshift"
		}
		for j := range c.Projects[i].Repos {
			if c.Projects[i].Repos[j].BaseBranch == "" {
				c.Projects[i].Repos[j].BaseBranch = "main"
			}
		}
	}
}
