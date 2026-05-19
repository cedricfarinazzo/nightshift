# Architecture

How Nightshift's code is organised, how a run flows end-to-end, and the key design constraints.

```mermaid
graph TD
    CLI["cmd/nightshift<br/>cobra root + subcommands"]

    CLI --> Config["internal/config<br/>viper, YAML"]
    CLI --> Budget["internal/budget<br/>capacity calc"]
    CLI --> Tasks["internal/tasks<br/>~60 built-ins + selector"]
    CLI --> Orch["internal/orchestrator<br/>plan→implement→review"]
    CLI --> JiraOrch["internal/jira<br/>phase machine"]
    CLI --> DB["internal/db<br/>SQLite + migrations"]
    CLI --> Security["internal/security<br/>credentials, audit, sandbox"]
    CLI --> Sched["internal/scheduler<br/>cron + SkipIfStillRunning"]

    Orch --> Agents["internal/agents<br/>claude, codex, copilot"]
    Orch --> Providers["internal/providers<br/>usage/quota tracking"]
    Orch --> Reporting["internal/reporting"]
    Orch --> State["internal/state"]

    JiraOrch --> Agents
    JiraOrch --> JiraAPI["go-atlassian/v2"]
    JiraOrch --> GH["gh CLI"]

    Budget --> UsageAPI["Anthropic / OpenAI<br/>GitHub Usage APIs"]
    Sched --> Orch
    Sched --> JiraOrch
```

## High-Level Map

```
cmd/nightshift/             # CLI binary (cobra root + subcommands)
internal/
  agents/                   # Spawns external CLI binaries (claude, codex, gh copilot)
  analysis/                 # Bus-factor / code ownership analysis
  budget/                   # Budget capacity calculation
  config/                   # YAML loading via viper; the ONLY home for viper
  db/                       # SQLite + migrations; the ONLY home for raw SQL
  jira/                     # Jira autonomous pipeline (validate→plan→implement→PR)
  logging/                  # zerolog setup
  orchestrator/             # Task plan→implement→review loop
  projects/                 # Project discovery
  providers/                # Quota / usage tracking (Anthropic, OpenAI, Copilot)
  reporting/                # Run reports + daily summaries
  scheduler/                # Cron / interval wrapper
  security/                 # Credentials + audit + sandbox
  setup/                    # Setup wizard presets
  snapshots/                # Usage snapshot collector
  state/                    # Persisted RunRecord
  stats/                    # Historical aggregates
  tasks/                    # Built-in task catalog + selector
```

## Providers vs Agents

A common confusion:

| Layer | Package | Purpose |
|-------|---------|---------|
| **Providers** | `internal/providers/` | Track usage + quota via API. Three: Claude, Codex, Copilot. |
| **Agents** | `internal/agents/` | Spawn external CLI binaries. Return raw output + JSON if present. |

A provider answers "how many tokens do I have left?". An agent does the actual work. The task orchestrator uses providers for budget gating + agents for execution. The Jira orchestrator only uses agents.

```mermaid
flowchart LR
    Budget["budget.Manager"] -.uses.-> P1["providers.Claude"]
    Budget -.uses.-> P2["providers.Codex"]
    Budget -.uses.-> P3["providers.Copilot"]
    Orchestrator["orchestrator"] -.uses.-> A1["agents.ClaudeAgent"]
    Orchestrator -.uses.-> A2["agents.CodexAgent"]
    Orchestrator -.uses.-> A3["agents.CopilotAgent"]
    A1 -- spawns --> B1["claude CLI"]
    A2 -- spawns --> B2["codex CLI"]
    A3 -- spawns --> B3["gh copilot / copilot CLI"]
    P1 -- HTTP --> API1["Anthropic Usage API"]
    P2 -- HTTP --> API2["OpenAI Usage API"]
    P3 -- HTTP --> API3["GitHub Copilot API"]
```

## Run Lifecycle

```
nightshift run
    │
    ├── Load config (internal/config)
    ├── Open DB + apply migrations (internal/db)
    ├── Check budget — skip if exhausted (internal/budget)
    ├── Select tasks: priority + staleness + cost (internal/tasks)
    ├── For each project × task:
    │      ├── Validate workdir is a git repo, not $HOME
    │      ├── Save current branch (defer restore)
    │      ├── Build prompt: PromptPrefix + Prompt(compressible) + PromptSuffix(protected)
    │      ├── Optional: compress Prompt if > threshold
    │      ├── Plan phase (LLM)
    │      ├── Implement phase (LLM)
    │      ├── Review phase
    │      │     └── if fail and iterations < 3: retry implement
    │      ├── Write report
    │      └── Restore branch
    └── Update state, write summary
```

```mermaid
flowchart TD
    Start(["nightshift run"]) --> Cfg["load config"]
    Cfg --> DB["open DB + apply migrations"]
    DB --> Bud{"budget OK?"}
    Bud -- no --> Skip(["skip run"])
    Bud -- yes --> Sel["select tasks<br/>priority + staleness + cost"]
    Sel --> Loop["for each project × task"]
    Loop --> Val["validateGitRepo<br/>refuse non-git or $HOME"]
    Val --> Save["save branch<br/>defer restore"]
    Save --> Prompt["build prompt<br/>prefix + body + suffix"]
    Prompt --> Comp{"len(body) ><br/>threshold?"}
    Comp -- yes --> Cmprs["compressViaAgent"] --> Plan
    Comp -- no --> Plan["plan phase (LLM)"]
    Plan --> Impl["implement phase (LLM)"]
    Impl --> Rev{"review<br/>passes?"}
    Rev -- yes --> Report["write report"]
    Rev -- no --> Iter{"iter < 3?"}
    Iter -- yes --> Impl
    Iter -- no --> Abandon["abandoned"]
    Report --> Restore["checkout original branch"]
    Abandon --> Restore
    Restore --> Loop
```

See [Run Lifecycle](run-lifecycle.md) for full detail.

## Jira Lifecycle

```
nightshift jira run
    │
    ├── Connect (Atlassian Cloud) + discover statuses
    ├── FetchTodoTickets — filter by label, status, sprint (optional)
    ├── BuildDependencyGraph — issuelinks, ResolveOrder, DetectCycles
    ├── For each ready ticket:
    │      ├── SetupWorkspace (clone once, reuse + git pull --rebase)
    │      ├── detectResumeState (read 🤖 nightshift comments)
    │      ├── ProcessTicket: validate→plan→implement→commit→PR→status
    │      └── PostComment after each phase
    └── FetchReviewTickets → ProcessFeedback (PR review → fix → push)
```

```mermaid
flowchart TD
    Start(["nightshift jira run"]) --> Conn["Connect + DiscoverStatuses"]
    Conn --> Fetch["FetchTodoTickets<br/>+ FetchReviewTickets"]
    Fetch --> Graph["BuildDependencyGraph<br/>ResolveOrder, DetectCycles"]
    Graph --> WS["SetupWorkspace<br/>clone or reuse + pull --rebase"]
    WS --> Resume["detectResumeState"]
    Resume --> Phase["ProcessTicket"]
    subgraph Phase
        direction TB
        V["validate<br/>score ≥6"] --> P["plan"] --> I["implement"] --> C["commit"] --> PR["PR<br/>gh pr create/edit"] --> St["status → In Review"]
    end
    Phase --> Done["jira_ticket_results row<br/>run report"]
    Phase -. failure .-> ErrC["🤖 error comment<br/>skip ticket"]
```

See [Jira Pipeline Internals](jira-pipeline.md).

## Budget Enforcement

```
fetch_usage() per provider          # Anthropic / OpenAI / GitHub APIs
  → per-window utilisation          # five_hour, seven_day, monthly_limit
  → computeWindowCapacity(window, max_percent)
  → capacity = min across windows
gate: OK = ignoreBudget || capacity > 0
```

Single config knob: `budget.max_percent` (default 90). No token arithmetic. See [Budget Internals](budget-internals.md).

```mermaid
flowchart LR
    Trigger(["run / scheduler tick"]) --> Each["for each enabled provider<br/>in preference order"]
    Each --> Fetch["Usage(ctx)"]
    Fetch --> Each2["for each window<br/>5h, 7d, monthly"]
    Each2 --> Calc["computeWindowCapacity<br/>= max_percent − used_pct"]
    Calc --> Min["min across windows"]
    Min --> Gate{"capacity > 0<br/>OR ignoreBudget?"}
    Gate -- yes --> Use(["use this provider"])
    Gate -- no --> Next{"next provider<br/>in preference?"}
    Next -- yes --> Each
    Next -- no --> SkipRun(["skip run"])
```

## Database

SQLite at `~/.local/share/nightshift/nightshift.db` via `modernc.org/sqlite` (pure Go, no CGO).

Properties:
- WAL mode enabled (concurrent reads)
- `busy_timeout=5000`
- Foreign keys on
- Migrations auto-applied on `Open()`
- All SQL lives in `internal/db/` — nowhere else

See [Database](database.md).

## Key Design Constraints

1. **Agents are external processes** — always use the `CommandRunner` interface; never call `exec.Command` directly in agent code.
2. **Interfaces at the use site** — define interfaces in the package that uses them, not the implementor.
3. **Thin `cmd/`** — all business logic in `internal/`. `cmd/` only wires cobra commands.
4. **No `init()`** — explicit initialisation in `main` / `setup` only.
5. **Context first** — `context.Context` is the first parameter for any I/O or blocking function.
6. **One SQL home** — all SQL in `internal/db/`; no raw queries elsewhere.
7. **One config home** — all viper access in `internal/config/`.
8. **One credentials home** — all secret access in `internal/security/credentials.go`.
9. **Compressible vs protected prompts** — `Prompt` is compressible (task data); `PromptPrefix` + `PromptSuffix` are never compressed. Instructions + JSON schemas MUST go in `PromptSuffix`.
10. **Atomic PID files** — `O_CREATE|O_EXCL`; never `O_TRUNC`.
