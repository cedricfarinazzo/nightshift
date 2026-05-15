package commands

import (
	"context"
	"slices"
	"testing"

	"github.com/marcus/nightshift/internal/agents"
	"github.com/marcus/nightshift/internal/config"
)

// mockRunner captures args for helpers tests.
type mockRunner struct {
	captured []string
}

func (m *mockRunner) Run(_ context.Context, _ string, args []string, _ string, _ string) (string, string, int, error) {
	m.captured = args
	return "ok", "", 0, nil
}

func TestNewClaudeAgentFromConfig_ReasoningEffort(t *testing.T) {
	tests := []struct {
		name       string
		effort     string
		wantFlag   bool
		wantEffort string
	}{
		{"effort set", "high", true, "high"},
		{"effort empty", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &mockRunner{}
			cfg := &config.Config{}
			cfg.Providers.Claude.ReasoningEffort = tt.effort

			a := newClaudeAgentFromConfig(cfg, agents.WithRunner(mr))
			_, _ = a.Execute(context.Background(), agents.ExecuteOptions{Prompt: "test"})

			hasFlag := slices.Contains(mr.captured, "--effort")
			if hasFlag != tt.wantFlag {
				t.Errorf("--effort present = %v, want %v (args: %v)", hasFlag, tt.wantFlag, mr.captured)
			}
			if tt.wantFlag && !slices.Contains(mr.captured, tt.wantEffort) {
				t.Errorf("effort value %q not in args %v", tt.wantEffort, mr.captured)
			}
		})
	}
}

func TestNewCodexAgentFromConfig_ReasoningEffort(t *testing.T) {
	tests := []struct {
		name       string
		effort     string
		wantFlag   bool
		wantEffort string
	}{
		{"effort set", "medium", true, "model_reasoning_effort=medium"},
		{"effort empty", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &mockRunner{}
			cfg := &config.Config{}
			cfg.Providers.Codex.ReasoningEffort = tt.effort

			a := newCodexAgentFromConfig(cfg, agents.WithCodexRunner(mr))
			_, _ = a.Execute(context.Background(), agents.ExecuteOptions{Prompt: "test"})

			hasConfig := slices.Contains(mr.captured, "--config")
			hasVal := slices.Contains(mr.captured, tt.wantEffort)
			if tt.wantFlag {
				if !hasConfig {
					t.Errorf("--config not in args %v", mr.captured)
				}
				if !hasVal {
					t.Errorf("effort value %q not in args %v", tt.wantEffort, mr.captured)
				}
			} else {
				for _, a := range mr.captured {
					if len(a) > 22 && a[:22] == "model_reasoning_effort" {
						t.Errorf("unexpected model_reasoning_effort in args %v", mr.captured)
					}
				}
			}
		})
	}
}

func TestNewCopilotAgentFromConfig_ReasoningEffort(t *testing.T) {
	tests := []struct {
		name       string
		effort     string
		wantFlag   bool
		wantEffort string
	}{
		{"effort set", "xhigh", true, "xhigh"},
		{"effort empty", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &mockRunner{}
			cfg := &config.Config{}
			cfg.Providers.Copilot.ReasoningEffort = tt.effort

			a := newCopilotAgentFromConfig(cfg, "", agents.WithCopilotRunner(mr))
			_, _ = a.Execute(context.Background(), agents.ExecuteOptions{Prompt: "test"})

			hasFlag := slices.Contains(mr.captured, "--effort")
			if hasFlag != tt.wantFlag {
				t.Errorf("--effort present = %v, want %v (args: %v)", hasFlag, tt.wantFlag, mr.captured)
			}
			if tt.wantFlag && !slices.Contains(mr.captured, tt.wantEffort) {
				t.Errorf("effort value %q not in args %v", tt.wantEffort, mr.captured)
			}
		})
	}
}
