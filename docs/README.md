# Nightshift Documentation

Plain-markdown docs for Nightshift. Organised into three tracks.

## User Guides

For people running Nightshift on their own machine.

**Start here:**

- [Introduction](user/introduction.md) — what Nightshift is and the core ideas
- [Installation](user/installation.md) — Homebrew, binaries, source builds
- [Quick Start](user/quick-start.md) — 2-minute walkthrough (Jira flavour + tasks flavour)

**Jira pipeline (the headline workflow):**

- [Jira Pipeline](user/jira-pipeline.md) — full lifecycle, config, resume logic, failure modes
- [Agents](user/agents.md) — per-phase provider/model selection
- [CLI Reference](user/cli-reference.md) — `nightshift jira run` / `preview` flags

**General:**

- [Configuration](user/configuration.md) — YAML reference
- [Tasks](user/tasks.md) — maintenance task catalog
- [Budget](user/budget.md) — how budget enforcement works
- [Scheduling](user/scheduling.md) — systemd timer setup
- [Bus Factor](user/bus-factor.md) — code ownership analysis
- [Troubleshooting](user/troubleshooting.md) — common errors

## Operations Guides

For running Nightshift as a long-lived service.

- [Systemd Install](operations/systemd-install.md) — install timer+oneshot unit, autostart
- [Kubernetes CronJob](operations/kubernetes-cronjob.md) — deploy as a K8s CronJob with Kustomize manifests
- [Logs & Reports](operations/logs-and-reports.md) — where output lives, retention
- [Data & Backup](operations/data-and-backup.md) — DB layout, what to back up
- [Security Model](operations/security.md) — credentials, audit log, sandbox
- [Release Process](operations/release.md) — cutting a new version

## Developer / Internal Guides

For people hacking on the Nightshift codebase.

- [Architecture](dev/architecture.md) — package map, layers, key constraints

**Jira pipeline internals:**

- [Jira Pipeline Internals](dev/jira-pipeline.md) — phase state machine, resume logic, dependency graph
- [Workspace Management](dev/workspace.md) — clone/branch/push conventions
- [Agents Internals](dev/agents-internals.md) — provider wrappers used by every Jira phase

**General internals:**

- [Run Lifecycle](dev/run-lifecycle.md) — end-to-end task run flow
- [Task Orchestrator](dev/orchestrator.md) — plan→implement→review loop
- [Database](dev/database.md) — SQLite schema, migrations
- [Budget Internals](dev/budget-internals.md) — capacity formula, provider APIs
- [Tasks Internals](dev/tasks-internals.md) — `TaskDefinition`, selector scoring
- [State & Snapshots](dev/state-and-snapshots.md) — RunRecord, staleness, snapshots
- [Scheduling Internals](dev/scheduling-internals.md) — systemd timer model, no in-process scheduler
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
