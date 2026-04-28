# Nightshift Jira Pipeline

This guide covers the autonomous Jira pipeline — how `nightshift jira run` and `nightshift jira preview` work, the ticket lifecycle, and resume behaviour.

## Overview

The Jira pipeline lets nightshift autonomously drive a Jira project: it fetches tickets labelled `nightshift`, validates each one, writes a plan, implements it in a local workspace, pushes a branch, creates a PR, and transitions the Jira ticket to review — all without human intervention.

```
nightshift jira run             # Full autonomous run
nightshift jira preview         # Dry-run: show what would happen
```

## Ticket Lifecycle

Each TODO ticket goes through six phases in order:

```
validate → plan → implement → commit → pr → status
```

| Phase | What happens |
|-------|-------------|
| **validate** | LLM scores ticket 1–10. Score < 6 → `HandleInvalidTicket` → ticket rejected. Successful validation transitions the ticket to "En cours" (in-progress). |
| **plan** | LLM writes an implementation plan and posts it as a Jira comment. |
| **implement** | LLM runs in the workspace directory and makes code changes. |
| **commit** | Staged changes are committed with `git commit` and pushed to the feature branch. |
| **pr** | `gh pr create` (or `gh pr edit` for existing open PR) creates the pull request. |
| **status** | Summary comment posted; ticket transitions to review and is marked completed internally. |

### Review Phase (separate loop)

Tickets already in a review status (e.g. "Revue en cours") with open PRs that have review comments go through a separate feedback loop: nightshift fetches review comments, applies fixes, and pushes an updated commit.

## Resume from Any Phase

If a run is interrupted (network error, timeout, agent failure), nightshift can resume from where it left off on the next invocation. It reads the machine-parseable HTML markers embedded in existing Jira comments to detect the furthest completed phase:

| Last comment found | Next phase on resume |
|--------------------|----------------------|
| None | `validate` (start fresh) |
| `validation` | `plan` |
| `plan` | `implement` (plan text is recovered from comment) |
| `implementation` | `commit` (plan recovered for context) |
| `pr` | `status` (PR URLs recovered from comment body) |
| `status_change` | `status` (already complete; no-op) |

This means re-running `nightshift jira run --ticket VC-N` is always safe — completed phases are skipped.

## Preview Command

`nightshift jira preview` is a dry-run that shows exactly what `nightshift jira run` would do without mutating any state:

```bash
nightshift jira preview                 # TUI pager (default)
nightshift jira preview --plain         # No pager
nightshift jira preview --json          # JSON output
nightshift jira preview --validate      # Run LLM validation per ticket (costs tokens)
nightshift jira preview --explain       # Full budget breakdown
nightshift jira preview --project MYP   # Override project key
```

Output sections:
- **Connection** — Jira site, project, user
- **Phase Assignments** — which provider handles each phase
- **Budget** — token allowance remaining (summary, or full breakdown with `--explain`)
- **TODO Tickets** — tickets ready to process, in topological dependency order
- **Execution Order** — keys in the order they will be processed
- **Blocked Tickets** — tickets with unresolved dependencies
- **Review Tickets** — tickets awaiting rework
- **Skipped** — errors that prevented processing a ticket (e.g. agent not found)

## Dependency Graph

Jira issue links of type `Blocks` are used to build a dependency graph. `nightshift jira preview` shows which tickets are ready (`ExecutionOrder`) and which are blocked (`BlockedTickets`).

Cycles are detected and reported. A ticket involved in a cycle is treated as blocked.

## Workspace

For each ticket, nightshift creates an isolated workspace:

1. Clones (or reuses) the configured repo into `~/.local/share/nightshift/workspaces/<ticket-key>/`
2. Checks out a branch named `feature/<ticket-key>` (created from `base_branch` if it doesn't exist)
3. The LLM agent runs with this directory as its working directory
4. After implementation, changes are committed and pushed; the workspace is retained for the review cycle
5. Stale workspaces (tickets no longer active) are cleaned up at the end of each run

## Configuration

All Jira settings live under `jira:` in `~/.config/nightshift/config.yaml`:

```yaml
jira:
  site: yoursite                     # Atlassian site name (yoursite.atlassian.net)
  email: you@example.com
  token_env: NIGHTSHIFT_JIRA_TOKEN   # Env var holding the API token
  project: PROJ                      # Jira project key
  label: nightshift                  # Label used to find tickets
  repos:
    - name: myrepo
      url: git@github.com:org/repo.git   # Use SSH to avoid interactive auth
      base_branch: main
  validation:
    provider: claude
    model: claude-haiku-4-5-20251001
    timeout: 2m
  plan:
    provider: claude
    model: claude-sonnet-4-6
    timeout: 5m
  implement:
    provider: claude
    model: claude-sonnet-4-6
    timeout: 30m
  review_fix:
    provider: claude
    model: claude-sonnet-4-6
    timeout: 20m
  budget_enabled: true
  max_tickets: 10
```

### Agent Permissions

For the implementation agent to write files autonomously, the Claude provider must have permissions bypassed:

```yaml
providers:
  claude:
    dangerously_bypass_approvals_and_sandbox: true
    dangerously_skip_permissions: true
```

**Use SSH remotes** (`git@github.com:...`) — HTTPS remotes prompt for credentials in a non-interactive context and will fail.

## Comment Format

All comments nightshift posts contain an HTML marker for machine parsing:

```
🤖 Nightshift — Plan (2026-04-08 00:38)
Provider: claude | Model: claude-sonnet-4-6 | Duration: 16s

<plan body text>

<!-- nightshift:type=plan provider=claude model=claude-sonnet-4-6 duration=16s -->
```

The `<!-- nightshift:type=... -->` marker is parsed by `ParseNightshiftComments` on subsequent runs to detect the resume state. These markers must not be edited.

## Key Source Files

| File | Purpose |
|------|---------|
| `internal/jira/orchestrator.go` | `ProcessTicket` — drives the full lifecycle; `detectResumeState` — phase skip logic |
| `internal/jira/tickets.go` | `FetchTodoTickets`, `FetchReviewTickets` — JQL queries |
| `internal/jira/dependencies.go` | `BuildDependencyGraph`, `ResolveOrder` |
| `internal/jira/status.go` | `TransitionToReview`, `TransitionToInProgress`, `DiscoverStatuses` |
| `internal/jira/comments.go` | `PostComment`, `ParseNightshiftComments`, `GetLastCommentOfType` |
| `internal/jira/workspace.go` | `SetupWorkspace`, `CleanupStaleWorkspaces` |
| `internal/jira/branch.go` | `BranchName`, `CommitAndPush`, `HasChanges` |
| `internal/jira/pr.go` | `CreateOrUpdatePR`, `FetchPRReviewComments` |
| `cmd/nightshift/commands/jira_run.go` | CLI entrypoint for `nightshift jira run` |
| `cmd/nightshift/commands/jira_preview.go` | CLI entrypoint for `nightshift jira preview` |
