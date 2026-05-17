// codex.go implements the Agent interface for OpenAI Codex CLI.
package agents

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CodexAgent spawns Codex CLI for task execution.
type CodexAgent struct {
	binaryPath string        // Path to codex binary (default: "codex")
	timeout    time.Duration // Default timeout
	runner     CommandRunner // Command executor (for testing)
	bypassPerm bool          // Pass --dangerously-bypass-approvals-and-sandbox
	model      string        // Default model to use
	effort     string        // Default reasoning effort
}

// CodexOption configures a CodexAgent.
type CodexOption func(*CodexAgent)

// WithCodexBinaryPath sets a custom path to the codex binary.
func WithCodexBinaryPath(path string) CodexOption {
	return func(a *CodexAgent) {
		a.binaryPath = path
	}
}

// WithCodexDefaultTimeout sets the default execution timeout.
func WithCodexDefaultTimeout(d time.Duration) CodexOption {
	return func(a *CodexAgent) {
		a.timeout = d
	}
}

// WithDangerouslyBypassApprovalsAndSandbox sets whether to pass --dangerously-bypass-approvals-and-sandbox.
func WithDangerouslyBypassApprovalsAndSandbox(enabled bool) CodexOption {
	return func(a *CodexAgent) {
		a.bypassPerm = enabled
	}
}

// WithCodexModel sets the default model to use.
func WithCodexModel(model string) CodexOption {
	return func(a *CodexAgent) {
		a.model = model
	}
}

// WithCodexEffort sets the default reasoning effort level.
func WithCodexEffort(effort string) CodexOption {
	return func(a *CodexAgent) {
		a.effort = effort
	}
}

// WithCodexRunner sets a custom command runner (for testing).
func WithCodexRunner(r CommandRunner) CodexOption {
	return func(a *CodexAgent) {
		a.runner = r
	}
}

// NewCodexAgent creates a Codex CLI agent.
func NewCodexAgent(opts ...CodexOption) *CodexAgent {
	a := &CodexAgent{
		binaryPath: "codex",
		timeout:    DefaultTimeout,
		runner:     &ExecRunner{},
		bypassPerm: true,
	}
	for _, opt := range opts {
		opt(a)
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
	if a.bypassPerm {
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
		promptPath, cleanup, stats, err := writePromptFile(ctx, opts)
		if err != nil {
			return &ExecuteResult{
				Error:    fmt.Sprintf("writing prompt file: %v", err),
				Duration: time.Since(start),
			}, err
		}
		defer cleanup()
		compressStats = stats
		args = append(args, fmt.Sprintf("Read and follow the task instructions in file: %s", promptPath))
	}

	// Run command
	stdout, stderr, exitCode, err := a.runner.Run(ctx, a.binaryPath, args, opts.WorkDir, "")
	return handleExecuteResult(ctx, stdout, stderr, exitCode, err, timeout, start, compressStats, a.extractJSON)
}

// ExecuteWithFiles runs codex with file context included.
func (a *CodexAgent) ExecuteWithFiles(ctx context.Context, prompt string, files []string, workDir string) (*ExecuteResult, error) {
	return a.Execute(ctx, ExecuteOptions{
		Prompt:  prompt,
		Files:   files,
		WorkDir: workDir,
	})
}


func (a *CodexAgent) extractJSON(output []byte) []byte {
	return extractJSON(output)
}

// Available checks if the codex binary is available in PATH.
func (a *CodexAgent) Available() bool {
	_, err := exec.LookPath(a.binaryPath)
	return err == nil
}

// Version returns the codex CLI version.
func (a *CodexAgent) Version() (string, error) {
	cmd := exec.Command(a.binaryPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
