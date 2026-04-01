# Nightshift — Agent Guide

## Description

Nightshift is a CLI tool that orchestrates AI coding agents (Claude Code, Codex, GitHub Copilot) to run tasks overnight. It manages token budgets, schedules runs, coordinates parallel agent execution, and generates pull-request-based reports. It finds issues you forgot to look for.

Repo: https://github.com/cedricfarinazzo/nightshift

---

## Stack

- **Language**: Go 1.24
- **CLI framework**: `spf13/cobra` (commands) + `spf13/viper` (config)
- **TUI**: `charmbracelet/bubbletea` + `bubbles` + `lipgloss`
- **Database**: `modernc.org/sqlite` (pure Go, no CGO required)
- **Scheduling**: `robfig/cron/v3`
- **Logging**: `rs/zerolog`
- **Config format**: YAML
- **Build/release**: `Makefile` + `goreleaser`
- **Docs site**: Docusaurus v3 (`website/`)
- **CI**: GitHub Actions (`.github/workflows/`)

---

## Project Structure

```
cmd/
  nightshift/           # Main CLI binary (cobra root)
    main.go             # Entry point; calls Execute()
    commands/
      root.go           # Root cobra command; version = 0.3.4; global flags
      run.go            # `nightshift run` — start a scheduled/manual agent run
      preview.go        # `nightshift preview` — dry-run: show what would run + cost
      preview_output.go # TUI rendering for preview command
      budget.go         # `nightshift budget` — show token budget usage
      budget_helpers.go # Budget display formatting helpers
      task.go           # `nightshift task` — list/add/remove tasks
      stats.go          # `nightshift stats` — historical run stats
      status.go         # `nightshift status` — current run status
      report.go         # `nightshift report` — generate/show run reports
      snapshot.go       # `nightshift snapshot` — manage usage snapshots
      daemon.go         # `nightshift daemon` — run as background scheduler
      setup.go          # `nightshift setup` — interactive onboarding wizard
      config.go         # `nightshift config` — show/edit config
      logs.go           # `nightshift logs` — view log files
      init.go           # `nightshift init` — init config in new project
      install.go        # `nightshift install` — install pre-commit hooks etc.
      doctor.go         # `nightshift doctor` — check env, credentials, config
      busfactor.go      # `nightshift busfactor` — run bus-factor analysis
      helpers.go        # Shared command helpers (output formatting, error display)
      time_parse.go     # Duration/time string parsing for CLI flags
      run_output.go     # TUI rendering for run command
      run_reporting.go  # Report writing during/after runs
  provider-calibration/ # Standalone utility: compare Claude/Codex session costs

internal/
  agents/               # AI agent execution layer (spawns external CLI binaries)
    agent.go            # Agent interface + ExecuteOptions/ExecuteResult types; DefaultTimeout = 30min
    claude.go           # ClaudeAgent: spawns `claude` CLI; CommandRunner interface for testability
    codex.go            # CodexAgent: spawns `codex` CLI; supports --dangerously-bypass-approvals
    copilot.go          # CopilotAgent: spawns `gh copilot` or standalone `copilot`; use --no-ask-user --silent for non-interactive

  analysis/             # Bus-factor / code ownership analysis
    analyzer.go         # GitParser: extracts commit authors from git history
    metrics.go          # OwnershipMetrics: Herfindahl index, Gini coefficient, bus-factor count, risk level
    report.go           # ReportGenerator: formats analysis results into Report structs
    db.go               # Persists analysis results to SQLite

  budget/               # Token budget calculation and allocation
    budget.go           # Core budget logic: daily/weekly modes, reserve, aggressive end-of-week;
                        # interfaces ClaudeUsageProvider, CodexUsageProvider

  calibrator/           # Infers subscription budget from historical snapshots
    calibrator.go       # Calibrator struct: uses DB snapshots to infer budget with confidence score

  config/               # YAML config loading and validation
    config.go           # Config struct (Schedule, Budget, Providers, Projects, Tasks, Integrations,
                        # Logging, Reporting); uses viper; env var overrides supported

  db/                   # SQLite persistence layer — all SQL lives here, nowhere else
    db.go               # DB struct; DefaultPath = ~/.local/share/nightshift/nightshift.db;
                        # Open() applies pragmas + auto-runs migrations
    migrations.go       # Versioned schema migrations; auto-applied on Open()
    import.go           # Bulk data import utilities

  integrations/         # Readers for external config and task sources
    integrations.go     # Reader interface + Result/TaskItem/Hint types
    claudemd.go         # ClaudeMDReader: reads CLAUDE.md from project root; enabled via cfg.Integrations.ClaudeMD
    agentsmd.go         # AgentsMDReader: reads AGENTS.md from project root (legacy); returns nil if file absent
    td.go               # TDReader: integrates with `td` CLI for task sourcing; enabled via config
    github.go           # GitHub integration reader

  logging/              # zerolog setup
    logging.go          # Logger init; log files → ~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log

  orchestrator/         # Coordinates agent execution (plan-implement-review loop)
    orchestrator.go     # Orchestrator: task lifecycle; statuses: pending→planning→executing→reviewing
                        # →completed/failed/abandoned; DefaultMaxIterations=3
    events.go           # Event types emitted during orchestration (consumed by TUI and logging)

  projects/             # Project discovery and management
    projects.go         # Scans configured paths; returns ProjectConfig list

  providers/            # AI provider API backends (distinct from agents: providers track usage/cost)
    provider.go         # Provider interface: Name(), Execute(), Cost() (inputCents, outputCents per 1K tokens)
    claude.go           # Claude provider: usage/cost tracking via Anthropic API
    codex.go            # Codex provider: usage/cost tracking via OpenAI API
    copilot.go          # Copilot provider: usage/cost tracking

  reporting/            # Report generation
    run_report.go       # Per-run report → ~/.local/share/nightshift/reports/run-YYYY-MM-DD-HHMMSS.md
    run_results.go      # RunResult types and aggregation
    summary.go          # Daily summary → ~/.local/share/nightshift/summaries/summary-YYYY-MM-DD.md

  scheduler/            # Cron-based scheduling
    scheduler.go        # Wraps robfig/cron; reads schedule config; triggers runs

  security/             # Credential management — env vars only, never config files
    credentials.go      # CredentialManager: validates ANTHROPIC_API_KEY, OPENAI_API_KEY;
                        # masks values for display; never stores secrets
    audit.go            # Security audit checks
    sandbox.go          # Sandbox restrictions for agent execution
    security.go         # Top-level security coordination

  setup/                # Interactive onboarding
    presets.go          # Preset configurations for setup wizard

  snapshots/            # Token usage snapshot storage
    collector.go        # Collects usage snapshots from providers; persists to DB

  state/                # Run state tracking (persistence + concurrency-safe)
    state.go            # State struct (sync.RWMutex + DB); RunRecord type;
                        # tracks run history per project+task

  stats/                # Performance metrics
    stats.go            # Aggregates historical run data for display

  tasks/                # Task registry and queue
    tasks.go            # TaskDefinition: Type, Category, Name, Description,
                        # CostTier (Low/Medium/High/VeryHigh), RiskLevel, Interval;
                        # 6 categories: PR, Analysis, Options, Safe, Map, Emergency
    register.go         # RegisterCustomTasksFromConfig(): config → TaskDefinition; rolls back on failure
    selector.go         # Task selection logic (budget-aware, staleness-aware)

  tmux/                 # Tmux session scraping
    tmux.go             # Tmux session detection
    scraper.go          # Scrapes tmux pane output for agent context

  trends/               # Historical trend analysis
    analyzer.go         # Analyzes run history for trends and anomalies

docs/                   # Internal developer docs (NOT user-facing)
  guides/               # Implementation guides: run-lifecycle, adding-tasks, agent-tmux-integration, etc.
  implemented/          # Design docs for completed features
  deprecated/           # Archived docs

website/                # Docusaurus v3 user-facing documentation site
  docs/                 # 11 user guides: installation, config, cli-reference, budget, scheduling,
                        # tasks, troubleshooting, etc.
  package.json          # Node.js deps; deployed to https://nightshift.haplab.com

scripts/
  pre-commit.sh         # Runs gofmt, go vet, go build on staged .go files

.claude/skills/
  nightshift-release/   # Claude Code skill: cut a release (goreleaser, git tag, GH Actions verify)

.goreleaser.yml         # Builds darwin/linux amd64+arm64; archives as tar.gz; auto-changelog
Makefile                # Targets: build, test, test-verbose, test-race, coverage, lint, clean,
                        # deps, check, install, install-hooks
go.mod                  # module github.com/marcus/nightshift; Go 1.24
CHANGELOG.md            # Version history
SECURITY_AUDIT.md       # Security findings
```

---

## Critical Integrations (Claude / Codex / Copilot)

- **CLAUDE.md** (this file) is read at runtime by `internal/integrations/claudemd.go` and injected as context into agent prompts. Keep it accurate and up to date.
- **AGENTS.md** legacy reader (`internal/integrations/agentsmd.go`) gracefully returns nil when the file is absent — no code change needed after removal.
- **Authentication** — credentials from env vars only:
  - `ANTHROPIC_API_KEY` — Claude
  - `OPENAI_API_KEY` — Codex
  - GitHub token — Copilot (via `gh auth`)
  - Never put secrets in config files or commit them.
- **Copilot non-interactive flags**: `--no-ask-user --silent`
- **Output paths**:
  - Logs: `~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log`
  - Run reports: `~/.local/share/nightshift/reports/run-YYYY-MM-DD-HHMMSS.md`
  - Daily summaries: `~/.local/share/nightshift/summaries/summary-YYYY-MM-DD.md`
  - Database: `~/.local/share/nightshift/nightshift.db`

---

## Development Setup

```bash
# Install pre-commit hooks (runs gofmt, go vet, go build on commit)
make install-hooks

# Build
make build           # go build -o nightshift ./cmd/nightshift

# Run tests
make test            # go test ./...
make test-race       # with race detection
make coverage        # with coverage report

# Lint (requires golangci-lint)
make lint

# Install binary to GOPATH/bin
make install

# Provider cost calibration
make calibrate-providers
```

---

## Conventions

Agents MUST follow these conventions:

- **Logging**: Hyper-concise messages via `rs/zerolog`. Include only what's needed, minimize words. Use structured fields, not string interpolation.
- **Style**: Standard Go (`gofmt`, `go vet`). Explicit over magic. No unexported globals.
- **Errors**: Always wrap with context (`fmt.Errorf("context: %w", err)`). Never swallow. Never `panic` in library code.
- **Tests**: Table-driven, in `_test.go` files alongside the code they test. Use `CommandRunner` interface pattern for testability of external commands.
- **No new files unless necessary**: Prefer editing existing files.
- **No speculative abstractions**: Only add complexity the current task requires.
- **No backwards-compat shims**: If something is unused, delete it completely.
- **Dependencies**: Always search online for latest stable version before adding. Prefer pure-Go packages (no CGO). SQLite via `modernc.org/sqlite` only.

---

## Git Conventions

Agents MUST follow these git conventions:

- Commits must be **atomic**: one logical change per commit.
- Commit messages follow **Conventional Commits**: `type(scope): summary`
  - Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `ci`
  - Example: `feat(budget): add per-provider daily cap enforcement`
- Include **Jira ticket ID** at end of commit body when applicable: `Refs: NS-123`
- **No merge commits** on feature branches — rebase onto main.
- **Never amend commits** — always create a new commit for follow-up changes.
- Branch naming: `type/short-description` (e.g. `feat/model-selection`, `fix/budget-overflow`)
- PRs must be small and focused; split large changes into sequential PRs.
- Never force-push to `main`.

---

## Go Conventions

Agents MUST follow these Go conventions:

- Use `internal/` packages; nothing in `internal/` is part of a public API.
- Interfaces belong in the package that *uses* them, not the package that implements them.
- `context.Context` is the first parameter for any function that does I/O or may block.
- Avoid `init()` functions; prefer explicit initialization in `main` or setup.
- Keep `cmd/` thin — all business logic lives in `internal/`.
- All SQL lives in `internal/db/` only; no raw SQL in other packages.
- Config access via `internal/config/` only; no direct `viper` calls outside that package.
- All credential access via `internal/security/credentials.go`.

---

## Self-Improvement Loop

Agents MUST follow these rules:

- **On every new learning or architectural insight**: update this file and the relevant doc in `docs/` immediately, in the same commit or PR as the code change.
- **Add gotchas as soon as discovered**: add them to the `## Gotchas` section below to avoid repeating the same investigation.
- **On adding a new package, module, or file**: add it to the Project Structure section above with a one-line description.
- **Before using any package**: search online for the latest stable version and docs; never assume a cached version is current.
- **When user-facing docs in `website/docs/` become stale**: update them in the same PR as the code change.
- **Security findings**: add to `SECURITY_AUDIT.md`.
- **Agents are encouraged to add notes, new sections, or any content they find useful** directly into this file at any time. If it's worth knowing, put it here. This file is meant to grow over time as institutional knowledge accumulates.

---

## Gotchas

- `modernc.org/sqlite` is pure Go — no CGO needed. Do not switch to `mattn/go-sqlite3`.
- Agent binaries (`claude`, `codex`, `gh`) must be in PATH. Always use the `CommandRunner` interface for testability; never call `exec.Command` directly in agent code.
- Credentials are **env-var only** — `CredentialManager` never reads from config files or disk.
- `internal/integrations/agentsmd.go` still exists and looks for `AGENTS.md` at runtime. It returns `nil` if the file is absent — this is intentional and not an error.
