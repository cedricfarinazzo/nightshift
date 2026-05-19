# Nightshift Documentation

Plain-markdown docs for Nightshift. Organised into three tracks.

## User Guides

For people running Nightshift on their own machine.

- [Introduction](user/introduction.md) — what Nightshift is and the core ideas
- [Installation](user/installation.md) — Homebrew, binaries, source builds
- [Quick Start](user/quick-start.md) — 2-minute getting started
- [Configuration](user/configuration.md) — YAML reference
- [CLI Reference](user/cli-reference.md) — every command and flag
- [Tasks](user/tasks.md) — built-in tasks, categories, cost tiers
- [Agents](user/agents.md) — Claude / Codex / Copilot setup
- [Jira Pipeline](user/jira-pipeline.md) — autonomous ticket implementation
- [Budget](user/budget.md) — how budget enforcement works
- [Scheduling](user/scheduling.md) — cron vs interval
- [Bus Factor](user/bus-factor.md) — code ownership analysis
- [Troubleshooting](user/troubleshooting.md) — common errors

## Operations Guides

For running Nightshift as a long-lived service.

- [Daemon Mode](operations/daemon.md) — start/stop/status, PID files
- [Systemd Install](operations/systemd-install.md) — install unit, autostart
- [Logs & Reports](operations/logs-and-reports.md) — where output lives, retention
- [Data & Backup](operations/data-and-backup.md) — DB layout, what to back up
- [Security Model](operations/security.md) — credentials, audit log, sandbox
- [Release Process](operations/release.md) — cutting a new version

## Developer / Internal Guides

For people hacking on the Nightshift codebase.

- [Architecture](dev/architecture.md) — package map, layers, key constraints
- [Run Lifecycle](dev/run-lifecycle.md) — end-to-end task run flow
- [Task Orchestrator](dev/orchestrator.md) — plan→implement→review loop
- [Agents Internals](dev/agents-internals.md) — Claude/Codex/Copilot wrappers, compression
- [Jira Pipeline Internals](dev/jira-pipeline.md) — phase machine, resume logic
- [Workspace Management](dev/workspace.md) — clone, branch, push conventions
- [Database](dev/database.md) — SQLite schema, migrations
- [Budget Internals](dev/budget-internals.md) — capacity formula, provider APIs
- [Tasks Internals](dev/tasks-internals.md) — `TaskDefinition`, selector scoring
- [State & Snapshots](dev/state-and-snapshots.md) — RunRecord, staleness, snapshots
- [Scheduling Internals](dev/scheduling-internals.md) — cron wrapper, `SkipIfStillRunning`
- [Reporting](dev/reporting.md) — run reports, daily summaries
- [Logging](dev/logging.md) — zerolog setup, log queries
- [Bus Factor](dev/bus-factor.md) — HHI, Gini, risk levels
- [Testing](dev/testing.md) — test patterns, `CommandRunner`, mocks
- [Contributing](dev/contributing.md) — dev setup, git conventions, PR checklist
- [Debugging](dev/debugging.md) — log locations, common errors

## See Also

- Top-level [README](../README.md) — project overview
- [CLAUDE.md](../CLAUDE.md) — agent operating guide (also injected into agent prompts)
- [CHANGELOG.md](../CHANGELOG.md) — version history
