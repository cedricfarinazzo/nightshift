# Scheduling Internals

`internal/scheduler/scheduler.go` — wraps `robfig/cron/v3` with safety middleware.

## Construction

```go
sched := scheduler.New(cfg, runFunc)
sched.Start(ctx)
defer sched.Stop()
```

## Cron Wrapper

```go
c := cron.New(
    cron.WithChain(
        cron.SkipIfStillRunning(cron.DiscardLogger),
    ),
)
```

`SkipIfStillRunning` — if a previous fire is still running when the next is due, the next fire is silently dropped. Prevents:

- Overlapping runs hitting the same project simultaneously
- Unbounded goroutine growth on slow runs

`cron.DiscardLogger` silences the "skip" log (cron's own logger). Our zerolog logger captures the skip via the wrapped job.

## Two Modes

```yaml
schedule:
  cron: "0 2 * * *"
  # OR
  interval: "8h"
```

If both are set, `Validate()` errors out.

**Cron**: registered as a cron job.

**Interval**: wrapped as `cron.Every(interval)` to reuse the same `SkipIfStillRunning` machinery.

## Fire path

```
cron tick
  → wrapped middleware (SkipIfStillRunning)
  → runFunc(ctx)
       → orchestrator.RunAll(ctx)
```

`runFunc` is provided by the daemon and ties together budget check + task selection + orchestrator.

## Tests

`scheduler_test.go` uses an immediate-fire mock cron + a counting `runFunc` to verify:

- Single fire when not running
- Skipped fire when previous still running
- Stop signal propagation

## Daemon integration

`cmd/nightshift/commands/daemon.go` constructs the scheduler with a `runFunc` closure that:

1. Re-loads config (in case it changed between fires)
2. Checks budget
3. Selects tasks
4. Drives the orchestrator
5. Writes report + updates state

## Gotchas

- Config changes do not require a daemon restart — `runFunc` re-loads on each fire.
- Schedule changes (cron/interval) DO require restart — the cron job is registered once.
- `SkipIfStillRunning` is on the cron instance, not per-job — registering multiple jobs would need refactoring.
