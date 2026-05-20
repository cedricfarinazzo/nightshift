// claude.go implements the Agent interface for Claude Code CLI.
package agents

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// CommandRunner executes shell commands. Allows mocking in tests.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdin string) (stdout, stderr string, exitCode int, err error)
}

// ExecRunner is the default CommandRunner using os/exec.
type ExecRunner struct{}

// Run executes a command and returns output.
func (r *ExecRunner) Run(ctx context.Context, name string, args []string, dir string, stdin string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	// Use process group so timeout kills the entire process tree,
	// not just the direct child (e.g. Node wrapper leaves Rust child alive).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Kill the entire process group (negative PID)
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	err := cmd.Run()

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}

// ClaudeAgent spawns Claude Code CLI for task execution.
type ClaudeAgent struct {
	agentConfig
}

// NewClaudeAgent creates a Claude Code agent.
func NewClaudeAgent(opts ...Option) *ClaudeAgent {
	a := &ClaudeAgent{
		agentConfig: agentConfig{
			binaryPath:        "claude",
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

// Name returns "claude".
func (a *ClaudeAgent) Name() string {
	return "claude"
}

// Execute runs claude --print with the given prompt.
func (a *ClaudeAgent) Execute(ctx context.Context, opts ExecuteOptions) (*ExecuteResult, error) {
	start := time.Now()

	// Create context with the shortest applicable timeout.
	ctx, cancel, timeout := withEffectiveTimeout(ctx, a.timeout, opts.Timeout)
	defer cancel()

	// Build command args
	args := []string{"--print"}
	if a.bypassPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	// Add model if specified
	model := opts.Model
	if model == "" {
		model = a.model
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	// Add reasoning effort if specified
	effort := opts.ReasoningEffort
	if effort == "" {
		effort = a.effort
	}
	if effort != "" {
		args = append(args, "--effort", effort)
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

// ExecuteWithFiles runs claude with file context included.
func (a *ClaudeAgent) ExecuteWithFiles(ctx context.Context, prompt string, files []string, workDir string) (*ExecuteResult, error) {
	return a.Execute(ctx, ExecuteOptions{
		Prompt:  prompt,
		Files:   files,
		WorkDir: workDir,
	})
}

// Available checks if the claude binary is available in PATH.
func (a *ClaudeAgent) Available() bool {
	return cliAvailable(a.binaryPath)
}

// Version returns the claude CLI version.
func (a *ClaudeAgent) Version() (string, error) {
	return cliVersion(a.binaryPath)
}
