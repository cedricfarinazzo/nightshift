# Logging Guide

How structured logging works in Nightshift.

---

## Stack

Nightshift uses **`rs/zerolog`** via the `internal/logging` package. All log output flows through `logging.Logger` — never call `zerolog` or `log` (stdlib) directly.

---

## Initialization

At startup, `logging.Init(cfg)` sets the global logger:

```go
logging.Init(logging.Config{
    Level:         "info",       // trace | debug | info | warn | error
    Path:          "~/.local/share/nightshift/logs",
    Format:        "json",       // json | text
    RetentionDays: 7,
})
```

If `Init` has not been called, `logging.Get()` returns a default logger writing to `os.Stderr` at `info` level — safe for tests and early startup.

---

## Log File Location

```
~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log
```

A new file is created for each calendar day. Files older than `retention_days` are pruned on startup.

---

## Using the Logger

### Getting a logger

```go
import "github.com/marcus/nightshift/internal/logging"

log := logging.Get()                          // global logger
log := logging.Get().With().Str("component", "jira.orchestrator").Logger()
```

### Logging methods

```go
log.Infof("ticket %s: phase %s complete", key, phase)
log.Debugf("fetching PR reviews for %s", key)
log.Warnf("workspace %s is stale", wsPath)
log.Errorf("failed to push: %v", err)
```

`Infof` / `Debugf` / `Warnf` / `Errorf` are the standard methods — they accept `fmt.Sprintf` style arguments.

> **Do not** use the zerolog chainable API (`.Info().Str("k","v").Msg("...")`) in packages that use the `logging.Logger` wrapper. The chain is internal to `internal/logging`.

### Structured fields via component logger

For packages that log frequently, create a component-scoped logger:

```go
log := logging.Get().Component("jira.orchestrator")
// all messages from this logger include {"component":"jira.orchestrator"}
```

---

## Log Levels

| Level | Use for |
|-------|---------|
| `error` | Unrecoverable failures, returned as errors |
| `warn` | Degraded state, non-fatal issues |
| `info` | Significant lifecycle events (run start/end, phase transitions) |
| `debug` | Detailed flow (prompts, raw outputs, decision points) |
| `trace` | Very fine-grained (DB queries, individual HTTP requests) |

**Rule**: default level is `info`. `debug` is for investigating problems. Never log at `debug` in hot paths (tight loops, per-token operations).

---

## Log Format

### JSON (default, production)

```json
{"level":"info","component":"jira.orchestrator","time":"2026-05-01T02:14:00+02:00","message":"ticket VC-42: phase implement complete"}
```

### Text (development)

```
2026-05-01T02:14:00+02:00 INF jira.orchestrator > ticket VC-42: phase implement complete
```

Set `logging.format: text` in config for local development.

---

## Viewing Logs

```bash
# CLI shortcut
nightshift logs

# Tail live
tail -f ~/.local/share/nightshift/logs/nightshift-$(date +%Y-%m-%d).log

# JSON pretty-print with jq
tail -f ~/.local/share/nightshift/logs/nightshift-$(date +%Y-%m-%d).log | jq .

# Filter by component
cat nightshift-2026-05-01.log | jq 'select(.component == "jira.orchestrator")'

# Filter errors only
cat nightshift-2026-05-01.log | jq 'select(.level == "error")'
```

---

## What to Log

**Always log:**
- Run start and end (with duration)
- Phase transitions in the Jira pipeline
- Agent invocations (provider, model, timeout)
- Budget check results
- Error paths (with context)

**Log at debug:**
- Agent prompts and raw outputs
- Dependency graph decisions
- Budget calculation steps

**Never log:**
- Credential values (even masked — use `AuditLogger` for security events)
- User data beyond project paths and ticket keys
- PII

---

## Retention

Logs are pruned on startup. Default: 7 days. Change with:

```yaml
logging:
  retention_days: 30
```

To disable pruning: set `retention_days: 0`.
