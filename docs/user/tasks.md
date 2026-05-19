# Tasks

Nightshift ships with a catalog of ~60 built-in tasks. Each task is a single LLM-driven operation: a planning prompt, an execution prompt, optional review, and a result.

## Listing

```bash
nightshift task list
nightshift task list --category pr
nightshift task list --cost low
nightshift task show lint-fix
```

## Categories

| Category | Purpose |
|----------|---------|
| `pr` | Produces a PR (lint, docs, tests, refactors) |
| `analysis` | Read-only investigation; writes a report |
| `options` | Generates multiple alternative approaches for you to pick from |
| `safe` | Low-risk hygiene tasks |
| `map` | Repo-wide indexing / mapping |
| `emergency` | Critical fixes (security, broken builds) |

## Cost Tiers

| Tier | Approx token cost |
|------|-------------------|
| `low` | < 5k tokens |
| `medium` | 5k–25k |
| `high` | 25k–100k |
| `veryhigh` | > 100k |

The selector weighs cost against remaining budget when picking tasks.

## Configuration

```yaml
tasks:
  enabled: [lint-fix, docs-backfill, bug-finder]
  disabled: [skill-groom]
  priorities:
    lint-fix: 1
    bug-finder: 2
  intervals:
    lint-fix: 24h
    docs-backfill: 168h
```

- `enabled`/`disabled` — turn tasks on/off
- `priorities` — lower number = higher priority
- `intervals` — minimum gap between repeated runs of the same task on the same project (cooldown)

Defaults exist for every task. `skill-groom` is enabled by default.

## Selection Algorithm

For each scheduled tick:

1. Filter to enabled tasks
2. Drop tasks that ran on this project within their interval window
3. Score by `priority + staleness + cost_fit`
4. Pick top N (`--max-tasks`)

Use `--random-task` to skip scoring and pick uniformly from eligible tasks.

## Custom Tasks

Define ad-hoc tasks in config:

```yaml
custom_tasks:
  - name: my-task
    category: pr
    cost: medium
    risk: low
    interval: 48h
    description: "Custom thing we want done"
    prompt: |
      Find all uses of X in this repo and ...
```

Registered at startup via `RegisterCustomTasksFromConfig`. Rolls back on failure.

## Running One-Off

```bash
nightshift task run lint-fix --provider claude
nightshift task run my-task --provider copilot --dry-run
```

`--dry-run` prints the assembled prompt without executing.

## See Also

- [Tasks Internals](../dev/tasks-internals.md) — `TaskDefinition`, selector formula, adding built-ins
