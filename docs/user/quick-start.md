# Quick Start

Get running in two minutes.

## 1. Install

```bash
brew install marcus/tap/nightshift
```

## 2. Setup

```bash
nightshift setup
```

Walks you through:
- providers + models
- budget cap (`max_percent`)
- schedule (cron or interval)
- target projects
- optional Jira pipeline
- optional systemd unit

Writes `~/.config/nightshift/config.yaml`.

## 3. Preview

See what would run, with budget breakdown:

```bash
nightshift preview
nightshift budget
```

## 4. Run

```bash
nightshift run                # interactive preflight + confirm
nightshift run --dry-run      # preflight only
nightshift run --yes          # skip confirm (cron-friendly)
```

## 5. Inspect

```bash
nightshift status --today
nightshift logs --tail
nightshift report list
```

Every change lands as a PR. Merge the keepers, close the rest.

## Next

- [Configuration](configuration.md) — full YAML reference
- [Tasks](tasks.md) — what's in the catalog
- [Jira Pipeline](jira-pipeline.md) — autonomous ticket implementation
