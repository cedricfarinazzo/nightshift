# Workspace Management

`internal/jira/workspace.go` — clone, branch, push for the Jira pipeline.

```mermaid
flowchart TD
    Start(["SetupWorkspace(ctx, repo, ticket)"]) --> Exists{"workspace dir<br/>exists?"}
    Exists -- no --> Clone["git clone ssh-url dir<br/>isNew = true"]
    Exists -- yes --> Reuse["reuse dir<br/>isNew = false"]
    Clone --> Branch
    Reuse --> Branch
    Branch["setupBranch"] --> Fetch["git fetch origin"]
    Fetch --> Checkout["git checkout -B feature/&lt;KEY&gt; origin/&lt;base&gt;"]
    Checkout --> Rebase["git pull --rebase origin feature/&lt;KEY&gt;<br/>(may no-op if branch new)"]
    Rebase --> Ready(["workspace ready"])

    Ready -. later .-> CP["CommitAndPush"]
    CP --> HC{"HasChanges?"}
    HC -- no --> NoOp(["no-op"])
    HC -- yes --> Add["git add -A → commit → push -u"]
```

## Layout

```
~/.local/share/nightshift/workspaces/
  <repo-name>/
    .git/
    ... working tree ...
```

One workspace per repo; reused across tickets.

## Setup

```go
ws, err := SetupWorkspace(ctx, repo, ticket)
```

Logic:

1. If `<workspace-dir>` does not exist → `git clone <ssh-url> <dir>`; `isNew = true`
2. Else → reuse; `isNew = false`
3. Always: `setupBranch(ws, ticket)`

`setupBranch` does:

```
git fetch origin
git checkout -B feature/<TICKET-KEY> origin/<base-branch>
git pull --rebase origin feature/<TICKET-KEY>     # sync if branch already exists remotely
```

## SSH Required

Repos must use `git@github.com:...`. HTTPS remotes fail silently in non-interactive contexts (`fatal: could not read Username`). The config validator does not currently reject HTTPS URLs — but the run will hang or fail mysteriously, so set SSH explicitly.

## Branch Naming

`BranchName(ticket)`:

```
feature/<TICKET-KEY>
```

E.g. `feature/VC-42`. Always rooted at `feature/`.

## Commit Message

`CommitMessage(ticket, summary)`:

```
<type>(<scope>): <summary>

<body>

Refs: <TICKET-KEY>
```

`type` is inferred from the ticket type (Bug → fix, Story → feat, etc.).

## Push

`CommitAndPush(ws, msg)`:

1. `HasChanges(ws)` — if no diff, no-op + return false
2. `git add -A`
3. `git commit -m <msg>`
4. `git push -u origin feature/<TICKET-KEY>`

## Cleanup

`CleanupStaleWorkspaces(cfg)` — removes workspace dirs whose newest file activity (across all contained files and subdirs) is older than `cfg.CleanupAfterDays` days. Called from the daemon periodically.

**Staleness is determined by walking the entire workspace directory tree** and taking the newest mtime found among all entries. The workspace directory entry's own mtime is not used alone, because on Linux, git operations (clone, fetch, checkout, file writes) update file mtimes inside a directory without updating the directory entry's own mtime. Using the directory mtime alone would cause active workspaces to be falsely deleted.

## Resume semantics

Because workspaces are reused, a partial run leaves edits in the working tree. On resume:

1. `setupBranch` syncs to remote (which may not yet have the partial commit) — partial uncommitted edits remain in the working tree.
2. `detectResumeState` reads ticket comments to find the resume point.
3. Phases run from that point. If the implement phase had already partially edited the tree, those edits may still be present, which is fine — the agent will see them as starting state.

For a true clean slate, move the ticket back to TODO and let `CleanupStaleWorkspaces` clear the dir, or manually `rm -rf <workspace>`.

## Testability

`workspace_test.go` uses a temp dir + a stub `git` executable wrapper. The `Run` function for git commands is a package-level var; substitute in tests.
