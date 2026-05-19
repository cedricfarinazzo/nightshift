# Data & Backup

What lives on disk, what to back up, and how to migrate.

## Layout

```
~/.config/nightshift/
  config.yaml          # main config
  env                  # optional env file for systemd

~/.local/share/nightshift/
  nightshift.db        # SQLite — primary state
  nightshift.pid       # PID file (ephemeral)
  logs/                # daily JSONL logs
  audit/               # daily JSONL audit
  reports/             # per-run markdown
  summaries/           # daily markdown rollups
```

## What to Back Up

| File | Importance | Backup? |
|------|-----------|---------|
| `config.yaml` | High | Yes |
| `nightshift.db` | Medium | Optional — rebuilt over time |
| `audit/` | High if you care about audit trail | Yes |
| `reports/`, `summaries/` | Low | Optional |
| `logs/` | Low | No — rotate instead |
| `nightshift.pid` | None | No |

## Database

SQLite at `~/.local/share/nightshift/nightshift.db`. Pure Go (`modernc.org/sqlite`), no CGO.

**Live backup (safe while daemon runs):**

```bash
sqlite3 ~/.local/share/nightshift/nightshift.db \
  ".backup '/path/to/backup-$(date +%F).db'"
```

**Tables:**

| Table | Purpose |
|-------|---------|
| `projects` | Registered project paths |
| `task_history` | Per-project task run records |
| `assigned_tasks` | Current assignments |
| `run_history` | Historical runs (tokens, provider, branch) |
| `snapshots` | Provider usage snapshots |
| `bus_factor_results` | Bus-factor analysis output |
| `jira_ticket_results` | Jira pipeline ticket outcomes |

Schema migrations are versioned and applied automatically on `db.Open()`. See [Database](../dev/database.md) for the migration system.

## Restore

Stop the daemon first, then drop in the backup:

```bash
nightshift daemon stop
cp /path/to/backup.db ~/.local/share/nightshift/nightshift.db
nightshift daemon start
```

## Migrating Between Machines

```bash
# Old machine
nightshift daemon stop
tar czf nightshift-data.tar.gz \
  -C ~ .config/nightshift .local/share/nightshift

# New machine
tar xzf nightshift-data.tar.gz -C ~
nightshift daemon start
```

API keys (env vars) are not in the tarball — set them separately.

## Pruning Old Data

```bash
# Old reports + summaries (managed by `reporting.retention_days`)
# Old logs (rotate manually)
find ~/.local/share/nightshift/logs -mtime +30 -delete

# Old snapshots (truncate in-place)
sqlite3 ~/.local/share/nightshift/nightshift.db \
  "DELETE FROM snapshots WHERE created_at < datetime('now','-90 days'); VACUUM;"
```
