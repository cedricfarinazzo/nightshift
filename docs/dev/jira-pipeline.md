# Jira Pipeline Internals

`internal/jira/` — autonomous ticket lifecycle.

## Components

| File | Purpose |
|------|---------|
| `client.go` | `Client` wraps `go-atlassian/v2`; auth, `Ping`, `AddComment` |
| `config.go` | `JiraConfig`, `RepoConfig`, `PhaseConfig`; `Validate()`, `Defaults()` |
| `status.go` | `StatusMap`; `DiscoverStatuses`; `TransitionTo{InProgress,Review,Done,NeedsInfo}` |
| `tickets.go` | `Ticket`, `Comment`, `IssueLink`; `FetchTodoTickets`, `FetchReviewTickets` |
| `dependencies.go` | `BuildDependencyGraph`, `ResolveOrder`, `DetectCycles` |
| `validation.go` | LLM validation; `ValidateTicket(ctx, agent, ticket, compression)` score ≥ 6 = valid |
| `workspace.go` | `SetupWorkspace`, `CleanupStaleWorkspaces` |
| `branch.go` | `BranchName`, `CommitMessage`, `HasChanges`, `CommitAndPush` |
| `orchestrator.go` | `ProcessTicket`; phase machine; `detectResumeState` |
| `pr.go` | `CreateOrUpdatePR`, `FetchPRReviewComments`; `findExistingPR` uses `--state open` |
| `comments.go` | `PostComment`, `ParseNightshiftComments` (requires `🤖` prefix) |
| `feedback.go` | `ProcessFeedback`, `buildReworkPrompt`, `filterNewComments` (idempotency) |

## Phase Machine

`ProcessTicket` drives:

```
validate ─► plan ─► implement ─► commit ─► PR ─► status
   │                                  │
   ▼                                  ▼
needs-info                          (resume on next run if any phase failed)
```

`phaseOrder` map drives `skip()` so resume can leap to the right phase.

After each phase a `🤖 <!-- nightshift:type=<phase> -->` comment is posted (`PostComment`).

## Resume Logic

`detectResumeState(ctx, ticket)`:

1. Fetch ticket comments
2. `ParseNightshiftComments` filters for `🤖 <!-- nightshift:type=... -->` markers
3. Determine furthest completed phase
4. Return resume cursor

`ProcessTicket` then skips already-completed phases. To force a restart, move the ticket back to TODO.

## Dependency Resolution

`BuildDependencyGraph` reads `issuelinks` (blocks/is-blocked-by). `ResolveOrder` returns a topo-sorted slice; `DetectCycles` errors out if it finds one. The pipeline processes tickets in `ResolveOrder` order.

## Validation

`ValidateTicket(ctx, agent, ticket, compression)` — calls LLM with a scoring prompt. Score ≥ 6 = valid. Invalid tickets:

```go
HandleInvalidTicket(ctx, client, ticket, score, reasons)
  → PostComment(needs-info)
  → TransitionToNeedsInfo
```

`--skip-validation` (CLI flag) passes nil agent + `WithSkipValidation()`. `ProcessTicket` permits nil `validationAgent` only when `skipValidation=true`. Preflight UI shows the phase as "skipped".

## Workspace

`SetupWorkspace(repo, ticket)`:

- First run: `git clone <ssh-url> <workspace-dir>`; `isNew=true`
- Subsequent runs: reuse existing dir; `isNew=false`
- Always: `git fetch origin && git pull --rebase origin <branch>` — even on reuse

SSH URLs required (`git@github.com:...`). HTTPS remotes fail silently in non-interactive contexts (`fatal: could not read Username`).

## Branch + commit conventions

`BranchName(ticket)` → e.g. `feature/VC-44`. `CommitMessage(...)` includes `Refs: VC-44` in body. `CommitAndPush(workspace, msg)` no-ops if `!HasChanges`.

## PR creation

`CreateOrUpdatePR(ctx, repo, ticket, plan)`:

1. `findExistingPR(repo, branch, --state open)` — `--state open` is critical; without it closed PRs match and `gh pr edit` runs on the wrong PR
2. If found → `gh pr edit`; else → `gh pr create`
3. PR body includes plan summary + ticket link

`ghExec` is a package-level var so tests can stub it.

## Feedback Handling

For tickets in review status:

```
FetchReviewTickets
  → for each:
       FetchPRReviewComments
       filterNewComments(by lastReworkAt)   // idempotency
       buildReworkPrompt
       agent.Execute
       CommitAndPush
       PostComment(rework)
```

`filterNewComments` drops comments older than the last `CommentRework` marker — prevents re-fixing the same feedback.

## French status names (VC project)

The VC project uses: "À faire", "En cours", "Revue en cours", "Terminé". `isReviewStatus` checks for the substring "revue" to classify "Revue en cours" correctly. `TransitionToReview` is called in `PhaseStatus` after PR creation — if anything fails before PhaseStatus, the ticket stays in "En cours".

## Sprint / Backlog Filtering

```yaml
require_active_sprint: true   # injects AND sprint in openSprints()
board_type: kanban
board_id: 42                  # needed for kanban
```

- `openSprints()` requires Jira Software (not Work Management).
- Kanban: `/rest/agile/1.0/board/<id>/issue` includes backlog items. `fetchKanbanBoardTickets` also calls `/board/<id>/backlog` and subtracts.

## Testability

- `jiraClient` is an interface in `Orchestrator` (not `*Client`) — `*Client` satisfies it implicitly
- `log` field is lazily initialised inside `ProcessTicket` if nil — safe to omit in tests
- `ghExec` is a package-level var — substitute in tests
- See `e2e_test.go` — skips unless `NIGHTSHIFT_JIRA_TOKEN` is set
- See `orchestrator_test.go` for `stubJiraClient` pattern
