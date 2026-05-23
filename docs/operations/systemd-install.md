# Systemd Installation

Nightshift v1.0.0 uses a **systemd timer + oneshot service** pair instead of a
long-running daemon. The timer fires on schedule; the service runs
`nightshift run` once and exits.

## Quick install

```bash
nightshift install systemd
```

This writes two unit files to `~/.config/systemd/user/` and enables the timer.

---

## Manual unit files

### `~/.config/systemd/user/nightshift.service`

```ini
[Unit]
Description=Nightshift AI-powered code maintenance
Documentation=https://github.com/cedricfarinazzo/nightshift
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/nightshift run --yes
Environment=HOME=%h
EnvironmentFile=-%h/.config/nightshift/env
StandardOutput=journal
StandardError=journal
SyslogIdentifier=nightshift

[Install]
WantedBy=default.target
```

### `~/.config/systemd/user/nightshift.timer`

```ini
[Unit]
Description=Nightshift scheduled runs

[Timer]
OnCalendar=*-*-* 02:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

Adjust `OnCalendar=` to match your preferred schedule. Examples:

| Expression | Meaning |
|---|---|
| `*-*-* 02:00:00` | Daily at 02:00 |
| `Mon..Fri *-*-* 23:00:00` | Weeknights at 23:00 |
| `*-*-* 0/4:00:00` | Every 4 hours |

Validate with:

```bash
systemd-analyze calendar '*-*-* 02:00:00'
```

---

## Jira pipeline

Install a separate oneshot pair for `nightshift jira run`:

```bash
nightshift install --systemd-jira
```

This writes `nightshift-jira.service` and `nightshift-jira.timer`. The Jira
timer's `OnCalendar` is taken from `jira.systemd_on_calendar` in config
(default `*-*-* 22:00:00`).

---

## Enable and start

```bash
systemctl --user daemon-reload
systemctl --user enable --now nightshift.timer
# Optional: also enable jira timer
systemctl --user enable --now nightshift-jira.timer
```

## Check status

```bash
# Next fire times
systemctl --user list-timers nightshift.timer nightshift-jira.timer

# Last run logs
journalctl --user -u nightshift.service -n 50
journalctl --user -u nightshift-jira.service -n 50
```

## Disable

```bash
systemctl --user disable --now nightshift.timer
```

## Linger (run while logged out)

By default user services stop when you log out. To keep Nightshift running on a server:

```bash
sudo loginctl enable-linger $USER
```

## Environment variables

Put `KEY=VALUE` lines in `~/.config/nightshift/env` with `chmod 600`. The
generated service unit includes `EnvironmentFile=-%h/.config/nightshift/env`
so the file is picked up automatically when it exists.

---

## macOS

macOS does not have systemd. Use `launchd`:

```bash
nightshift install launchd
```

Or schedule manually with `crontab -e`:

```
0 2 * * * /usr/local/bin/nightshift run --yes >> ~/.local/share/nightshift/logs/cron.log 2>&1
```

## Uninstall

```bash
nightshift uninstall
```

Removes the unit files and reloads systemd. Config and data are preserved.

## Troubleshooting

- `systemctl --user list-timers` shows no `nightshift.timer` — run `nightshift install systemd`.
- Service fails — check journal: `journalctl --user -u nightshift.service -e`.
- CLI not found — add to `~/.config/nightshift/env`: `PATH=/usr/local/bin:/usr/bin:/home/USER/.local/bin`.
