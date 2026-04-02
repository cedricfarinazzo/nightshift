package jira

import (
	"strings"
	"testing"
)

func TestJiraConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     JiraConfig
		wantErr string
	}{
		{
			"valid full config",
			JiraConfig{Site: "x", Email: "a@b", Project: "P", Repos: []RepoConfig{{Name: "r", URL: "u"}}},
			"",
		},
		{
			"missing site",
			JiraConfig{Email: "a@b", Project: "P", Repos: []RepoConfig{{Name: "r", URL: "u"}}},
			"jira.site is required",
		},
		{
			"missing email",
			JiraConfig{Site: "x", Project: "P", Repos: []RepoConfig{{Name: "r", URL: "u"}}},
			"jira.email is required",
		},
		{
			"missing project",
			JiraConfig{Site: "x", Email: "a@b", Repos: []RepoConfig{{Name: "r", URL: "u"}}},
			"jira.project is required",
		},
		{
			"no repos",
			JiraConfig{Site: "x", Email: "a@b", Project: "P"},
			"at least one repo",
		},
		{
			"repo missing url",
			JiraConfig{Site: "x", Email: "a@b", Project: "P", Repos: []RepoConfig{{Name: "r"}}},
			"repos[0].url is required",
		},
		{
			"repo missing name",
			JiraConfig{Site: "x", Email: "a@b", Project: "P", Repos: []RepoConfig{{URL: "u"}}},
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
