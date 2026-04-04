package jira

import (
	"testing"
)

func validCfg() JiraConfig {
	return JiraConfig{
		Site:     "mysite",
		Email:    "user@example.com",
		TokenEnv: "NIGHTSHIFT_JIRA_TOKEN",
		Project:  "PROJ",
		Label:    "nightshift",
		Repos:    []RepoConfig{{Name: "repo", URL: "https://github.com/org/repo"}},
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     JiraConfig
		envVal  string
		wantErr bool
	}{
		{
			name:    "valid config",
			cfg:     validCfg(),
			envVal:  "test-token",
			wantErr: false,
		},
		{
			name: "missing site",
			cfg: func() JiraConfig {
				c := validCfg()
				c.Site = ""
				return c
			}(),
			envVal:  "test-token",
			wantErr: true,
		},
		{
			name: "missing project",
			cfg: func() JiraConfig {
				c := validCfg()
				c.Project = ""
				return c
			}(),
			envVal:  "test-token",
			wantErr: true,
		},
		{
			name: "missing email",
			cfg: func() JiraConfig {
				c := validCfg()
				c.Email = ""
				return c
			}(),
			envVal:  "test-token",
			wantErr: true,
		},
		{
			name: "missing token_env",
			cfg: func() JiraConfig {
				c := validCfg()
				c.TokenEnv = ""
				return c
			}(),
			envVal:  "",
			wantErr: true,
		},
		{
			name:    "missing api token env var",
			cfg:     validCfg(),
			envVal:  "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.TokenEnv != "" {
				t.Setenv(tt.cfg.TokenEnv, tt.envVal)
			}
			_, err := NewClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
