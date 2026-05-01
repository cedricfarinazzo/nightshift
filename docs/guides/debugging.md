# Debugging Guide

How to diagnose issues with Nightshift runs, the Jira pipeline, and agent execution.

---

## Quick Checks

```bash
# Verify environment: binaries, credentials, config
nightshift doctor

# Dry-run without executing anything
nightshift preview
nightshift jira preview

# Show recent run history
nightshift stats

# View today's log
nightshift logs
```

---

## Log Locations

| Log | Path |
|-----|------|
| Application log | `~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log` |
| Audit log | `~/.local/share/nightshift/audit/audit-YYYY-MM-DD.jsonl` |
| Run reports | `~/.local/share/nightshift/reports/run-YYYY-MM-DD-HHMMSS.md` |
| Daily summaries | `~/.local/share/nightshift/summaries/summary-YYYY-MM-DD.md` |
| Database | `~/.local/share/nightshift/nightshift.db` |

```bash
# Tail the application log
tail -f ~/.local/share/nightshift/logs/nightshift-$(date +%Y-%m-%d).log

# Read the audit log (JSONL, pipe to jq)
cat ~/.local/share/nightshift/audit/audit-$(date +%Y-%m-%d).jsonl | jq .

# List run reports
ls -lt ~/.local/share/nightshift/reports/ | head -20
```

---

## Log Levels

Set `LOG_LEVEL` or `logging.level` in config:

```yaml
logging:
  level: debug    # trace | debug | info | warn | error
```

`debug` logs agent prompts and raw output. `trace` logs individual DB queries.

For a single run:

```bash
LOG_LEVEL=debug nightshift run
LOG_LEVEL=debug nightshift jira run --ticket PROJ-42
```

---

## Common Problems

### "agent not available"

```
agent "claude" not available: exec: "claude": executable file not found in $PATH
```

The CLI binary is not installed or not in `PATH`. See [Agent Integrations](../website/docs/agents.md) for install instructions. Verify:

```bash
which claude
which codex
gh extension list | grep copilot
```

### Budget exhausted — run skipped

```
budget check: 0 tokens available (used 100%)
```

Check budget state:

```bash
nightshift budget
```

Possible causes: budget exhausted for today (resets weekly), `max_percent` too low, wrong `mode` (daily vs weekly). Check `~/.config/nightshift/config.yaml`.

### Push rejected on Jira feature branch

```
! [rejected] feature/PROJ-42 -> feature/PROJ-42 (fetch first)
```

The remote branch has commits that the workspace doesn't have. On the next run, `SetupWorkspace` will `git pull --rebase` to sync. If the problem persists:

```bash
cd ~/.local/share/nightshift/jira-workspaces/PROJ-42/myrepo
git fetch origin
git rebase origin/feature/PROJ-42
```

### Jira ticket stuck "In Progress" after error

The implement phase failed mid-run before the status transition. On the next run, `detectResumeState` reads the `🤖` comments and resumes from the commit phase. If the workspace is corrupted:

```bash
# Force restart by moving the ticket back to "To Do" in Jira,
# or delete the stale workspace:
rm -rf ~/.local/share/nightshift/jira-workspaces/PROJ-42/
```

### Claude/Codex asking for approval mid-run

- **Claude**: set `dangerously_skip_permissions: true` in config
- **Codex**: `--dangerously-bypass-approvals-and-sandbox` is on by default; check that `dangerously_bypass_approvals_and_sandbox` is not `false` in config

### HTTPS remote fails silently

```
fatal: could not read Username for 'https://github.com'
```

Jira repo URLs must use SSH, not HTTPS:

```yaml
jira:
  repos:
    - url: "git@github.com:org/repo.git"   # ✓ SSH
    # url: "https://github.com/org/repo"   # ✗ HTTPS fails non-interactively
```

### Validation always fails / score too low

Run with `--skip-validation` to bypass LLM validation for testing:

```bash
nightshift jira run --ticket PROJ-42 --skip-validation
```

Or add more detail to the ticket description and acceptance criteria.

### Wrong PR matched (closed PR)

`findExistingPR` uses `--state open` to avoid matching closed PRs on the same branch. If you see a closed PR being edited, ensure the `gh` CLI is up to date:

```bash
gh extension upgrade copilot
gh version
```

---

## Inspecting the Database

```bash
# Open SQLite shell
sqlite3 ~/.local/share/nightshift/nightshift.db

# Recent runs
SELECT started_at, project_path, task_type, success, provider
FROM run_history
ORDER BY started_at DESC LIMIT 20;

# Budget snapshots
SELECT provider, tokens_used, captured_at
FROM snapshots
ORDER BY captured_at DESC LIMIT 10;

# Task history (for staleness debugging)
SELECT project_path, task_type, last_run FROM task_history;
```

---

## Debugging Agent Prompts

Set `LOG_LEVEL=debug` — the application log will contain the full prompt sent to each agent and the raw output returned.

For the Jira pipeline specifically, add `--dry-run` or check the `🤖` comments on the ticket after each phase to see what was posted.

---

## Running a Single Task Manually

```bash
# Execute a specific task against a project
nightshift task run lint-fix --project /path/to/repo --provider claude --dry-run

# With debug logging
LOG_LEVEL=debug nightshift task run dead-code --project /path/to/repo
```

---

## Reporting a Bug

Before filing an issue:
1. Run `nightshift doctor` — capture the output
2. Check the log for the relevant run: `nightshift logs`
3. Run with `LOG_LEVEL=debug` to reproduce
4. Check `CLAUDE.md` Gotchas section — the issue may already be documented
