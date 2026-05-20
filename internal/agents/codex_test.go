package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewCodexAgent_Defaults(t *testing.T) {
	agent := NewCodexAgent()

	if agent.binaryPath != "codex" {
		t.Errorf("binaryPath = %q, want %q", agent.binaryPath, "codex")
	}
	if agent.timeout != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", agent.timeout, DefaultTimeout)
	}
	if agent.runner == nil {
		t.Error("expected non-nil runner")
	}
}

func TestNewCodexAgent_WithOptions(t *testing.T) {
	mockRunner := &MockRunner{}
	agent := NewCodexAgent(
		WithBinaryPath("/custom/codex"),
		WithDefaultTimeout(5*time.Minute),
		WithRunner(mockRunner),
	)

	if agent.binaryPath != "/custom/codex" {
		t.Errorf("binaryPath = %q, want %q", agent.binaryPath, "/custom/codex")
	}
	if agent.timeout != 5*time.Minute {
		t.Errorf("timeout = %v, want %v", agent.timeout, 5*time.Minute)
	}
	if agent.runner != mockRunner {
		t.Error("expected custom runner")
	}
}

func TestCodexAgent_Name(t *testing.T) {
	agent := NewCodexAgent()
	if agent.Name() != "codex" {
		t.Errorf("Name() = %q, want %q", agent.Name(), "codex")
	}
}

func TestCodexAgent_Execute_Success(t *testing.T) {
	mock := &MockRunner{
		Stdout:   "Task completed successfully",
		ExitCode: 0,
	}
	agent := NewCodexAgent(WithRunner(mock))

	result, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt:  "fix the bug",
		WorkDir: "/project",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !result.IsSuccess() {
		t.Error("expected IsSuccess() to be true")
	}
	if result.Output != "Task completed successfully" {
		t.Errorf("Output = %q, want %q", result.Output, "Task completed successfully")
	}

	// Verify captured values
	if mock.CapturedName != "codex" {
		t.Errorf("binary = %q, want %q", mock.CapturedName, "codex")
	}
	if len(mock.CapturedArgs) != 3 || mock.CapturedArgs[0] != "exec" || mock.CapturedArgs[1] != "--dangerously-bypass-approvals-and-sandbox" {
		t.Errorf("args = %v, want [exec --dangerously-bypass-approvals-and-sandbox <directive>]", mock.CapturedArgs)
	}
	if !strings.HasPrefix(mock.CapturedArgs[2], promptFileDirectivePrefix) {
		t.Errorf("args[2] = %q, want prefix %q", mock.CapturedArgs[2], promptFileDirectivePrefix)
	}
	if !strings.Contains(mock.CapturedPromptFileData, "fix the bug") {
		t.Errorf("prompt file content = %q, expected to contain %q", mock.CapturedPromptFileData, "fix the bug")
	}
	if mock.CapturedDir != "/project" {
		t.Errorf("dir = %q, want %q", mock.CapturedDir, "/project")
	}
}

func TestCodexAgent_Execute_JSONOutput(t *testing.T) {
	mock := &MockRunner{
		Stdout:   `{"status":"success","files_changed":3}`,
		ExitCode: 0,
	}
	agent := NewCodexAgent(WithRunner(mock))

	result, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt: "analyze code",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JSON == nil {
		t.Error("expected JSON to be extracted")
	}
	if string(result.JSON) != `{"status":"success","files_changed":3}` {
		t.Errorf("JSON = %s", result.JSON)
	}
}

func TestCodexAgent_Execute_Timeout(t *testing.T) {
	mock := &MockRunner{
		Delay: 5 * time.Second, // Will be cancelled
	}
	agent := NewCodexAgent(
		WithRunner(mock),
		WithDefaultTimeout(50*time.Millisecond),
	)

	result, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt: "long task",
	})

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
	if result.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", result.ExitCode)
	}
	if !strings.Contains(result.Error, "timeout") {
		t.Errorf("Error = %q, want timeout message", result.Error)
	}
}

func TestCodexAgent_Execute_WithOptionsTimeout(t *testing.T) {
	mock := &MockRunner{
		Delay: 5 * time.Second,
	}
	agent := NewCodexAgent(
		WithRunner(mock),
		WithDefaultTimeout(10*time.Second), // Long default
	)

	result, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt:  "task",
		Timeout: 50 * time.Millisecond, // Short override
	})

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
}

func TestCodexAgent_Execute_ExitError(t *testing.T) {
	mock := &MockRunner{
		Stdout:   "",
		Stderr:   "command failed: no such file",
		ExitCode: 1,
		Err:      errors.New("exit status 1"),
	}
	agent := NewCodexAgent(WithRunner(mock))

	result, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt: "bad task",
	})

	if err == nil {
		t.Error("expected error")
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
	if result.IsSuccess() {
		t.Error("expected IsSuccess() to be false")
	}
}

func TestCodexAgent_Execute_BinaryNotFound(t *testing.T) {
	mock := &MockRunner{
		Err: errors.New("executable file not found"),
	}
	agent := NewCodexAgent(
		WithBinaryPath("/nonexistent/codex"),
		WithRunner(mock),
	)

	result, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt: "test",
	})

	if err == nil {
		t.Error("expected error for missing binary")
	}
	if result == nil {
		t.Fatal("expected result even on error")
		return
	}
	if result.Error == "" {
		t.Error("expected error message in result")
	}
}

func TestCodexAgent_Execute_WithFiles(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	mock := &MockRunner{
		Stdout:   "analyzed file",
		ExitCode: 0,
	}
	agent := NewCodexAgent(WithRunner(mock))

	result, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt: "review code",
		Files:  []string{testFile},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(mock.CapturedPromptFileData, testFile) {
		t.Error("expected file path in prompt file")
	}
	if !strings.Contains(mock.CapturedPromptFileData, "# Related Files") {
		t.Error("expected related files header in prompt file")
	}
	if result.Output != "analyzed file" {
		t.Errorf("Output = %q", result.Output)
	}
}

func TestCodexAgent_Execute_MissingFile(t *testing.T) {
	mock := &MockRunner{Stdout: "ok", ExitCode: 0}
	agent := NewCodexAgent(WithRunner(mock))

	// Missing files are listed by path only — no read, no error.
	_, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt: "review",
		Files:  []string{"/nonexistent/file.go"},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(mock.CapturedPromptFileData, "/nonexistent/file.go") {
		t.Error("expected file path listed in prompt file")
	}
}

func TestCodexAgent_ExecuteWithFiles(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(testFile, []byte("func main() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	mock := &MockRunner{
		Stdout:   "ok",
		ExitCode: 0,
	}
	agent := NewCodexAgent(WithRunner(mock))

	result, err := agent.ExecuteWithFiles(context.Background(), "analyze", []string{testFile}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "ok" {
		t.Errorf("Output = %q", result.Output)
	}
	if mock.CapturedDir != tmpDir {
		t.Errorf("WorkDir = %q, want %q", mock.CapturedDir, tmpDir)
	}
}


func TestCodexAgent_extractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantJSON bool
	}{
		{"plain json", `{"key":"value"}`, true},
		{"json array", `[1,2,3]`, true},
		{"json with prefix", `Some text {"key":"value"}`, true},
		{"json with suffix", `{"key":"value"} more text`, true},
		{"json with both", `prefix {"key":"value"} suffix`, true},
		{"no json", `plain text no json here`, false},
		{"invalid json", `{"key":}`, false},
		{"nested json", `{"outer":{"inner":"value"}}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON([]byte(tt.input))
			if tt.wantJSON && result == nil {
				t.Error("expected JSON, got nil")
			}
			if !tt.wantJSON && result != nil {
				t.Errorf("expected nil, got %s", result)
			}
		})
	}
}

func TestCodexAgent_Available(t *testing.T) {
	// Test with known available binary
	agent := NewCodexAgent(WithBinaryPath("echo"))
	if !agent.Available() {
		t.Error("expected echo to be available")
	}

	// Test with nonexistent binary
	agent = NewCodexAgent(WithBinaryPath("/nonexistent/binary"))
	if agent.Available() {
		t.Error("expected nonexistent binary to not be available")
	}
}

func TestCodexAgent_Version(t *testing.T) {
	agent := NewCodexAgent(WithBinaryPath("/nonexistent/codex"))
	_, err := agent.Version()
	if err == nil {
		t.Error("expected error for nonexistent binary")
	}
}

func TestCodexAgent_ContextCancellation(t *testing.T) {
	mock := &MockRunner{
		Delay: 5 * time.Second,
	}
	agent := NewCodexAgent(
		WithRunner(mock),
		WithDefaultTimeout(10*time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := agent.Execute(ctx, ExecuteOptions{
		Prompt: "task",
	})

	if err == nil {
		t.Error("expected error for cancelled context")
	}
	if result == nil {
		t.Fatal("expected result")
	}
}

func TestCodexAgent_ImplementsAgentInterface(t *testing.T) {
	// Verify CodexAgent implements the Agent interface
	var _ Agent = (*CodexAgent)(nil)
}

func TestCodexAgent_Execute_WithModel(t *testing.T) {
	mock := &MockRunner{Stdout: "response", ExitCode: 0}
	agent := NewCodexAgent(
		WithModel("gpt-5.1-codex"),
		WithRunner(mock),
	)

	_, err := agent.Execute(context.Background(), ExecuteOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(mock.CapturedArgs, "--model") {
		t.Error("expected --model in args")
	}
	if !containsArg(mock.CapturedArgs, "gpt-5.1-codex") {
		t.Error("expected model value in args")
	}
}

func TestCodexAgent_Execute_NoModel(t *testing.T) {
	mock := &MockRunner{Stdout: "response", ExitCode: 0}
	agent := NewCodexAgent(WithRunner(mock))

	_, err := agent.Execute(context.Background(), ExecuteOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(mock.CapturedArgs, "--model") {
		t.Error("expected no --model flag when model is not set")
	}
}

func TestCodexAgent_Execute_ModelFromOptions(t *testing.T) {
	mock := &MockRunner{Stdout: "response", ExitCode: 0}
	agent := NewCodexAgent(
		WithModel("gpt-5.2"),
		WithRunner(mock),
	)

	_, err := agent.Execute(context.Background(), ExecuteOptions{
		Prompt: "test",
		Model:  "gpt-5.3-codex",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsArg(mock.CapturedArgs, "gpt-5.3-codex") {
		t.Error("expected ExecuteOptions.Model to override agent default")
	}
	if containsArg(mock.CapturedArgs, "gpt-5.2") {
		t.Error("expected agent default model to be overridden")
	}
}

// TestCodexAgentDefaultsBypassFlag verifies that a CodexAgent created with no
// options includes --dangerously-bypass-approvals-and-sandbox (required for
// non-interactive execution). This guards against Bug #19 fix 3, where
// newCodexAgentFromConfig() was inadvertently stripping the flag when the
// config field defaulted to false.
func TestCodexAgentDefaultsBypassFlag(t *testing.T) {
	mock := &MockRunner{Stdout: "done"}
	agent := NewCodexAgent(WithRunner(mock))

	ctx := context.Background()
	_, err := agent.Execute(ctx, ExecuteOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	found := false
	for _, arg := range mock.CapturedArgs {
		if arg == "--dangerously-bypass-approvals-and-sandbox" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --dangerously-bypass-approvals-and-sandbox in args %v", mock.CapturedArgs)
	}
}

func TestCodexAgent_Effort(t *testing.T) {
	tests := []struct {
		name        string
		agentEffort string
		optsEffort  string
		wantEffort  string
		wantFlag    bool
	}{
		{"agent default used", "medium", "", "medium", true},
		{"opts override agent default", "medium", "high", "high", true},
		{"opts alone", "", "xhigh", "xhigh", true},
		{"no effort set", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockRunner{Stdout: "response", ExitCode: 0}
			opts := []Option{WithRunner(mock)}
			if tt.agentEffort != "" {
				opts = append(opts, WithEffort(tt.agentEffort))
			}
			agent := NewCodexAgent(opts...)

			_, err := agent.Execute(context.Background(), ExecuteOptions{
				Prompt:          "test",
				ReasoningEffort: tt.optsEffort,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			wantConfigVal := "model_reasoning_effort=" + tt.wantEffort
			hasFlag := containsArg(mock.CapturedArgs, "--config")
			hasVal := containsArg(mock.CapturedArgs, wantConfigVal)

			if tt.wantFlag {
				if !hasFlag {
					t.Errorf("expected --config in args %v", mock.CapturedArgs)
				}
				if !hasVal {
					t.Errorf("expected %q in args %v", wantConfigVal, mock.CapturedArgs)
				}
			} else {
				// Ensure no model_reasoning_effort config value present
				for _, a := range mock.CapturedArgs {
					if strings.Contains(a, "model_reasoning_effort") {
						t.Errorf("unexpected model_reasoning_effort in args %v", mock.CapturedArgs)
					}
				}
			}
		})
	}
}

// TestCodexAgentExplicitFalseBypassDisablesFlag verifies that explicitly calling
// WithDangerouslyBypassApprovalsAndSandbox(false) does disable the flag.
func TestCodexAgentExplicitFalseBypassDisablesFlag(t *testing.T) {
	mock := &MockRunner{Stdout: "done"}
	agent := NewCodexAgent(
		WithBypassPermissions(false),
		WithRunner(mock),
	)

	ctx := context.Background()
	_, err := agent.Execute(ctx, ExecuteOptions{Prompt: "test"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for _, arg := range mock.CapturedArgs {
		if arg == "--dangerously-bypass-approvals-and-sandbox" {
			t.Errorf("expected flag to be absent when explicitly disabled, got args %v", mock.CapturedArgs)
		}
	}
}
