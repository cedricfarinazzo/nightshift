// codex.go implements the Agent interface for OpenAI Codex CLI.
package agents

import (
	"context"
	"fmt"
	"time"
)

// CodexAgent spawns Codex CLI for task execution.
type CodexAgent struct {
	agentConfig
}

// NewCodexAgent creates a Codex CLI agent.
func NewCodexAgent(opts ...Option) *CodexAgent {
	a := &CodexAgent{
		agentConfig: agentConfig{
			binaryPath:        "codex",
			timeout:           DefaultTimeout,
			runner:            &ExecRunner{},
			bypassPermissions: true,
		},
	}
	for _, opt := range opts {
		opt(&a.agentConfig)
	}
	return a
}

// Name returns "codex".
func (a *CodexAgent) Name() string {
	return "codex"
}

// Execute runs codex with the given prompt in non-interactive mode.
func (a *CodexAgent) Execute(ctx context.Context, opts ExecuteOptions) (*ExecuteResult, error) {
	start := time.Now()

	// Create context with the shortest applicable timeout.
	ctx, cancel, timeout := withEffectiveTimeout(ctx, a.timeout, opts.Timeout)
	defer cancel()

	// Build command args for headless/non-interactive execution.
	// Codex CLI uses the `exec` subcommand for non-interactive mode.
	args := []string{"exec"}
	if a.bypassPermissions {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}

	// Add model if specified
	model := opts.Model
	if model == "" {
		model = a.model
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	// Add reasoning effort if specified (codex uses --config key=value form)
	effort := opts.ReasoningEffort
	if effort == "" {
		effort = a.effort
	}
	if effort != "" {
		args = append(args, "--config", "model_reasoning_effort="+effort)
	}

	// Write prompt (optionally compressed) + file context to a temp file.
	// Pass a short directive as the arg to avoid OS ARG_MAX limits.
	var compressStats *CompressStats
	if opts.Prompt != "" {
		promptPath, cleanup, cs, err := writePromptFile(ctx, opts)
		if err != nil {
			return &ExecuteResult{
				Error:    fmt.Sprintf("writing prompt file: %v", err),
				Duration: time.Since(start),
			}, err
		}
		defer cleanup()
		compressStats = cs
		args = append(args, fmt.Sprintf("Read and follow the task instructions in file: %s", promptPath))
	}

	// Run command
	stdout, stderr, exitCode, err := a.runner.Run(ctx, a.binaryPath, args, opts.WorkDir, "")
	return handleExecuteResult(ctx, stdout, stderr, exitCode, err, timeout, start, compressStats)
}

// ExecuteWithFiles runs codex with file context included.
func (a *CodexAgent) ExecuteWithFiles(ctx context.Context, prompt string, files []string, workDir string) (*ExecuteResult, error) {
	return a.Execute(ctx, ExecuteOptions{
		Prompt:  prompt,
		Files:   files,
		WorkDir: workDir,
	})
}

// Available checks if the codex binary is available in PATH.
func (a *CodexAgent) Available() bool {
	return cliAvailable(a.binaryPath)
}

// Version returns the codex CLI version.
func (a *CodexAgent) Version() (string, error) {
	return cliVersion(a.binaryPath)
}
