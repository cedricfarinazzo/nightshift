# Debugging

## First steps

```bash
nightshift doctor                              # env, paths, agent binaries, timer
systemctl --user list-timers nightshift.timer  # next fire?
tail -F ~/.local/share/nightshift/logs/*.log | jq .
```

## Verbose mode

```bash
nightshift --verbose run
```

## Log locations

| What | Path |
|------|------|
| App | `~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log` |
| Audit | `~/.local/share/nightshift/audit/audit-YYYY-MM-DD.jsonl` |
| Run reports | `~/.local/share/nightshift/reports/run-*.md` |
| Systemd service | `journalctl --user -u nightshift.service` |

## Common errors

### `agent "claude" not available`

Binary missing from `$PATH`. `which claude`. If using systemd, the service `$PATH` is minimal — add a drop-in:

```ini
[Service]
Environment="PATH=/usr/local/bin:/usr/bin:%h/.local/bin"
```

### `failed to validate workdir: not a git repo`

`orchestrator.RunTask` refuses non-git workdirs. Either:

- Configure project paths that point at git repos
- Don't pass `$HOME` as a project path

### `could not read Username` (Jira clone)

HTTPS remote in a non-interactive context. Switch to SSH (`git@github.com:org/repo.git`).

### `gh: not found` / Copilot fails

`gh` CLI missing or Copilot extension not installed:

```bash
brew install gh
gh extension install github/gh-copilot
gh auth login
```

### Validation always scores 0

Wrong model for the validation phase. Validation expects JSON output; older models or wrong model names can produce free-form text. Check:

```yaml
jira:
  validation:
    provider: copilot
    model: gpt-5.4-mini
```

### Implement phase produces no changes

Permission prompts halted the agent mid-run. Set:

```yaml
providers:
  claude:
    dangerously_skip_permissions: true
    dangerously_bypass_approvals_and_sandbox: true
```

(Or equivalents for codex/copilot.)

### Run skipped: `budget exhausted`

Check actual usage with `nightshift budget --provider <name>`. If you believe the API is wrong, bypass once with `--ignore-budget` and verify it's a sustained issue before lowering `max_percent`.

### PR created on wrong branch

`findExistingPR` is supposed to filter `--state open`. If a closed PR is being reused, check that `internal/jira/pr.go` still passes `--state open` to `gh pr list`.

### Resume restarts from the beginning

`detectResumeState` requires `🤖` prefix + `<!-- nightshift:type=... -->` comment markers. If comments are missing or you posted from a non-Nightshift account without the prefix, resume defaults to phase 1.

To force a clean slate intentionally: move the ticket back to TODO.

### Compression strips instructions

You put instructions in `Prompt` instead of `PromptSuffix`. Move them. `Prompt` is compressed; `PromptPrefix`+`PromptSuffix` are not.

## Database inspection

```bash
sqlite3 ~/.local/share/nightshift/nightshift.db
> .tables
> SELECT * FROM run_history ORDER BY created_at DESC LIMIT 10;
> SELECT * FROM snapshots ORDER BY created_at DESC LIMIT 5;
> SELECT * FROM jira_ticket_results ORDER BY created_at DESC LIMIT 10;
```

## Reproducing a run

```bash
nightshift run --project ~/code/myrepo --task lint-fix --dry-run
nightshift task show lint-fix --prompt-only > /tmp/prompt.md
nightshift task run lint-fix --provider claude --dry-run
```

`--dry-run` builds the prompt without spawning the agent — useful for prompt debugging.

## Capturing an agent invocation

Set the relevant logger to debug:

```yaml
logging:
  level: debug
```

The agents package logs the temp-file path + full argv. Reproduce manually:

```bash
claude --print --model claude-sonnet-4-5 "Read and follow the task instructions in file: /tmp/nightshift-prompt-XXXX.md"
```

## Race / deadlock

```bash
make test-race
```

If it hits a real production race:

```bash
GORACE=halt_on_error=1 go test -race ./internal/<pkg>/...
```
