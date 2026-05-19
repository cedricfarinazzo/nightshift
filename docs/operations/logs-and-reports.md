# Logs & Reports

Where Nightshift writes things and how to read them.

## Paths

| Type | Path | Format |
|------|------|--------|
| App logs | `~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log` | JSONL |
| Audit log | `~/.local/share/nightshift/audit/audit-YYYY-MM-DD.jsonl` | JSONL |
| Run reports | `~/.local/share/nightshift/reports/run-YYYY-MM-DD-HHMMSS.md` | Markdown |
| Daily summaries | `~/.local/share/nightshift/summaries/summary-YYYY-MM-DD.md` | Markdown |

## Viewing

```bash
nightshift logs --tail              # tail today's log
nightshift logs --since 1h          # last hour
nightshift logs --level error
nightshift logs --json              # raw JSONL
nightshift report list
nightshift report show run-2026-05-19-020000
```

## Querying with jq

```bash
# All errors today
jq 'select(.level == "error")' \
   ~/.local/share/nightshift/logs/nightshift-$(date +%F).log

# Filter by component
jq 'select(.component == "orchestrator")' ~/.local/share/nightshift/logs/*.log

# Token cost summary by run
jq -s 'group_by(.run_id) | map({run: .[0].run_id, tokens: map(.tokens // 0) | add})' \
   ~/.local/share/nightshift/logs/*.log
```

## Log Format

zerolog JSONL:

```json
{"level":"info","time":"2026-05-19T02:00:01Z","component":"scheduler","msg":"fire"}
{"level":"info","time":"2026-05-19T02:00:01Z","component":"orchestrator","task":"lint-fix","project":"~/code/myrepo"}
{"level":"error","time":"2026-05-19T02:01:23Z","component":"agents","error":"timeout"}
```

## Audit Log

Append-only, 0700 permissions. Captures:

- `agent_start`, `agent_complete`, `agent_error`
- `file_read`, `file_write`, `file_delete`
- `git_commit`, `git_push`
- `security_check`, `security_denied`
- `config_change`, `budget_check`

Used for after-the-fact forensics. Never overwritten.

## Run Reports

One markdown file per run. Sections:

- Selected projects + tasks
- Budget before/after
- Per-task result (status, duration, tokens, branch/PR, error)
- Aggregate stats

## Daily Summary

Rolls up all runs in a day into one markdown summary.

## Retention

```yaml
reporting:
  retention_days: 30
```

Reports + summaries older than this are deleted on each daemon startup. Logs are NOT auto-pruned — rotate them with logrotate if needed.

## Disk Usage

```bash
du -sh ~/.local/share/nightshift/{logs,audit,reports,summaries,nightshift.db}
```
