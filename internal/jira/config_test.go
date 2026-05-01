package jira

import (
	"strings"
	"testing"
)

func TestJiraConfig_Validate(t *testing.T) {
	validProject := ProjectConfig{Key: "P", Label: "nightshift", Repos: []RepoConfig{{Name: "r", URL: "u"}}}
	tests := []struct {
		name    string
		cfg     JiraConfig
		wantErr string
	}{
		{
			"valid full config",
			JiraConfig{Site: "x", Email: "a@b", Projects: []ProjectConfig{validProject}},
			"",
		},
		{
			"missing site",
			JiraConfig{Email: "a@b", Projects: []ProjectConfig{validProject}},
			"jira.site is required",
		},
		{
			"missing email",
			JiraConfig{Site: "x", Projects: []ProjectConfig{validProject}},
			"jira.email is required",
		},
		{
			"missing project",
			JiraConfig{Site: "x", Email: "a@b", Projects: []ProjectConfig{}},
			"at least one project is required",
		},
		{
			"no repos",
			JiraConfig{Site: "x", Email: "a@b", Projects: []ProjectConfig{{Key: "P", Label: "nightshift"}}},
			"at least one repo",
		},
		{
			"repo missing url",
			JiraConfig{Site: "x", Email: "a@b", Projects: []ProjectConfig{{Key: "P", Label: "nightshift", Repos: []RepoConfig{{Name: "r"}}}}},
			"repos[0].url is required",
		},
		{
			"repo missing name",
			JiraConfig{Site: "x", Email: "a@b", Projects: []ProjectConfig{{Key: "P", Label: "nightshift", Repos: []RepoConfig{{URL: "u"}}}}},
			"repos[0].name is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("want error containing %q, got %v", tt.wantErr, err)
				}
			}
		})
	}
}

func TestJiraConfig_Defaults(t *testing.T) {
	cfg := JiraConfig{
		Repos: []RepoConfig{{Name: "r", URL: "u"}},
	}
	cfg.Defaults()

	if cfg.TokenEnv != "JIRA_API_TOKEN" {
		t.Errorf("TokenEnv = %q, want JIRA_API_TOKEN", cfg.TokenEnv)
	}
	if cfg.Label != "nightshift" {
		t.Errorf("Label = %q, want nightshift", cfg.Label)
	}
	if cfg.MaxTickets != 10 {
		t.Errorf("MaxTickets = %d, want 10", cfg.MaxTickets)
	}
	if cfg.CleanupAfterDays != 30 {
		t.Errorf("CleanupAfterDays = %d, want 30", cfg.CleanupAfterDays)
	}
	if cfg.Validation.Model != "claude-haiku-4.5" {
		t.Errorf("Validation.Model = %q, want claude-haiku-4.5", cfg.Validation.Model)
	}
	if cfg.Implement.Model != "claude-sonnet-4.5" {
		t.Errorf("Implement.Model = %q, want claude-sonnet-4.5", cfg.Implement.Model)
	}
	if cfg.ReviewFix.Model != "claude-sonnet-4.5" {
		t.Errorf("ReviewFix.Model = %q, want claude-sonnet-4.5", cfg.ReviewFix.Model)
	}
	if cfg.Repos[0].BaseBranch != "main" {
		t.Errorf("Repos[0].BaseBranch = %q, want main", cfg.Repos[0].BaseBranch)
	}
}

func TestJiraConfig_Defaults_NoOverwrite(t *testing.T) {
	cfg := JiraConfig{
		TokenEnv:   "MY_TOKEN",
		MaxTickets: 5,
		Repos:      []RepoConfig{{Name: "r", URL: "u", BaseBranch: "develop"}},
		Validation: PhaseConfig{Model: "my-model"},
	}
	cfg.Defaults()

	if cfg.TokenEnv != "MY_TOKEN" {
		t.Errorf("TokenEnv overwritten, got %q", cfg.TokenEnv)
	}
	if cfg.MaxTickets != 5 {
		t.Errorf("MaxTickets overwritten, got %d", cfg.MaxTickets)
	}
	if cfg.Repos[0].BaseBranch != "develop" {
		t.Errorf("BaseBranch overwritten, got %q", cfg.Repos[0].BaseBranch)
	}
	if cfg.Validation.Model != "my-model" {
		t.Errorf("Validation.Model overwritten, got %q", cfg.Validation.Model)
	}
}

func TestJiraConfig_BackwardCompatPromotion(t *testing.T) {
	// Old flat config with Project+Label+Repos should be promoted to Projects[0] by Defaults().
	cfg := JiraConfig{
		Site:     "x",
		Email:    "a@b",
		Project:  "VC",
		Label:    "nightshift",
		TokenEnv: "MY_TOKEN",
		Repos:    []RepoConfig{{Name: "repo", URL: "git@github.com:org/repo.git", BaseBranch: "main"}},
	}
	cfg.Defaults()

	if len(cfg.Projects) != 1 {
		t.Fatalf("expected 1 project after promotion, got %d", len(cfg.Projects))
	}
	if cfg.Projects[0].Key != "VC" {
		t.Errorf("Projects[0].Key = %q, want VC", cfg.Projects[0].Key)
	}
	if cfg.Projects[0].Label != "nightshift" {
		t.Errorf("Projects[0].Label = %q, want nightshift", cfg.Projects[0].Label)
	}
	if len(cfg.Projects[0].Repos) != 1 || cfg.Projects[0].Repos[0].Name != "repo" {
		t.Errorf("Projects[0].Repos not promoted correctly: %+v", cfg.Projects[0].Repos)
	}

	// Should also validate without error.
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate after backward-compat promotion: %v", err)
	}
}

func TestJiraConfig_MultiProject(t *testing.T) {
	cfg := JiraConfig{
		Site:  "x",
		Email: "a@b",
		// Global defaults
		Implement: PhaseConfig{Provider: "copilot", Model: "claude-sonnet-4.6", Timeout: "30m"},
		Projects: []ProjectConfig{
			{
				Key:   "VC",
				Label: "nightshift",
				Repos: []RepoConfig{{Name: "r", URL: "u"}},
				// No per-project override — should inherit global
			},
			{
				Key:   "INFRA",
				Label: "nightshift",
				Repos: []RepoConfig{{Name: "infra", URL: "git@github.com:org/infra.git"}},
				// Per-project override for implement phase
				Implement: PhaseConfig{Timeout: "45m"},
			},
		},
	}
	cfg.Defaults()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// VC project inherits global implement config.
	effVC := cfg.EffectiveImplement(cfg.Projects[0])
	if effVC.Model != "claude-sonnet-4.6" {
		t.Errorf("VC EffectiveImplement.Model = %q, want claude-sonnet-4.6", effVC.Model)
	}
	if effVC.Timeout != "30m" {
		t.Errorf("VC EffectiveImplement.Timeout = %q, want 30m", effVC.Timeout)
	}

	// INFRA project overrides timeout but inherits model.
	effINFRA := cfg.EffectiveImplement(cfg.Projects[1])
	if effINFRA.Model != "claude-sonnet-4.6" {
		t.Errorf("INFRA EffectiveImplement.Model = %q, want claude-sonnet-4.6", effINFRA.Model)
	}
	if effINFRA.Timeout != "45m" {
		t.Errorf("INFRA EffectiveImplement.Timeout = %q, want 45m", effINFRA.Timeout)
	}
}
