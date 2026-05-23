# Scheduling Internals

As of v1.0.0, Nightshift has **no in-process scheduler**. The `internal/scheduler/` package was removed. Scheduling is fully delegated to the operating system.

## How It Works

```
systemd timer (OnCalendar=*-*-* 02:00:00)
  └─ fires → runs  nightshift run --yes
                   nightshift jira run --yes
```

The service unit is `Type=oneshot` — it starts, runs to completion, and exits. The timer fires the next scheduled run. No daemon process runs between fires.

## Overlap Prevention

`Persistent=true` on the timer ensures missed fires are caught on next boot. Overlapping runs are impossible by construction: the timer only starts the service when the previous invocation has exited (oneshot semantics).

## Configuring the Schedule

Edit `~/.config/systemd/user/nightshift.timer`:

```ini
[Timer]
OnCalendar=*-*-* 02:00:00
Persistent=true
```

Validate with `systemd-analyze calendar '<expression>'`. Reload with `systemctl --user daemon-reload && systemctl --user restart nightshift.timer`.

## `assertDaemonNotActive`

`cmd/nightshift/commands/helpers.go` contains `assertDaemonNotActive()`, which checks `systemctl --user is-active` for old daemon unit names (`nightshift-daemon.service`, `nightshift.service` in daemon mode). If an old daemon is still active, `nightshift run` and `nightshift jira run` abort with a clear migration message.

## macOS / Non-systemd

Use `launchd` or `cron`:

```bash
nightshift install launchd
# or:
crontab -e
# 0 2 * * * /usr/local/bin/nightshift run --yes >> ~/.local/share/nightshift/logs/cron.log 2>&1
```

## See Also

- [Systemd Install](../operations/systemd-install.md)
- [Scheduling](../user/scheduling.md)
