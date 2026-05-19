# Run Lifecycle

End-to-end trace of `nightshift run`, daemon-triggered or manual.

## Top-level

`cmd/nightshift/commands/run.go` is the entry point. It:

1. Loads config (`internal/config`)
2. Opens DB (`internal/db.Open` — auto-migrates)
3. Loads budget manager (`internal/budget.NewManager`)
4. Constructs orchestrator (`internal/orchestrator.New`)
5. Builds preflight summary
6. Confirms (or auto-skip in non-TTY) → runs orchestrator

## Daemon path

`cmd/nightshift/commands/daemon.go` registers the schedule (`internal/scheduler`) and on each fire calls the same orchestrator entry point as `nightshift run`. Cron uses `cron.SkipIfStillRunning(cron.DiscardLogger)` so overlapping fires are dropped.

## Orchestrator loop

`internal/orchestrator/orchestrator.go`:

```go
RunTask(ctx, project, task) error {
    if err := validateGitRepo(ctx, workDir); err != nil { return err }
    orig := CurrentBranch(workDir)
    defer checkoutBranch(workDir, orig)

    opts := buildExecuteOptions(task, project)  // Prompt + PromptPrefix + PromptSuffix
    if cfg.Compression.Enabled { opts.Compression = &cfg.Compression }

    for i := 0; i < DefaultMaxIterations; i++ {
        plan := planAgent.Execute(ctx, opts)
        impl := implementAgent.Execute(ctx, withPlan(opts, plan))
        if reviewPasses(impl) { return nil }
    }
    return ErrAbandoned
}
```

Default `DefaultMaxIterations = 3`.

## Prompt assembly

`ExecuteOptions` has three slots:

| Field | Compressed? | Contents |
|-------|-------------|----------|
| `PromptPrefix` | No | System rules, repo metadata (small) |
| `Prompt` | **Yes** | Task-specific data (often large) |
| `PromptSuffix` | No | Output schema, behavioral rules (small) |

If `Compression` is set and `len(Prompt) > threshold`, `compressViaAgent()` runs the configured CLI with a caveman-style meta-prompt and substitutes the compressed text. `PromptSuffix` is never compressed — critical instructions must live there.

## Agent invocation

All three agents (`internal/agents/{claude,codex,copilot}.go`) share the same pattern:

1. `writePromptFile()` — write full prompt to `os.CreateTemp("", "nightshift-prompt-*.md")`
2. Pass short directive `"Read and follow the task instructions in file: <path>"` as CLI arg
3. `runner.Run()` — execute via `CommandRunner` interface
4. `handleExecuteResult()` (`util.go`) — post-run logic: exit-code mapping, timeout detection, JSON extraction, `CompressStats` propagation
5. `defer cleanup()` — delete temp file

Never pass large prompts as positional args (`ARG_MAX` ~128KB on Linux).

## Reporting

After each task: `internal/reporting/run_report.go` writes a markdown report. After the run: `summary.go` aggregates into a daily summary.

## Updates to State

- `state` table — last run per project/task
- `task_history` — append per-task record
- `run_history` — append per-run record with token totals
- `snapshots` — periodic provider usage capture

## Sequence Diagram

```
CLI ──┐
      ▼
   load config ──► open DB ──► budget check
                                    │
                                    ▼
                              select tasks
                                    │
            ┌───────────────────────┼───────────────────┐
            ▼                       ▼                   ▼
       project 1                project 2          project N
         │                                              
         ▼ for each task                                
       validate workdir                                  
       save branch                                       
         │                                              
         ▼ plan → implement → review                    
            │                                          
            ▼ iterate ≤ 3                              
         report                                         
         restore branch                                 
```
