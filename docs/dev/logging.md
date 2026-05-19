# Logging

`internal/logging/logging.go` — zerolog setup.

## Important: API style

**The internal `Logger` does NOT expose zerolog's chainable API.** Use the printf-style methods:

```go
log.Infof("starting run for project %s task %s", project, task)
log.Errorf("agent failed: %v", err)
log.Warnf("budget low: %.1f%% remaining", capacity)
log.Debugf("prompt size: %d bytes", len(prompt))
```

Do NOT use `.Error().Str().Msg(...)` — that chain is internal. The wrapper exists to keep call sites concise + structured-field-free in the source.

## Initialisation

`logging.Init(cfg.Logging)` is called once in `main`. Configures:

- Log level (`debug` / `info` / `warn` / `error`)
- Format (`json` or `text` — text only useful for console debug)
- Output: stderr + daily rotated file

## Log file

```
~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log
```

JSONL format. One event per line. Fields typically include:

| Field | Meaning |
|-------|---------|
| `time` | RFC3339 |
| `level` | debug/info/warn/error |
| `component` | Package emitting the log |
| `msg` | Free-text |
| `task`, `project`, `run_id`, `tokens`, `error` | Contextual |

## Component loggers

Each package gets its own logger via:

```go
log := logging.Component("orchestrator")
log.Infof(...)
```

The `component` field is auto-attached.

## Querying with jq

```bash
# Errors in the last run
jq 'select(.level=="error") | select(.run_id=="run-2026-05-19-020000")' \
   ~/.local/share/nightshift/logs/*.log

# Per-component frequency
jq -s 'group_by(.component) | map({c: .[0].component, n: length}) | sort_by(-.n)' \
   ~/.local/share/nightshift/logs/*.log

# Token spend by run
jq -s 'group_by(.run_id) | map({r: .[0].run_id, t: map(.tokens // 0) | add})' \
   ~/.local/share/nightshift/logs/*.log
```

## Rotation

Logs are NOT auto-rotated. Set up logrotate if disk pressure is a concern:

```
/home/user/.local/share/nightshift/logs/*.log {
    daily
    rotate 30
    compress
    missingok
    notifempty
}
```

## CLI helpers

```bash
nightshift logs --tail              # tail today's
nightshift logs --since 1h
nightshift logs --level error
nightshift logs --json              # raw JSONL
```

Implementation: `cmd/nightshift/commands/logs.go`.

## Don't log secrets

`internal/security` ensures credentials don't pass through logging by design (env-var-only). But be cautious when logging config blobs — `CredentialManager.Mask()` exists for that.
