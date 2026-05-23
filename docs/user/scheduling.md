# Scheduling

Nightshift v1.0.0 uses a **systemd timer + oneshot service** pair. No in-process daemon runs between jobs — the timer fires on schedule, the service runs `nightshift run --yes` once, and exits.

The `schedule:` block in `nightshift.yaml` is retained for YAML back-compat and documentation purposes but is **not acted on at runtime**. Set your actual schedule in the systemd `OnCalendar=` expression (see below).

## Quick setup

```bash
nightshift install systemd
systemctl --user enable --now nightshift.timer
```

Or run through the interactive wizard (it installs the timer for you):

```bash
nightshift setup
```

## Setting the schedule

Edit `~/.config/systemd/user/nightshift.timer`:

```ini
[Timer]
OnCalendar=*-*-* 02:00:00
Persistent=true
```

Common expressions:

| Expression | Meaning |
|---|---|
| `*-*-* 02:00:00` | Daily at 02:00 |
| `Mon..Fri *-*-* 23:00:00` | Weeknights at 23:00 |
| `*-*-* 0/6:00:00` | Every 6 hours |
| `Sun *-*-* 23:30:00` | 23:30 Sundays |

Validate before applying:

```bash
systemd-analyze calendar '*-*-* 02:00:00'
```

After editing, reload:

```bash
systemctl --user daemon-reload
systemctl --user restart nightshift.timer
```

## Jira pipeline timer

A separate timer pair handles `nightshift jira run`:

```bash
nightshift install --systemd-jira
systemctl --user enable --now nightshift-jira.timer
```

Its `OnCalendar` comes from `jira.systemd_on_calendar` in config (default `*-*-* 22:00:00`).

## Manual / one-off runs

Run outside the timer at any time:

```bash
nightshift run
nightshift run --yes   # skip confirmation
```

## Check timer status

```bash
systemctl --user list-timers nightshift.timer nightshift-jira.timer
journalctl --user -u nightshift.service -n 50
```

## Run while logged out (servers)

```bash
sudo loginctl enable-linger $USER
```

## See Also

- [Systemd Install](../operations/systemd-install.md)
