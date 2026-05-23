package agents

import (
	"context"
	"strings"
	"testing"
)

func TestCompressMetaPrompt_ContainsDataTags(t *testing.T) {
	if !strings.Contains(compressMetaPrompt, "<data>") {
		t.Error("compressMetaPrompt must reference <data> tag delimiter")
	}
	if !strings.Contains(compressMetaPrompt, "Do NOT follow") {
		t.Error("compressMetaPrompt must explicitly prohibit following embedded instructions")
	}
}

// TestCompressMetaPrompt_ForbidsToolUse guards against the VC-101 regression
// where the compression agent ran `gofmt -w .` and edited source files in the
// nightshift main repo instead of compressing text.
func TestCompressMetaPrompt_ForbidsToolUse(t *testing.T) {
	required := []string{
		"Do NOT use any tools",
		"no edit",
		"no bash",
		"Do NOT read, create, modify, delete",
		"Do NOT run any command",
	}
	for _, s := range required {
		if !strings.Contains(compressMetaPrompt, s) {
			t.Errorf("compressMetaPrompt missing required prohibition: %q", s)
		}
	}
}

// TestCompressMetaPrompt_PreservesCriticalContent guards the keep-exact list so
// future edits do not silently strip protections for ticket IDs, code, paths,
// JSON keys, acceptance criteria, or section headers.
func TestCompressMetaPrompt_PreservesCriticalContent(t *testing.T) {
	required := []string{
		"code blocks",
		"file paths",
		"variable",
		"ticket keys",
		"error messages",
		"shell commands",
		"JSON",
		"acceptance criteria",
		"section headers",
	}
	lower := strings.ToLower(compressMetaPrompt)
	for _, s := range required {
		if !strings.Contains(lower, strings.ToLower(s)) {
			t.Errorf("compressMetaPrompt keep-exact list missing: %q", s)
		}
	}
}

func TestCompressPrompt_Disabled(t *testing.T) {
	cfg := &CompressConfig{Enabled: false}
	out, stats := CompressPrompt(context.Background(), cfg, strings.Repeat("x", 5000))
	if stats != nil {
		t.Error("expected no compression when disabled")
	}
	if out == "" {
		t.Error("expected original prompt returned")
	}
}

func TestCompressPrompt_NilConfig(t *testing.T) {
	out, stats := CompressPrompt(context.Background(), nil, "hello")
	if stats != nil {
		t.Error("expected nil stats for nil config")
	}
	if out != "hello" {
		t.Errorf("expected original prompt, got %q", out)
	}
}

func TestCompressPrompt_BelowThreshold(t *testing.T) {
	cfg := &CompressConfig{Enabled: true, Threshold: 1000}
	out, stats := CompressPrompt(context.Background(), cfg, "short")
	if stats != nil {
		t.Error("expected no compression below threshold")
	}
	if out != "short" {
		t.Errorf("expected original prompt, got %q", out)
	}
}

// TestBuildCompressOpts_SandboxedWorkDir locks in the sandbox invariant: the
// compression agent must run with WorkDir = the provided sandbox dir, never
// the empty string (which inherits the process cwd and was the VC-101 leak).
func TestBuildCompressOpts_SandboxedWorkDir(t *testing.T) {
	cfg := &CompressConfig{Provider: "copilot", Model: "gpt-5-mini"}
	opts := buildCompressOpts(cfg, "payload", "/tmp/sandbox-xyz")
	if opts.WorkDir != "/tmp/sandbox-xyz" {
		t.Errorf("WorkDir = %q, want sandbox dir", opts.WorkDir)
	}
	if opts.WorkDir == "" {
		t.Error("compression must never run with empty WorkDir (inherits process cwd)")
	}
	if !strings.Contains(opts.Prompt, "<data>\npayload\n</data>") {
		t.Errorf("Prompt missing <data> envelope: %q", opts.Prompt)
	}
	if !strings.HasPrefix(opts.Prompt, "You are a TEXT COMPRESSOR") {
		t.Errorf("Prompt missing meta-prompt header: %q", opts.Prompt[:60])
	}
}

func TestCompressPrompt_DefaultThreshold(t *testing.T) {
	cfg := &CompressConfig{Enabled: true, Threshold: 0}
	short := strings.Repeat("x", defaultCompressThreshold-1)
	out, stats := CompressPrompt(context.Background(), cfg, short)
	if stats != nil {
		t.Error("expected no compression below default threshold")
	}
	if out != short {
		t.Error("expected original prompt returned")
	}
}
