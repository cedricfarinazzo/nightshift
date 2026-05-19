# Reporting

`internal/reporting/` — per-run reports and daily summaries.

## Files

| File | Purpose |
|------|---------|
| `run_report.go` | Per-run markdown report |
| `run_results.go` | `RunResult` types + aggregation |
| `summary.go` | Daily summary markdown |

## Run Report

Written at the end of every run to:

```
~/.local/share/nightshift/reports/run-YYYY-MM-DD-HHMMSS.md
```

Sections:

1. Header (timestamp, host, version)
2. Preflight summary (selected projects + tasks, budget at start)
3. Per-task block:
   - Status (`completed` / `failed` / `abandoned`)
   - Duration
   - Tokens in/out
   - Provider + model
   - Branch + PR URL
   - Error (if any)
   - Compression stats (if compression was enabled)
4. Aggregate stats (total tokens, total duration)

## Daily Summary

`summary.go` rolls up all runs in a day into:

```
~/.local/share/nightshift/summaries/summary-YYYY-MM-DD.md
```

Sections:

- Total runs + total tokens
- Per-project breakdown
- Per-task breakdown
- PRs opened
- Failures
- Budget at start vs end of day

Written once per day by the daemon (or on demand via `nightshift report`).

## JSON Results

`run_results.go` exposes `RunResult` for machine consumption:

```go
type RunResult struct {
    RunID       string
    StartedAt   time.Time
    FinishedAt  time.Time
    Tasks       []TaskResult
    Tokens      TokenStats
}
type TaskResult struct {
    Project   string
    Task      string
    Status    string
    Duration  time.Duration
    Tokens    TokenStats
    Provider  string
    Model     string
    Branch    string
    PRURL     string
    Error     string
    Compress  *CompressStats
}
```

Used by `nightshift report show --json` and CI integrations.

## Retention

```yaml
reporting:
  retention_days: 30
```

On daemon startup, reports + summaries older than `retention_days` are deleted. Logs are not affected (rotate separately).

## CLI surface

```bash
nightshift report list                    # last N runs
nightshift report show run-2026-05-19-020000
nightshift report show --json run-...
nightshift report today
nightshift report summary 2026-05-19
```

See `cmd/nightshift/commands/report.go`.

## Adding fields

1. Add to `TaskResult` or `RunResult` in `run_results.go`
2. Update the markdown template in `run_report.go` / `summary.go`
3. Update JSON consumers in `cmd/nightshift/commands/report.go`
4. Add tests asserting both the markdown and JSON shape
