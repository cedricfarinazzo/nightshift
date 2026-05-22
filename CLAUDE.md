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
- **Docs**: Plain markdown under `docs/` (no static-site generator)
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
      daemon.go         # `nightshift daemon` — run as background scheduler
      setup.go          # `nightshift setup` — interactive onboarding wizard; steps: schedule, budget,
                        # providers, model, effort, jira, compression (NEW), systemd, preview;
                        # compressionConfigFromApp() in helpers.go bridges config↔agents packages
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
      jira.go           # `nightshift jira` — Jira sub-command root
      jira_run.go       # `nightshift jira run` — autonomous Jira pipeline
      jira_preview.go   # `nightshift jira preview` — dry-run: shows tickets, deps, budget, phases
      jira_preview_output.go # TUI rendering for jira preview command

internal/
  agents/               # AI agent execution layer (spawns external CLI binaries)
    agent.go            # Agent interface + ExecuteOptions/ExecuteResult types; DefaultTimeout = 30min;
                        # ExecuteOptions.Compression *CompressConfig enables pre-execution prompt compression
    claude.go           # ClaudeAgent: spawns `claude` CLI; CommandRunner interface for testability;
                        # prompt written to temp file, short directive passed as arg (avoids ARG_MAX)
    codex.go            # CodexAgent: spawns `codex` CLI; supports --dangerously-bypass-approvals;
                        # same temp-file prompt pattern as claude.go
    copilot.go          # CopilotAgent: spawns `gh copilot` or standalone `copilot`; use --no-ask-user --silent for non-interactive;
                        # same temp-file prompt pattern; directive passed via -p flag
    compress.go         # CompressConfig struct; CompressPrompt() threshold check + fallback;
                        # compressViaAgent() calls configured provider CLI (claude/codex/copilot) with
                        # caveman meta-prompt; writePromptFile() creates temp file, appends file context
    util.go             # handleExecuteResult(): shared post-run logic for all three agents (exit code
                        # mapping, timeout detection, JSON extraction, CompressStats propagation)

  analysis/             # Bus-factor / code ownership analysis
    analyzer.go         # GitParser: extracts commit authors from git history
    metrics.go          # OwnershipMetrics: Herfindahl index, Gini coefficient, bus-factor count, risk level
    report.go           # ReportGenerator: formats analysis results into Report structs
    db.go               # Persists analysis results to SQLite

  budget/               # Token budget calculation and allocation
    budget.go           # Core budget logic: daily/weekly modes, reserve, aggressive end-of-week;
                        # interfaces ClaudeUsageProvider, CodexUsageProvider

  config/               # YAML config loading and validation
    config.go           # Config struct (Schedule, Budget, Providers, Projects, Tasks, Integrations,
                        # Logging, Reporting, PromptCompression); uses viper; env var overrides supported;
                        # PromptCompressionConfig: enabled/provider/model/reasoning_effort/threshold

  db/                   # SQLite persistence layer — all SQL lives here, nowhere else
    db.go               # DB struct; DefaultPath = ~/.local/share/nightshift/nightshift.db;
                        # Open() applies pragmas + auto-runs migrations
    migrations.go       # Versioned schema migrations (001–009); auto-applied on Open();
                        # migration009 adds UNIQUE index on jira_ticket_results(run_id, ticket_key)
    import.go           # Bulk data import utilities

  jira/                 # Jira autonomous system — drives ticket lifecycle via AI agents
    client.go           # Client struct wrapping go-atlassian; auth, Ping, AddComment
    config.go           # JiraConfig, RepoConfig, PhaseConfig structs; Validate(), Defaults()
    status.go           # Status/StatusMap; DiscoverStatuses(); TransitionTo{InProgress,Review,Done,NeedsInfo}()
    tickets.go          # Ticket/Comment/IssueLink structs; FetchTodoTickets(), FetchReviewTickets()
    dependencies.go     # DependencyGraph: BuildDependencyGraph(), ResolveOrder(), DetectCycles()
    validation.go       # ValidateTicket() (LLM-based, score ≥6 = valid); HandleInvalidTicket()
    workspace.go        # Workspace/RepoWorkspace; SetupWorkspace(), CleanupStaleWorkspaces()
    branch.go           # BranchName(), CommitMessage(), HasChanges(), CommitAndPush()
    orchestrator.go     # Orchestrator: Phase/TicketStatus/TicketResult types; ProcessTicket() drives
                        # validate→plan→implement→commit→PR→status; jiraClient interface for testability;
                        # detectResumeState() reads NightshiftComments to skip already-completed phases;
                        # phaseOrder map drives skip() logic; parsePRURLsFromComment() recovers PR URLs
    pr.go               # PRInfo/PRReviewState; CreateOrUpdatePR(), FetchPRReviewComments();
                        # findExistingPR uses --state open to avoid matching closed PRs; ghExec is a
                        # package-level var for test substitution
    comments.go         # CommentType/NightshiftComment; PostComment(), ParseNightshiftComments()
    feedback.go         # ProcessFeedback, buildReworkPrompt, filterNewComments; idempotency filter
                        # by lastReworkAt — skips review comments posted before the last CommentRework

  logging/              # zerolog setup
    logging.go          # Logger init; log files → ~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log

  orchestrator/         # Coordinates agent execution (plan-implement-review loop)
    orchestrator.go     # Orchestrator: task lifecycle; statuses: pending→planning→executing→reviewing
                        # →completed/failed/abandoned; DefaultMaxIterations=3;
                        # RunTask() validates workDir via gitValidator (blocks non-git dirs and $HOME);
                        # saves current branch before task and restores via defer checkoutBranch();
                        # gitValidator field is injectable for tests (WithGitValidator option);
                        # prompts split: compressible Prompt (task data) + protected PromptSuffix (instructions/schema)
    events.go           # Event types emitted during orchestration (consumed by TUI and logging)

  projects/             # Project discovery and management
    projects.go         # Scans configured paths; returns ProjectConfig list

  providers/            # AI provider usage/quota tracking (distinct from agents/ which spawns binaries)
    claude.go           # Claude provider: token usage tracking via Anthropic API
    codex.go            # Codex provider: usage tracking via OpenAI API
    copilot.go          # Copilot provider: request-count tracking (monthly, no API exposure)

  reporting/            # Report generation
    run_report.go       # Per-run report → ~/.local/share/nightshift/reports/run-YYYY-MM-DD-HHMMSS.md
    run_results.go      # RunResult types and aggregation
    summary.go          # Daily summary → ~/.local/share/nightshift/summaries/summary-YYYY-MM-DD.md

  scheduler/            # Cron-based scheduling
    scheduler.go        # Wraps robfig/cron with SkipIfStillRunning middleware (prevents concurrent fires);
                        # reads schedule config; triggers runs

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

  tasks/                # Task registry and selector
    tasks.go            # TaskDefinition: Type, Category, Name, Description,
                        # CostTier (Low/Medium/High/VeryHigh), RiskLevel, Interval;
                        # 6 categories: PR, Analysis, Options, Safe, Map, Emergency
    register.go         # RegisterCustomTasksFromConfig(): config → TaskDefinition; rolls back on failure
    selector.go         # Task selection logic (budget-aware, staleness-aware)

  workspace/            # Clone-based isolated task workspaces (opt-in via workspace.root config)
    workspace.go        # Config/RepoConfig/Workspace/RepoWorkspace types; SetupWorkspace() clones
                        # repos into <root>/<name>_<runID>/, writes .nightshift-workspace.json;
                        # CleanupStaleWorkspaces() removes dirs older than TTLDays (default 7);
                        # ValidateConfig() enforces SSH URLs; gitExecFn var injectable via SetGitExecFn();
                        # RepoConfig/RepoWorkspace carry Tasks/Pattern/Exclude for per-repo config

docs/                   # All documentation (plain markdown, no static-site generator)
  README.md             # Index pointing at user/operations/dev trees
  user/                 # End-user guides
    introduction.md     installation.md     quick-start.md
    configuration.md    cli-reference.md    tasks.md
    agents.md           jira-pipeline.md    budget.md
    scheduling.md       bus-factor.md       troubleshooting.md
  operations/           # Running Nightshift as a long-lived service
    daemon.md           systemd-install.md  logs-and-reports.md
    data-and-backup.md  security.md         release.md
  dev/                  # Internal / developer guides
    architecture.md            # Package map, layers, key design constraints
    run-lifecycle.md           # End-to-end run flow
    orchestrator.md            # Task plan→implement→review loop
    agents-internals.md        # CommandRunner, compression, temp-file pattern
    jira-pipeline.md           # Phase state machine, resume, dependency graph
    workspace.md               # Clone/branch/push conventions (Jira pipeline)
    database.md                # Schema, migration system
    budget-internals.md        # Capacity formula, provider APIs
    tasks-internals.md         # TaskDefinition, selector scoring
    state-and-snapshots.md     # RunRecord, staleness, snapshot collector
    scheduling-internals.md    # Cron wrapper, SkipIfStillRunning
    reporting.md               # Run reports, summaries, retention
    logging.md                 # zerolog setup, printf-style API, jq queries
    bus-factor.md              # HHI, Gini, risk classification
    testing.md                 # MockRunner, stubJiraClient, e2e patterns
    contributing.md            # Dev setup, git conventions, PR checklist
    debugging.md               # Log locations, common errors + fixes

scripts/
  pre-commit.sh         # Runs gofmt, go vet, go build on staged .go files

.goreleaser.yml         # Builds darwin/linux amd64+arm64; archives as tar.gz; auto-changelog
Makefile                # Targets: build, test, test-verbose, test-race, coverage, lint, clean,
                        # deps, check, install, install-hooks
go.mod                  # module github.com/cedricfarinazzo/nightshift; Go 1.24
CHANGELOG.md            # Version history
SECURITY_AUDIT.md       # Security findings
```

---

## Critical Integrations (Claude / Codex / Copilot)

- **CLAUDE.md** (this file) is injected as context into agent prompts via the `Hint` mechanism in `cmd/nightshift/commands/daemon.go`. Keep it accurate and up to date.
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
  - Audit log: `~/.local/share/nightshift/audit/audit-YYYY-MM-DD.jsonl`

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

## Documentation Workflow

Documentation lives in `docs/` as plain markdown, in three tracks:

- `docs/user/` — end-user guides (introduction, installation, quick-start, configuration, CLI reference, tasks, agents, jira-pipeline, budget, scheduling, bus-factor, troubleshooting)
- `docs/operations/` — running Nightshift as a service (daemon, systemd-install, logs-and-reports, data-and-backup, security, release)
- `docs/dev/` — internals (architecture, run-lifecycle, orchestrator, agents-internals, jira-pipeline, workspace, database, budget-internals, tasks-internals, state-and-snapshots, scheduling-internals, reporting, logging, bus-factor, testing, contributing, debugging)

Index: `docs/README.md`.

### Reading the docs FIRST when exploring

Before grepping or `Read`-ing source to understand a feature, **read the relevant `docs/dev/*.md` page first**. They are kept in sync with code and contain mermaid diagrams that map the package + state-machine layout faster than reading files. Mapping for common questions:

| Question | Start in |
|---|---|
| "How is the project structured?" | `docs/dev/architecture.md` |
| "How does a task run end-to-end?" | `docs/dev/run-lifecycle.md`, then `docs/dev/orchestrator.md` |
| "How does the Jira pipeline work?" | `docs/dev/jira-pipeline.md`, then `docs/user/jira-pipeline.md` |
| "How are prompts built / agents invoked?" | `docs/dev/agents-internals.md` |
| "How is the workspace managed?" | `docs/dev/workspace.md` |
| "How is budget enforced?" | `docs/dev/budget-internals.md` |
| "How does the selector pick tasks?" | `docs/dev/tasks-internals.md` |
| "How does the scheduler avoid overlap?" | `docs/dev/scheduling-internals.md` |
| "What's in the database?" | `docs/dev/database.md` |
| "How do I add a test?" | `docs/dev/testing.md` |
| "Where does this log go?" | `docs/dev/logging.md`, `docs/operations/logs-and-reports.md` |

When the docs disagree with the code, trust the code AND fix the docs in the same PR.

### Keeping docs in sync when changing code

Documentation is part of every code-changing PR. The rule is **same PR**, not "follow-up PR" — drift starts the moment a docs update is deferred.

For each code change, walk this checklist before requesting review:

- **Behaviour-visible to users** (new flag, changed default, removed command, new config field, new error message): update `docs/user/`. Mandatory.
- **New / renamed / removed CLI command or flag**: update `docs/user/cli-reference.md` and any `docs/user/*` page that demos it.
- **New / changed config field**: update `docs/user/configuration.md`; if Jira-related, also `docs/user/jira-pipeline.md`.
- **New / changed task in the catalog**: update `docs/user/tasks.md` if categories/tiers/intervals change; `docs/dev/tasks-internals.md` if the selector or definition shape changes.
- **Touched `internal/<pkg>/`**: update `docs/dev/<corresponding-page>.md`. If a mermaid diagram in that page no longer matches the code, regenerate it.
- **Operational change** (new file path on disk, new PID-file behaviour, new env var, new systemd-relevant detail): update `docs/operations/`.
- **Schema migration**: bump `docs/dev/database.md` and call it out in `CHANGELOG.md`.
- **New package or major file**: add a one-line entry to the Project Structure section in this file.
- **New gotcha discovered while debugging**: add to the `## Gotchas` section in this file. Include the *why* so future-you knows when the workaround can be removed.

If your change makes a doc page obsolete, **delete the page** in the same PR and remove links from `docs/README.md` and any sibling pages that reference it.

CI is not currently enforcing this — reviewers must. PRs that change `internal/` or `cmd/` without touching `docs/` should get a comment asking why.

## Self-Improvement Loop

Agents MUST follow these rules:

- **On every new learning or architectural insight**: update this file and the relevant doc in `docs/` immediately, in the same commit or PR as the code change.
- **Add gotchas as soon as discovered**: add them to the `## Gotchas` section below to avoid repeating the same investigation.
- **On adding a new package, module, or file**: add it to the Project Structure section above with a one-line description.
- **Before using any package**: search online for the latest stable version and docs; never assume a cached version is current.
- **When user-facing docs in `docs/user/` become stale**: update them in the same PR as the code change.
- **Security findings**: add to `SECURITY_AUDIT.md`.
- **Agents are encouraged to add notes, new sections, or any content they find useful** directly into this file at any time. If it's worth knowing, put it here. This file is meant to grow over time as institutional knowledge accumulates.

---

## Gotchas

- `modernc.org/sqlite` is pure Go — no CGO needed. Do not switch to `mattn/go-sqlite3`.
- Agent binaries (`claude`, `codex`, `gh`) must be in PATH. Always use the `CommandRunner` interface for testability; never call `exec.Command` directly in agent code.
- Credentials are **env-var only** — `CredentialManager` never reads from config files or disk.
- `internal/integrations/` package does **not exist** — it was removed. Do not re-create it. CLAUDE.md context is passed via Hints in `cmd/nightshift/commands/daemon.go`.
- `internal/logging.Logger` does NOT use zerolog's chainable API (`.Error().Str().Msg()`). Use `log.Infof(format, args...)` / `log.Errorf(format, args...)` directly. The zerolog chain is internal.
- `internal/jira.Orchestrator` stores `jiraClient` as an interface (not `*Client`) for testability. `*Client` satisfies the interface implicitly. The `log` field is lazily initialized inside `ProcessTicket` when nil — safe to omit in tests.
- `NIGHTSHIFT_JIRA_TOKEN` must be set for all e2e tests in `internal/jira/e2e_test.go`. Tests skip automatically when it is absent.
- **Jira resume logic** — `detectResumeState` reads `<!-- nightshift:type=... -->` HTML markers from ticket comments to determine the furthest completed phase. `ParseNightshiftComments` requires the `🤖` prefix on the comment body. If comments are missing or malformed, processing restarts from phase 1 (safe but wasteful).
- **Jira repo URL must use SSH** (`git@github.com:...`). HTTPS remotes fail silently in non-interactive contexts (`fatal: could not read Username`). Set `url: git@github.com:org/repo.git` in `jira.projects[].repos`.
- **Claude agent permissions** — for autonomous file writes, set `dangerously_bypass_approvals_and_sandbox: true` and `dangerously_skip_permissions: true` on the claude provider. Without these, the agent asks for approval mid-run and the implementation phase produces no changes.
- **`nightshift jira preview` invocation** — use `go run ./cmd/nightshift jira preview --plain` (arguments after the package path are passed to the program by `go run`). The `--` form (`go run ./cmd/nightshift -- jira preview`) is also valid.
- **Jira French status names** — the VC project uses "À faire" (todo), "En cours" (in-progress), "Revue en cours" (review), "Terminé" (done). `isReviewStatus` checks for "revue" keyword so it correctly classifies "Revue en cours". `TransitionToReview` is called in `PhaseStatus` after PR creation; if the pipeline fails at commit, `PhaseStatus` is never reached and the ticket stays "En cours".
- **`findExistingPR` requires `--state open`** — without it, closed PRs on the same branch match and `gh pr edit` runs on a closed PR instead of creating a new one. Fixed in VC-44.
- **Workspace reuse** — `SetupWorkspace` does NOT re-clone on subsequent runs for the same ticket. If the repo directory already exists, it is reused. `setupBranch` runs `git fetch origin` + `git pull --rebase origin <branch>` on every setup to sync with remote. First run sets `isNew=true`; subsequent runs set `isNew=false`.
- **Plan injected into implement prompt** — `buildImplementPrompt(ticket, plan, ws)` receives `result.Plan` (the plan agent's output). This is the text of the plan comment posted to Jira. It is injected as a "Plan" section in the implement prompt so the agent knows what to implement.
- **In-progress ticket resumption** — `detectResumeState` reads Nightshift comments to determine the furthest completed phase. If the plan phase is done (plan comment present) but implement is not, the next run resumes from implement. To force a full restart, set the ticket back to "À faire" (or equivalent todo status) — this causes `FetchTodoTickets` to pick it up fresh. WARNING: if phases are skipped via resume, the agent will not re-post the plan or re-validate. Only reset if you want a clean slate.
- **`--skip-validation` works** (since VC-38) — when `--skip-validation` is passed, the validation agent is not created and `WithSkipValidation()` is used. `ProcessTicket` permits a nil `validationAgent` when `skipValidation=true`. The phase is shown as "skipped" in preflight output.
- **`require_active_sprint`** — opt-in per-project flag in `ProjectConfig`. When `true`, `FetchTodoTickets` injects `AND sprint in openSprints()` into the JQL so only tickets in an active sprint are processed. Applied only to the TODO fetch, not to in-progress or review fetches, so in-flight tickets can always be resumed. Default `false` preserves existing behaviour for Kanban/non-sprint projects. Requires Jira Software license (`openSprints()` is not available on Jira Work Management).
- **Board auto-discovery** — `discoverBoardID()` calls `GET /rest/agile/1.0/board?projectKeyOrId=<KEY>`, caches the result in `Client.boardCache`, and returns 0 if no board found (Work Management projects). `FetchTodoTickets` uses the discovered ID to call `fetchBoardBacklogKeys` — no `board_id` config needed. If a project has multiple boards, the first one is used.
- **`board/{id}/backlog` to exclude backlog** — `GET /rest/agile/1.0/board/{id}/issue` returns ALL board-associated issues including backlog items. To subtract backlog tickets, `fetchBoardBacklogKeys` calls `board/{id}/backlog` separately and removes those keys from results.
- **Prompt temp-file pattern** — All three agents write the full prompt to `os.CreateTemp("", "nightshift-prompt-*.md")` and pass a short directive `"Read and follow the task instructions in file: <path>"` as the CLI arg. This bypasses OS ARG_MAX (~128KB on Linux). Temp file is deleted via `defer cleanup()` after `runner.Run()` returns. Never pass large prompts as positional args.
- **Compression import cycle** — `agents` cannot import `config` (config→jira→agents creates a cycle). `CompressConfig` lives in `agents` package; `PromptCompressionConfig` lives in `config` package. `compressionConfigFromApp()` in `cmd/nightshift/commands/helpers.go` bridges them. Do not try to unify these structs.
- **Compression uses CLI, not API** — `compressViaAgent()` calls the provider's `agent.Execute()`, which itself uses `writePromptFile()`. The compression meta-prompt+content goes through the same temp-file path. Never add direct HTTP/API calls to `compress.go`.
- **`ValidateTicket` signature** — takes 4 args: `(ctx, agent, ticket, compression *agents.CompressConfig)`. Pass `nil` for compression when not needed (e.g. jira_preview.go).
- **workDir git validation** — `orchestrator.RunTask()` calls `validateGitRepo(ctx, workDir)` before executing any task. Refuses to run if workDir is not inside a git repo, or if the repo root is `$HOME`. Prevents agents from running `git init` in unintended locations. Injectable via `WithGitValidator()` for tests.
- **Branch save/restore in RunTask()** — `RunTask()` calls `CurrentBranch()` before executing and uses `defer checkoutBranch()` to restore it on exit (regardless of outcome). Prevents stacked branches when multiple tasks run sequentially. If `CurrentBranch` fails or returns "HEAD" (detached), no restore is attempted.
- **Prompt split: compressible vs protected** — `ExecuteOptions.Prompt` is the compressible payload (task data only). `PromptPrefix` and `PromptSuffix` are never compressed. All critical instructions (output format, JSON schemas, behavioral rules) MUST go in `PromptSuffix`, not `Prompt`. Putting instructions in `Prompt` risks them being stripped by the compression agent.
- **PID file is atomic** — `writePidFile()` uses `O_CREATE|O_EXCL` so only one daemon can start even under a race. If a stale PID file exists (process gone), it is removed and retried. Never use `O_TRUNC` for PID files.
- **SkipIfStillRunning on cron** — `scheduler.go` wraps the cron instance with `cron.SkipIfStillRunning(cron.DiscardLogger)`. If a scheduled run exceeds its interval, the next fire is silently skipped rather than overlapping. This prevents unbounded goroutine growth on slow runs.
- **`internal/providers` has no Execute/Name/Cost** — the `Provider` interface and its stubs were removed. `providers/` only tracks quota/usage (token counts, request counts). Do not add `Execute()` back; agent invocation belongs in `internal/agents/`.
- **`handleExecuteResult` in util.go** — all three agents (claude, codex, copilot) share `handleExecuteResult()` for post-run logic. When adding a new agent, use this helper instead of duplicating exit-code/timeout/JSON-extraction logic.
- **Future-sprint exclusion is always on** — `buildTodoJQL` always appends `AND (sprint not in futureSprints() OR sprint is EMPTY)`. The `OR sprint is EMPTY` arm is mandatory: Jira's `sprint not in futureSprints()` silently excludes issues with no sprint (NULL), which drops all Kanban board tickets. `futureSprints()` requires Jira Software license; Work Management projects will get a JQL error at runtime. There is no config flag to disable this.
- **Workspace import cycle** — `internal/workspace` must NOT import `internal/config` or `internal/jira`. The bridge is `workspaceConfigFromApp()` in `cmd/nightshift/commands/helpers.go`. Do not try to merge `workspace.Config` with `config.WorkspaceConfig`.
- **Workspace state key** — in workspace mode, the repo clone path (not the configured repo name) is used as the state key for cooldown/staleness tracking. This means each fresh clone appears "new" to the staleness tracker, which is intentional: workspace runs always pick the highest-priority tasks.
- **Workspace clone uses `.` as target** — `git clone <url> .` clones into the already-created `<root>/<name>_<runID>/` directory. The directory must exist and be empty before the clone.
- **`CleanupStaleWorkspaces` walks dir contents for mtime** — on Linux, git operations update file mtimes inside a directory but not the directory entry's own mtime. `CleanupStaleWorkspaces` uses `filepath.WalkDir` to find the newest mtime across all files/subdirs inside each workspace. The directory entry mtime alone is unreliable and was the source of VC-83 (active workspaces deleted early). When writing tests for this, remember that `os.WriteFile` inside a dir updates the dir's own mtime — re-apply `os.Chtimes` on the dir after writing files if you need the dir entry to appear old.
- **Daemon workspace mode (VC-87)** — when `workspace.root` is set, `runScheduledTasks` routes to `runScheduledWorkspacedTasks` instead of the project-path loop. Both `nightshift run` and daemon share `runRepoTasks()` for per-repo task execution. State key is always `rw.Name` (not `rw.Path`) in workspace mode. First daemon run after upgrading from a pre-VC-87 release will re-process all workspace repos once (old cooldowns were keyed on paths, new ones on names — safe, one extra run).
- **`runRepoTasks` allowedTasks filter** — when `workspace.repos[*].tasks` is set, `runRepoTasks` filters selected tasks to only those types before execution. Empty/nil means no filter. Filter applied after selector, so cooldown/staleness scoring still runs on all tasks but only matching types are executed.
- **Rejection forces re-validation on next run** — `detectResumeState` scans raw `ticket.Comments` for `❌ Nightshift — Ticket Rejected` (posted by `HandleInvalidTicket`). If that rejection comment is newer than the last `CommentValidation`, `hasValidation` is treated as false and the ticket falls to `PhaseValidate` for re-evaluation. If `CommentValidation` is newer (user fixed ticket, a later run accepted it), the rejection is ignored and normal resume logic applies. Root cause of FIN-31: prior `CommentValidation` from an older partial run caused `detectResumeState` to skip validation despite a newer rejection.
