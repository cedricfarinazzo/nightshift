# Daemon Mode

The daemon is what executes scheduled runs. Without it, `nightshift run` works manually but nothing fires on schedule.

## Lifecycle

```bash
nightshift daemon start              # detach + run in background
nightshift daemon start --foreground # don't fork (debug; Ctrl-C to stop)
nightshift daemon stop
nightshift daemon status
```

## PID File

`~/.local/share/nightshift/nightshift.pid`

Created atomically with `O_CREATE|O_EXCL`. Only one daemon can run at a time. A stale PID file (process gone) is detected and removed automatically on the next start.

If you ever need to clear it manually:

```bash
rm ~/.local/share/nightshift/nightshift.pid
```

## What It Does

1. Loads config.
2. Initialises database, scheduler, snapshot collector.
3. Registers cron / interval schedule.
4. On each fire: select tasks → run orchestrator → write report.

Cron uses `SkipIfStillRunning` middleware so overlapping fires are silently dropped.

## Logs

`~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log`

JSON-line. Tail:

```bash
tail -F ~/.local/share/nightshift/logs/nightshift-*.log | jq .
```

Or:

```bash
nightshift logs --tail
```

## Status Codes

`nightshift daemon status` reports:

| State | Meaning |
|-------|---------|
| `running` | PID file present, process alive |
| `stopped` | No PID file |
| `stale` | PID file present, process gone — start will recover |

## Foreground (Debug)

```bash
nightshift daemon start --foreground --verbose
```

Logs to stderr in addition to the log file. Ctrl-C to stop.

## Autostart

Use systemd — see [Systemd Install](systemd-install.md).

## See Also

- [Logs & Reports](logs-and-reports.md)
- [Scheduling Internals](../dev/scheduling-internals.md)
