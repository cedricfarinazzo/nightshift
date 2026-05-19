package commands

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/cedricfarinazzo/nightshift/internal/agents"
	"github.com/cedricfarinazzo/nightshift/internal/budget"
	"github.com/cedricfarinazzo/nightshift/internal/config"
	"github.com/cedricfarinazzo/nightshift/internal/db"
	"github.com/cedricfarinazzo/nightshift/internal/workspace"
)

// agentByName creates an agent for the given provider name.
// Returns an error if the provider is unknown or its CLI is not in PATH.
func agentByName(cfg *config.Config, provider string) (agents.Agent, error) {
	switch strings.ToLower(provider) {
	case "claude":
		a := newClaudeAgentFromConfig(cfg)
		if !a.Available() {
			return nil, fmt.Errorf("claude CLI not found in PATH")
		}
		return a, nil
	case "codex":
		a := newCodexAgentFromConfig(cfg)
		if !a.Available() {
			return nil, fmt.Errorf("codex CLI not found in PATH")
		}
		return a, nil
	case "copilot":
		a := newCopilotAgentFromConfig(cfg, "")
		if !a.Available() {
			return nil, fmt.Errorf("copilot CLI not found in PATH (install via 'gh' or standalone)")
		}
		return a, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: claude, codex, copilot)", provider)
	}
}

func newClaudeAgentFromConfig(cfg *config.Config, extra ...agents.ClaudeOption) *agents.ClaudeAgent {
	if cfg == nil {
		return agents.NewClaudeAgent(extra...)
	}
	opts := []agents.ClaudeOption{
		agents.WithDangerouslySkipPermissions(cfg.Providers.Claude.DangerouslySkipPermissions),
	}
	if cfg.Providers.Claude.Model != "" {
		opts = append(opts, agents.WithModel(cfg.Providers.Claude.Model))
	}
	if cfg.Providers.Claude.ReasoningEffort != "" {
		opts = append(opts, agents.WithEffort(cfg.Providers.Claude.ReasoningEffort))
	}
	opts = append(opts, extra...)
	return agents.NewClaudeAgent(opts...)
}

func newCodexAgentFromConfig(cfg *config.Config, extra ...agents.CodexOption) *agents.CodexAgent {
	if cfg == nil {
		return agents.NewCodexAgent(extra...)
	}
	// The --dangerously-bypass-approvals-and-sandbox flag is required for
	// non-interactive (headless) Codex execution. The agent defaults to true.
	// We only pass the config value when it is explicitly enabled; when the
	// field is false (Go zero value / unconfigured) we preserve the agent's
	// default so that Codex continues to work as a fallback provider even
	// when the user has only configured Claude in their nightshift.yaml.
	//
	// NOTE: Both bool fields on ProviderConfig are zero-valued false, so we
	// cannot distinguish "not configured" from "explicitly false". Erring on
	// the side of enabling the flag for headless operation is the safe choice;
	// users who want Codex to prompt for approvals should disable the provider
	// entirely rather than toggling this flag.
	opts := []agents.CodexOption{}
	if cfg.Providers.Codex.DangerouslyBypassApprovalsAndSandbox {
		opts = append(opts, agents.WithDangerouslyBypassApprovalsAndSandbox(true))
	}
	if cfg.Providers.Codex.Model != "" {
		opts = append(opts, agents.WithCodexModel(cfg.Providers.Codex.Model))
	}
	if cfg.Providers.Codex.ReasoningEffort != "" {
		opts = append(opts, agents.WithCodexEffort(cfg.Providers.Codex.ReasoningEffort))
	}
	opts = append(opts, extra...)
	return agents.NewCodexAgent(opts...)
}

// newCopilotAgentFromConfig creates a CopilotAgent from config. If binaryPath
// is non-empty it overrides auto-detection; otherwise the binary is resolved
// from PATH (preferring standalone "copilot", falling back to "gh").
// Extra CopilotOptions (e.g. phase-specific model/timeout) are applied last.
func newCopilotAgentFromConfig(cfg *config.Config, binaryPath string, extra ...agents.CopilotOption) *agents.CopilotAgent {
	if cfg == nil {
		return agents.NewCopilotAgent()
	}

	binary := binaryPath
	if binary == "" {
		// Auto-detect: prefer standalone copilot, fallback to gh
		binary = "gh"
		if _, err := exec.LookPath("copilot"); err == nil {
			binary = "copilot"
		}
	}

	opts := []agents.CopilotOption{
		agents.WithCopilotBinaryPath(binary),
		agents.WithCopilotDangerouslySkipPermissions(cfg.Providers.Copilot.DangerouslySkipPermissions),
	}
	if cfg.Providers.Copilot.Model != "" {
		opts = append(opts, agents.WithCopilotModel(cfg.Providers.Copilot.Model))
	}
	if cfg.Providers.Copilot.ReasoningEffort != "" {
		opts = append(opts, agents.WithCopilotEffort(cfg.Providers.Copilot.ReasoningEffort))
	}
	opts = append(opts, extra...)
	return agents.NewCopilotAgent(opts...)
}

// compressionConfigFromApp converts app config to agents.CompressConfig.
func compressionConfigFromApp(cfg *config.Config) *agents.CompressConfig {
	if cfg == nil || !cfg.PromptCompression.Enabled {
		return nil
	}
	return &agents.CompressConfig{
		Enabled:         true,
		Provider:        cfg.PromptCompression.Provider,
		Model:           cfg.PromptCompression.Model,
		ReasoningEffort: cfg.PromptCompression.ReasoningEffort,
		Threshold:       cfg.PromptCompression.Threshold,
	}
}

// compressionSummary returns a human-readable description of the compression config.
// Returns empty string when compression is disabled.
func compressionSummary(cfg *config.Config) string {
	if cfg == nil || !cfg.PromptCompression.Enabled {
		return ""
	}
	c := cfg.PromptCompression
	threshold := c.Threshold
	if threshold <= 0 {
		threshold = 3000
	}
	model := c.Model
	if model == "" {
		model = "default"
	}
	s := fmt.Sprintf("enabled  provider=%s  model=%s  threshold=%d chars", c.Provider, model, threshold)
	if c.ReasoningEffort != "" {
		s += "  effort=" + c.ReasoningEffort
	}
	return s
}

// newBudgetManager builds a budget.Manager from config and an open database.
func newBudgetManager(cfg *config.Config, _ *db.DB) *budget.Manager {
	return budget.NewManagerWithTracking(cfg)
}

// workspaceConfigFromApp converts app config to workspace.Config.
func workspaceConfigFromApp(cfg *config.Config) workspace.Config {
	repos := make([]workspace.RepoConfig, len(cfg.Workspace.Repos))
	for i, r := range cfg.Workspace.Repos {
		repos[i] = workspace.RepoConfig{URL: r.URL, Name: r.Name}
	}
	return workspace.Config{Root: cfg.Workspace.Root, Repos: repos, TTLDays: 7}
}
