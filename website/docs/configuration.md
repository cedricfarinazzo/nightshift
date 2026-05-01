---
sidebar_position: 4
title: Configuration
---

# Configuration

Nightshift uses YAML config files. Run `nightshift setup` for an interactive setup, or edit directly.

## Config Location

- **Global:** `~/.config/nightshift/config.yaml`
- **Per-project:** `nightshift.yaml` or `.nightshift.yaml` in the repo root

## Minimal Config

```yaml
schedule:
  cron: "0 2 * * *"

budget:
  mode: daily
  max_percent: 75
  reserve_percent: 5
  billing_mode: subscription
  calibrate_enabled: true
  snapshot_interval: 30m

providers:
  preference:
    - claude
    - codex
  claude:
    enabled: true
    data_path: "~/.claude"
    dangerously_skip_permissions: true
  codex:
    enabled: true
    data_path: "~/.codex"
    dangerously_bypass_approvals_and_sandbox: true

projects:
  - path: ~/code/sidecar
  - path: ~/code/td
```

## Schedule

Use cron syntax or interval-based scheduling:

```yaml
schedule:
  cron: "0 2 * * *"        # Every night at 2am
  # interval: "8h"         # Or run every 8 hours
```

## Budget

Control how much of your token budget Nightshift uses:

| Field | Default | Description |
|-------|---------|-------------|
| `mode` | `daily` | `daily` or `weekly` |
| `max_percent` | `75` | Max budget % to use per run |
| `reserve_percent` | `5` | Always keep this % available |
| `billing_mode` | `subscription` | `subscription` or `api` |
| `calibrate_enabled` | `true` | Auto-calibrate from local CLI data |

## Task Selection

Enable/disable tasks and set priorities:

```yaml
tasks:
  enabled:
    - lint-fix
    - docs-backfill
    - bug-finder
  priorities:
    lint-fix: 1
    bug-finder: 2
  intervals:
    lint-fix: "24h"
    docs-backfill: "168h"
```

Each task has a default cooldown interval to prevent the same task from running too frequently on a project.

## Multi-Project Setup

```yaml
projects:
  - path: ~/code/project1
    priority: 1                # Higher priority = processed first
    tasks:
      - lint
      - docs
  - path: ~/code/project2
    priority: 2

  # Or use glob patterns
  - pattern: ~/code/oss/*
    exclude:
      - ~/code/oss/archived
```

## Safe Defaults

| Feature | Default | Override |
|---------|---------|----------|
| Read-only first run | Yes | `--enable-writes` |
| Max budget per run | 75% | `budget.max_percent` |
| Auto-push to remote | No | Manual only |
| Reserve budget | 5% | `budget.reserve_percent` |

## File Locations

| Type | Location |
|------|----------|
| Run logs | `~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log` |
| Audit logs | `~/.local/share/nightshift/audit/audit-YYYY-MM-DD.jsonl` |
| Summaries | `~/.local/share/nightshift/summaries/` |
| Database | `~/.local/share/nightshift/nightshift.db` |
| PID file | `~/.local/share/nightshift/nightshift.pid` |

If `state/state.json` exists from older versions, Nightshift migrates it to the SQLite database and renames the file to `state.json.migrated`.

## Providers

Nightshift supports Claude Code, Codex, and GitHub Copilot as execution providers. It uses whichever has budget remaining, in the order specified by `preference`.

```yaml
providers:
  preference:
    - claude
    - codex
    - copilot
  claude:
    enabled: true
    data_path: "~/.claude"
    dangerously_skip_permissions: true
    dangerously_bypass_approvals_and_sandbox: true
  codex:
    enabled: true
    data_path: "~/.codex"
    dangerously_bypass_approvals_and_sandbox: true
  copilot:
    enabled: true
```

## Integrations

```yaml
integrations:
  claude_md: true           # Read CLAUDE.md from project root for context
  task_sources:
    - td:
        enabled: true
        teach_agent: true   # Include td workflow in prompts
  github_issues:
    enabled: true
    label: "nightshift"
```

## Logging

```yaml
logging:
  level: info               # debug | info | warn | error
  format: json              # json | text
```

## Jira Autonomous Pipeline

Configure the Jira pipeline to autonomously implement, commit, and PR Jira tickets:

```yaml
jira:
  site: "https://yourorg.atlassian.net"
  token: ""                 # Use NIGHTSHIFT_JIRA_TOKEN env var instead
  email: "you@example.com"
  project: "PROJ"
  label: "nightshift"       # Only tickets with this label are processed

  # AI agent phases
  validation:
    provider: copilot
    model: gpt-5.4-mini
    timeout: 2m
  plan:
    provider: copilot
    model: claude-sonnet-4.6
    timeout: 5m
  implement:
    provider: copilot
    model: claude-sonnet-4.6
    timeout: 30m
  review_fix:
    provider: copilot
    model: gpt-5.4-mini
    timeout: 20m

  # Git workspace settings
  workspace_root: "~/.local/share/nightshift/jira-workspaces"
  cleanup_after_days: 14

  # Repositories to operate on
  repos:
    - name: myrepo
      url: "git@github.com:org/myrepo.git"   # SSH URL required
      base_branch: main
      lint_command: "golangci-lint run ./..."
      test_command: "go test ./..."

  # Jira status names (auto-discovered; set explicitly if discovery fails)
  statuses:
    todo: "To Do"
    in_progress: "In Progress"
    review: "In Review"
    done: "Done"
    needs_info: "Needs Info"
```

### Jira environment variables

| Variable | Description |
|----------|-------------|
| `NIGHTSHIFT_JIRA_TOKEN` | Jira API token (preferred over config) |
| `ANTHROPIC_API_KEY` | Required for Claude provider |
| `OPENAI_API_KEY` | Required for Codex provider |

> **SSH required**: repos must use `git@github.com:...` URLs. HTTPS remotes fail silently in non-interactive contexts.
