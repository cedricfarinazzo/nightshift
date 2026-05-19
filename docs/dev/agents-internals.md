# Agents Internals

`internal/agents/` — wrappers around external coding agent CLIs.

## Interface

```go
type Agent interface {
    Name() string
    Execute(ctx context.Context, opts ExecuteOptions) (ExecuteResult, error)
}

type ExecuteOptions struct {
    PromptPrefix string         // small, never compressed
    Prompt       string         // large, compressible
    PromptSuffix string         // small, never compressed (instructions, schemas)
    Model        string
    Compression  *CompressConfig
    Timeout      time.Duration  // default 30m via DefaultTimeout
    WorkDir      string
}

type ExecuteResult struct {
    Output        string
    ExitCode      int
    JSON          map[string]any  // extracted if agent emitted JSON
    CompressStats *CompressStats
    Duration      time.Duration
}
```

Compressible vs protected: see [Architecture](architecture.md). Putting instructions in `Prompt` risks them being stripped by the compression agent.

## Three Implementations

| File | Binary | Notable flags |
|------|--------|---------------|
| `claude.go` | `claude` | `--print`, `--dangerously-skip-permissions`, `--model` |
| `codex.go` | `codex` | `exec`, `--dangerously-bypass-approvals-and-sandbox`, `--model` |
| `copilot.go` | `gh copilot` or `copilot` | `-p`, `--no-ask-user`, `--silent`, `--allow-all-tools`, `--allow-all-urls` |

All three:

1. `writePromptFile()` — full prompt to `os.CreateTemp("", "nightshift-prompt-*.md")`
2. Pass short directive (`"Read and follow the task instructions in file: <path>"`) as CLI arg — bypasses `ARG_MAX`
3. `runner.Run()` via `CommandRunner` interface (testable; never call `exec.Command` directly)
4. Shared `handleExecuteResult()` in `util.go`
5. `defer cleanup()` to delete the temp file

## `CommandRunner`

```go
type CommandRunner interface {
    Run(ctx context.Context, name string, args []string, stdin io.Reader) (stdout string, stderr string, exitCode int, err error)
}
```

Default impl uses `exec.CommandContext`. Tests substitute a `MockRunner` that records calls and returns canned output.

## Shared post-run (`util.go`)

```go
func handleExecuteResult(out, errOut string, exitCode int, err error, compressStats *CompressStats) (ExecuteResult, error)
```

- Maps exit codes → result status
- Detects timeout (`context.DeadlineExceeded`)
- Extracts JSON if agent emitted ```json ... ``` block
- Propagates `CompressStats` so token savings show in reports

Use this helper for any new agent — never duplicate exit/timeout/JSON logic.

## Compression

`compress.go`:

```go
type CompressConfig struct {
    Enabled         bool
    Provider        string         // claude / codex / copilot
    Model           string
    ReasoningEffort string
    ThresholdChars  int
}

func CompressPrompt(ctx context.Context, opts ExecuteOptions) (string, *CompressStats, error)
```

Threshold check + fallback. If `len(opts.Prompt) < ThresholdChars`, returns it unchanged. Otherwise calls `compressViaAgent`:

```go
func compressViaAgent(ctx context.Context, cfg *CompressConfig, text string) (string, error)
```

`compressViaAgent` builds a meta-prompt + calls the configured provider's `Agent.Execute`. So compression uses the **CLI path**, never a direct API call — same `writePromptFile` + temp-file pattern.

## Import-cycle gotcha

`agents` cannot import `config` (config → jira → agents creates a cycle). Two structs:

- `agents.CompressConfig` — used inside the agents package
- `config.PromptCompressionConfig` — used in YAML loading

Bridge: `compressionConfigFromApp()` in `cmd/nightshift/commands/helpers.go`. Don't try to unify them.

## Adding a new agent

1. New file `internal/agents/<name>.go` implementing `Agent`
2. Use `writePromptFile()` for prompts
3. Call `handleExecuteResult()` after `runner.Run()`
4. Register the agent factory wherever provider preference is resolved
5. Add config block in `internal/config/config.go`
6. Tests with `MockRunner` mirroring existing patterns

## Default timeout

`DefaultTimeout = 30 * time.Minute`. Override via `ExecuteOptions.Timeout`.
