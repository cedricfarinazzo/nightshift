# Systemd Install

Run Nightshift as a user-level systemd service so it survives reboots.

## Install

```bash
nightshift install
```

The `install` command writes a user unit to `~/.config/systemd/user/nightshift.service`, enables it (`systemctl --user enable`), and starts it.

## Manual Verification

```bash
systemctl --user status nightshift
systemctl --user restart nightshift
journalctl --user -u nightshift -f
```

## Unit File

The generated unit roughly:

```ini
[Unit]
Description=Nightshift autonomous coding agent scheduler
Documentation=https://github.com/cedricfarinazzo/nightshift
After=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/nightshift daemon start --foreground
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
```

Tweak the path if you installed `nightshift` somewhere other than `~/.local/bin`.

## Linger (run while logged out)

By default user services stop when you log out. To keep Nightshift running on a server:

```bash
sudo loginctl enable-linger $USER
```

## Environment Variables

API tokens must reach the service. Two options:

**Option A — drop-in:**

```bash
mkdir -p ~/.config/systemd/user/nightshift.service.d/
cat > ~/.config/systemd/user/nightshift.service.d/env.conf <<EOF
[Service]
Environment="ANTHROPIC_API_KEY=sk-ant-..."
Environment="OPENAI_API_KEY=sk-..."
Environment="NIGHTSHIFT_JIRA_TOKEN=..."
EOF
systemctl --user daemon-reload
systemctl --user restart nightshift
```

**Option B — EnvironmentFile:**

```ini
[Service]
EnvironmentFile=%h/.config/nightshift/env
```

Then put `KEY=VALUE` lines in `~/.config/nightshift/env` with `chmod 600`.

## Uninstall

```bash
nightshift uninstall
```

Stops the service and removes the unit file. Config and data are preserved.

## Troubleshooting

- `systemctl --user status nightshift` shows `failed` — check journal: `journalctl --user -u nightshift -e`.
- Service starts but never fires runs — check that schedule is configured (`nightshift config get schedule`) and the daemon is healthy (`nightshift daemon status`).
- Service can't find `claude`/`codex` — `PATH` in user service is minimal. Add to the drop-in: `Environment="PATH=/usr/local/bin:/usr/bin:%h/.local/bin"`.
