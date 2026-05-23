# CLI Reference

All commands. Run any with `--help` for full flag list.

## Top-level

| Command | Purpose |
|---------|---------|
| `nightshift setup` | Interactive setup wizard |
| `nightshift init` | Drop a starter config in current project |
| `nightshift install` | Install systemd unit / pre-commit hooks |
| `nightshift uninstall` | Remove systemd unit |
| `nightshift doctor` | Check env, credentials, config, agent binaries |
| `nightshift run` | Execute one run now |
| `nightshift preview` | Show upcoming runs without executing |
| `nightshift budget` | Show budget status |
| `nightshift task` | List / show / run tasks |
| `nightshift status` | Current + recent run state |
| `nightshift stats` | Historical aggregates |
| `nightshift report` | Browse / show run reports |
| `nightshift logs` | Stream / tail / export logs |
| `nightshift config` | Show / get / set / validate config |
| `nightshift daemon` | Start / stop / status the scheduler |
| `nightshift busfactor` | Bus-factor analysis on a repo |
| `nightshift jira` | Jira autonomous pipeline (subcommands) |

## `nightshift run`

| Flag | Default | What |
|------|---------|------|
| `--dry-run` | `false` | Preflight only, no execution |
| `--yes`, `-y` | `false` | Skip confirmation |
| `--project`, `-p` | _(all)_ | Target one project directory |
| `--task`, `-t` | _(auto)_ | Run a specific task |
| `--max-projects` | `1` | Project cap (ignored with `--project`) |
| `--max-tasks` | `1` | Task cap per project (ignored with `--task`) |
| `--random-task` | `false` | Pick a random eligible task |
| `--ignore-budget` | `false` | Bypass budget check (warns) |
| `--provider` | _(auto)_ | Force a provider |
| `--timeout` | `30m` | Per-task timeout |

Non-TTY runs (cron/daemon/pipe) auto-skip the confirm prompt.

## `nightshift preview`

| Flag | What |
|------|------|
| `-n N` | Number of upcoming runs to show |
| `--long` | Detailed view |
| `--explain` | Include prompt previews |
| `--plain` | No pager / TUI |
| `--json` | Machine output |
| `--write DIR` | Dump prompts to files in DIR |

## `nightshift task`

```bash
nightshift task list
nightshift task list --category pr
nightshift task list --cost low --json
nightshift task show <name>
nightshift task show <name> --prompt-only
nightshift task run <name> --provider claude
nightshift task run <name> --provider codex --dry-run
```

Filters: `--category` (pr/analysis/options/safe/map/emergency), `--cost` (low/medium/high/veryhigh).

## `nightshift budget`

```bash
nightshift budget                       # current capacity per provider
nightshift budget --provider claude
nightshift budget snapshot --local-only
nightshift budget history -n 10
```

## `nightshift daemon`

```bash
nightshift daemon start
nightshift daemon start --foreground    # don't fork; useful for debug
nightshift daemon stop
nightshift daemon status
```

PID file at `~/.local/share/nightshift/nightshift.pid` (atomic — only one daemon can hold it).

## `nightshift jira`

```bash
nightshift jira run                          # process all labeled tickets
nightshift jira run --project VC             # only this project (mirrors jira preview)
nightshift jira run -p VC                    # short form
nightshift jira run --ticket VC-42           # one ticket
nightshift jira run --max-tickets 5
nightshift jira run --skip-validation
nightshift jira run --todo-only
nightshift jira run --review-only
nightshift jira run --label other-label
nightshift jira run --wait 5m               # wait up to 5m for a running instance to finish

nightshift jira preview                      # dry-run summary
nightshift jira preview --validate           # also run LLM validation
nightshift jira preview --explain
nightshift jira preview --project VC
nightshift jira preview --type Bug
nightshift jira preview --json
nightshift jira preview --plain
```

See [Jira Pipeline](jira-pipeline.md) for the full lifecycle.

## `nightshift busfactor`

```bash
nightshift busfactor                          # all configured projects
nightshift busfactor --project ~/code/repo
nightshift busfactor --since 6m
nightshift busfactor --json
```

Outputs: HHI, Gini, contributor share, risk rating, suggested remediation.

## Global flags

| Flag | What |
|------|------|
| `--verbose` | More output |
| `--config FILE` | Override config path |
| `--no-color` | Disable ANSI colour |
