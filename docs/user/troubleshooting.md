# Troubleshooting

## `nightshift doctor` is the first step

Run it. It checks PATH, env vars, config, agent binaries, and credentials.

## Agent binary not found

```
agent "claude" not available: exec: "claude": executable file not found in $PATH
```

Install the agent CLI (`@anthropic-ai/claude-code`, `@openai/codex`, `gh extension install github/gh-copilot`). Verify with `which claude` / `which codex` / `gh copilot --help`.

## Permission prompts halt the run mid-flight

The agent paused asking for approval. Fix:

```yaml
providers:
  claude:
    dangerously_skip_permissions: true
    dangerously_bypass_approvals_and_sandbox: true
  codex:
    dangerously_bypass_approvals_and_sandbox: true
  copilot:
    dangerously_skip_permissions: true
```

For Jira implement phase these are mandatory.

## Authentication expired

```bash
claude /login          # Claude
codex --login          # Codex
gh auth login          # Copilot
```

## Budget always says zero / run skips

- Check the provider's Usage API directly (Anthropic / OpenAI console).
- `nightshift budget --provider claude` to inspect raw windows.
- `nightshift run --ignore-budget` to bypass for testing.

## Daemon won't start: stale PID file

```
PID file exists: ~/.local/share/nightshift/nightshift.pid
```

The daemon detects + removes stale PID files automatically. If it doesn't, delete manually then retry:

```bash
rm ~/.local/share/nightshift/nightshift.pid
nightshift daemon start
```

## Jira clone fails: `could not read Username`

You're using an HTTPS remote in a non-interactive context. Switch to SSH:

```yaml
jira:
  projects:
    - repos:
        - url: git@github.com:org/repo.git    # not https://
```

## Jira ticket stuck in "In Progress"

Pipeline failed before reaching the PR phase, so `TransitionToReview` never ran. Check logs. Either:

- Move the ticket back to TODO to force a full restart, or
- Fix the underlying error and re-run (resume will skip completed phases)

## `nightshift jira preview` invocation

```bash
go run ./cmd/nightshift jira preview --plain    # args after pkg passed to program
# OR
go run ./cmd/nightshift -- jira preview --plain
```

## Wrong PR target / closed PR getting reused

`findExistingPR` filters by `--state open`. If you're still hitting this, check there's no upstream alias rewriting `gh` flags.

## Run reports missing

```bash
ls ~/.local/share/nightshift/reports/
nightshift report list
```

If empty, either no run completed (`nightshift status`), or `reporting.retention_days` is too low.

## Logs

```
~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log
```

JSON-line format. Query with `jq`:

```bash
jq 'select(.level == "error")' ~/.local/share/nightshift/logs/*.log
```

## Where to file bugs

[github.com/cedricfarinazzo/nightshift/issues](https://github.com/cedricfarinazzo/nightshift/issues)

Include: `nightshift --version`, OS, agent binary versions, redacted config, relevant log excerpt.
