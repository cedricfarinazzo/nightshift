# Configuration

YAML config files. `nightshift setup` builds one interactively; you can also edit by hand.

## Locations

- **Global**: `~/.config/nightshift/config.yaml`
- **Per-project**: `nightshift.yaml` or `.nightshift.yaml` in repo root
- **Env overrides**: any field overridable via `NIGHTSHIFT_*` env var (viper conventions)

## Minimal Example

```yaml
schedule:
  cron: "0 2 * * *"

budget:
  max_percent: 90

providers:
  preference: [claude, codex]
  claude:
    enabled: true
    dangerously_skip_permissions: true
  codex:
    enabled: true
    dangerously_bypass_approvals_and_sandbox: true

projects:
  - path: ~/code/myrepo
```

## Schedule

```yaml
schedule:
  cron: "0 2 * * *"          # cron expression
  # interval: "8h"           # OR fixed interval (mutually exclusive)
```

## Budget

```yaml
budget:
  max_percent: 90            # stop running once provider usage exceeds this
```

The single knob. Live API checks gate every run. See [Budget](budget.md) for the formula.

## Providers

```yaml
providers:
  preference: [claude, codex, copilot]
  claude:
    enabled: true
    data_path: "~/.claude"
    model: "claude-sonnet-4-6"        # optional: override default model
    reasoning_effort: "medium"        # optional: low/medium/high/xhigh/max
    timeout: "45m"                    # optional: agent execution timeout (default: 30m)
    dangerously_skip_permissions: true
    dangerously_bypass_approvals_and_sandbox: true
  codex:
    enabled: true
    data_path: "~/.codex"
    timeout: "45m"                    # optional: agent execution timeout (default: 30m)
    dangerously_bypass_approvals_and_sandbox: true
  copilot:
    enabled: true
    binary_path: "copilot"           # optional: standalone copilot binary
    timeout: "45m"                   # optional: agent execution timeout (default: 30m)
    dangerously_skip_permissions: true
```

Providers are tried in `preference` order; first one with budget wins.

### `timeout` field

Duration string (e.g. `"30m"`, `"1h"`, `"90m"`). Sets the maximum time the agent CLI process is allowed to run per task. When omitted, the built-in default of 30 minutes applies. Useful for:

- **Longer tasks** (large refactors, multi-file reviews): set `"60m"` or `"90m"`.
- **Budget conservation**: set `"15m"` to kill stalled agents early and save tokens.
- **Per-provider tuning**: different timeouts per provider (Claude vs Codex run differently).

The effective timeout shown in `nightshift preview` output reflects this config value.

Per-phase timeouts for `nightshift jira run` are configured separately under `jira.validation.timeout`, `jira.plan.timeout`, `jira.implement.timeout`, and `jira.review_fix.timeout`.

## Projects

```yaml
projects:
  - path: ~/code/proj1
    priority: 1
    tasks: [lint-fix, docs-backfill]
  - path: ~/code/proj2
  - pattern: ~/code/oss/*
    exclude: [~/code/oss/archived]
```

## Tasks

```yaml
tasks:
  enabled:   [lint-fix, docs-backfill, bug-finder]
  disabled:  [skill-groom]
  priorities:
    lint-fix: 1
    bug-finder: 2
  intervals:
    lint-fix: 24h
    docs-backfill: 168h
```

`enabled`/`disabled` override the built-in catalog. `priorities` and `intervals` tune the selector. See [Tasks](tasks.md).

## Prompt Compression

```yaml
prompt_compression:
  enabled: true
  provider: copilot
  model: gpt-5.4-mini
  reasoning_effort: low
  threshold_chars: 40000
```

When the compressible portion of a prompt exceeds `threshold_chars`, Nightshift first runs it through the configured agent for compression, then sends the compressed result to the main agent. Critical instructions live in `PromptSuffix` and are never compressed.

## Integrations

```yaml
integrations:
  claude_md: true            # inject CLAUDE.md from project root
  github_issues:
    enabled: true
    label: nightshift
```

## Logging

```yaml
logging:
  level: info                # debug | info | warn | error
  format: json               # json | text
```

Log file: `~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log`.

## Reporting

```yaml
reporting:
  retention_days: 30         # delete reports older than this
```

## Jira

See [Jira Pipeline](jira-pipeline.md) for the full Jira config block.

## File Locations

| What | Path |
|------|------|
| Config | `~/.config/nightshift/config.yaml` |
| Database | `~/.local/share/nightshift/nightshift.db` |
| Logs | `~/.local/share/nightshift/logs/` |
| Audit log | `~/.local/share/nightshift/audit/` |
| Run reports | `~/.local/share/nightshift/reports/` |
| Daily summaries | `~/.local/share/nightshift/summaries/` |
| PID file | `~/.local/share/nightshift/nightshift.pid` |
