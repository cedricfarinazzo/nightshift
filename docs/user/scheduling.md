# Scheduling

Nightshift runs on either a cron expression or a fixed interval. Pick one — they're mutually exclusive.

## Cron

```yaml
schedule:
  cron: "0 2 * * *"        # every night at 02:00
```

Standard 5-field cron via `robfig/cron/v3`. Examples:

| Expression | Meaning |
|-----------|---------|
| `0 2 * * *` | 02:00 daily |
| `0 */6 * * *` | every 6 hours |
| `0 2 * * 1-5` | 02:00 weekdays |
| `30 23 * * 0` | 23:30 Sundays |

## Interval

```yaml
schedule:
  interval: "8h"
```

Fires every N (Go duration). Useful when wall-clock time doesn't matter.

## SkipIfStillRunning

A run that exceeds its interval is silently skipped — the next scheduled fire just doesn't trigger. Prevents overlapping runs and unbounded goroutine growth.

## Daemon

To run scheduled, the daemon must be running:

```bash
nightshift daemon start
nightshift daemon status
```

See [Daemon Mode](../operations/daemon.md) and [Systemd Install](../operations/systemd-install.md).

## One-off Runs

The scheduler is bypassed by `nightshift run`. Useful for testing or ad-hoc work.

## See Also

- [Scheduling Internals](../dev/scheduling-internals.md)
