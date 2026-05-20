package agents

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// agentConfig holds fields common to all three agents.
type agentConfig struct {
	binaryPath        string
	timeout           time.Duration
	runner            CommandRunner
	model             string
	effort            string
	bypassPermissions bool // each agent maps this to its own CLI flag
}

// Option configures any agent that embeds agentConfig.
type Option func(*agentConfig)

// WithBinaryPath sets a custom path to the agent binary.
func WithBinaryPath(path string) Option {
	return func(c *agentConfig) { c.binaryPath = path }
}

// WithDefaultTimeout sets the default execution timeout.
func WithDefaultTimeout(d time.Duration) Option {
	return func(c *agentConfig) { c.timeout = d }
}

// WithRunner sets a custom command runner (for testing).
func WithRunner(r CommandRunner) Option {
	return func(c *agentConfig) { c.runner = r }
}

// WithModel sets the default model to use.
func WithModel(model string) Option {
	return func(c *agentConfig) { c.model = model }
}

// WithEffort sets the default reasoning effort level.
func WithEffort(effort string) Option {
	return func(c *agentConfig) { c.effort = effort }
}

// WithBypassPermissions sets whether to pass the agent-specific bypass-permissions flag.
func WithBypassPermissions(enabled bool) Option {
	return func(c *agentConfig) { c.bypassPermissions = enabled }
}

// Deprecated type aliases — use Option instead.
type ClaudeOption = Option
type CodexOption = Option
type CopilotOption = Option

// Deprecated: use WithBinaryPath instead.
func WithCodexBinaryPath(path string) Option {
	return WithBinaryPath(path)
}

// Deprecated: use WithDefaultTimeout instead.
func WithCodexDefaultTimeout(d time.Duration) Option {
	return WithDefaultTimeout(d)
}

// Deprecated: use WithRunner instead.
func WithCodexRunner(r CommandRunner) Option {
	return WithRunner(r)
}

// Deprecated: use WithModel instead.
func WithCodexModel(model string) Option {
	return WithModel(model)
}

// Deprecated: use WithEffort instead.
func WithCodexEffort(effort string) Option {
	return WithEffort(effort)
}

// Deprecated: use WithBinaryPath instead.
func WithCopilotBinaryPath(path string) Option {
	return WithBinaryPath(path)
}

// Deprecated: use WithDefaultTimeout instead.
func WithCopilotDefaultTimeout(d time.Duration) Option {
	return WithDefaultTimeout(d)
}

// Deprecated: use WithRunner instead.
func WithCopilotRunner(r CommandRunner) Option {
	return WithRunner(r)
}

// Deprecated: use WithModel instead.
func WithCopilotModel(model string) Option {
	return WithModel(model)
}

// Deprecated: use WithEffort instead.
func WithCopilotEffort(effort string) Option {
	return WithEffort(effort)
}

// Deprecated: use WithBypassPermissions instead.
func WithDangerouslySkipPermissions(enabled bool) Option {
	return WithBypassPermissions(enabled)
}

// Deprecated: use WithBypassPermissions instead.
func WithDangerouslyBypassApprovalsAndSandbox(enabled bool) Option {
	return WithBypassPermissions(enabled)
}

// Deprecated: use WithBypassPermissions instead.
func WithCopilotDangerouslySkipPermissions(enabled bool) Option {
	return WithBypassPermissions(enabled)
}

// cliAvailable checks if a binary is available in PATH.
func cliAvailable(binaryPath string) bool {
	_, err := exec.LookPath(binaryPath)
	return err == nil
}

// cliVersion returns the version string reported by a CLI binary via --version.
func cliVersion(binaryPath string) (string, error) {
	cmd := exec.Command(binaryPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
