# Introduction

Nightshift is a Go CLI that runs AI coding agents (Claude Code, Codex, GitHub Copilot) on your repos overnight, using your remaining provider budget. It finds dead code, doc drift, test gaps, security issues, and 50+ other things silently accumulating while you ship features.

> It finds what you forgot to look for.

## Core Principles

- **Everything is a PR.** Nightshift never writes directly to your primary branch. Don't like a change? Close the PR.
- **Budget-aware.** Live provider Usage API checks gate every run. Default max: 90%.
- **Multi-project.** Point it at your repos; it already knows what to look for.
- **Autonomous Jira pipeline.** Validates, plans, codes, commits, PRs, transitions status — overnight.
- **No CGO, no cloud.** SQLite via `modernc.org/sqlite`, all state on disk.

## What It Does

1. Wakes up on schedule (or runs on demand).
2. Checks provider budget. Skips if exhausted.
3. Selects projects + tasks based on priority, staleness, and cost.
4. Spawns the configured agent (`claude`, `codex`, `gh copilot`).
5. Pushes a branch + opens a PR per change.
6. Writes a run report.

## Quick Glance

```bash
brew install marcus/tap/nightshift
nightshift setup
nightshift preview
nightshift run
```

## Next

- [Installation](installation.md)
- [Quick Start](quick-start.md)
