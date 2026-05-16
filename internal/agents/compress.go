package agents

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// CompressConfig controls LLM-based prompt compression.
type CompressConfig struct {
	Enabled         bool
	Provider        string // "claude", "codex", or "copilot"
	Model           string
	ReasoningEffort string
	Threshold       int // min chars; 0 = use defaultCompressThreshold
}

const defaultCompressThreshold = 3000

const compressMetaPrompt = `Compress this text to minimum tokens while preserving all technical meaning.
Drop: articles, filler words, pleasantries, hedging, redundancy.
Keep exact: code blocks, variable names, file paths, URLs, numbers, technical terms.
Output only the compressed text, no explanation.

TEXT:
`

// CompressPrompt compresses prompt via agent CLI if enabled and above threshold.
// Always returns a usable prompt — falls back to original on any error.
func CompressPrompt(ctx context.Context, cfg *CompressConfig, prompt string) string {
	if cfg == nil || !cfg.Enabled {
		return prompt
	}
	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = defaultCompressThreshold
	}
	if len(prompt) <= threshold {
		return prompt
	}

	compressed, err := compressViaAgent(ctx, cfg, prompt)
	if err != nil || compressed == "" {
		return prompt
	}
	return compressed
}

// compressViaAgent spawns the configured provider CLI with a short timeout
// to compress the prompt. Reuses existing agent Execute() so temp-file
// handling, model/effort/permissions flags are all applied automatically.
func compressViaAgent(ctx context.Context, cfg *CompressConfig, prompt string) (string, error) {
	opts := ExecuteOptions{
		Prompt:          compressMetaPrompt + prompt,
		Model:           cfg.Model,
		ReasoningEffort: cfg.ReasoningEffort,
	}

	var agent Agent
	switch strings.ToLower(cfg.Provider) {
	case "claude":
		agent = NewClaudeAgent(
			WithDangerouslySkipPermissions(true),
		)
	case "codex":
		agent = NewCodexAgent(
			WithDangerouslyBypassApprovalsAndSandbox(true),
		)
	case "copilot":
		agent = NewCopilotAgent(
			WithCopilotDangerouslySkipPermissions(true),
		)
	default:
		return "", fmt.Errorf("unsupported compression provider: %s", cfg.Provider)
	}

	result, err := agent.Execute(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("compress agent: %w", err)
	}
	return strings.TrimSpace(result.Output), nil
}

// writePromptFile writes the (optionally compressed) prompt and any file context
// to a temp file. Returns the file path and a cleanup func. Caller must call cleanup.
func writePromptFile(ctx context.Context, opts ExecuteOptions) (path string, cleanup func(), err error) {
	prompt := CompressPrompt(ctx, opts.Compression, opts.Prompt)

	f, err := os.CreateTemp("", "nightshift-prompt-*.md")
	if err != nil {
		return "", func() {}, fmt.Errorf("create prompt file: %w", err)
	}

	_, err = f.WriteString(prompt)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", func() {}, fmt.Errorf("write prompt: %w", err)
	}

	if len(opts.Files) > 0 {
		fileCtx, ferr := buildFileContext(opts.Files)
		if ferr != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", func() {}, ferr
		}
		if _, err = fmt.Fprintf(f, "\n\n---\n\n%s", fileCtx); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", func() {}, fmt.Errorf("write file context: %w", err)
		}
	}

	if err = f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", func() {}, fmt.Errorf("close prompt file: %w", err)
	}

	name := f.Name()
	return name, func() { _ = os.Remove(name) }, nil
}
