# Workspace Management

How Nightshift creates and manages isolated working directories for Jira tickets.

## Purpose

Each Jira ticket gets its own workspace directory. Inside that directory every
configured repository is cloned (or reused) and checked out on the ticket's
feature branch. This isolates concurrent ticket runs from each other.

## Directory Layout

```
WorkspaceRoot/               # cfg.jira.workspace_root (default: ~/.local/share/nightshift/workspaces)
└── VC-42/                   # one directory per ticket key
    ├── my-api/              # clone of git@github.com:org/my-api.git
    │   └── (git checkout feature/vc-42)
    └── my-frontend/         # clone of git@github.com:org/my-frontend.git
        └── (git checkout feature/vc-42)
```

## SetupWorkspace

```go
func SetupWorkspace(ctx context.Context, cfg JiraConfig, ticketKey string) (*Workspace, error)
```

### Validation

- `ticketKey` must match `^[A-Z][A-Z0-9]+-\d+$`; rejected otherwise.
- Repo names must not contain `/`, `\`, or `..` (path traversal guard).

### Steps

1. Resolve `WorkspaceRoot` (expands `~/`).
2. `os.MkdirAll(workspaceRoot/ticketKey)`.
3. For each configured repo:
   a. If `repoPath` does not exist → `git clone <url> <name>`.
   b. If `repoPath` already exists → reuse (no re-clone).
   c. `setupBranch(ctx, repoPath, branchName, baseBranch)`:
      - Tries `git checkout <branch>` (already exists).
      - On failure: `git checkout -b <branch> origin/<baseBranch>` (new branch).
      - Returns `isNew bool`.
4. Returns a `Workspace` with all `RepoWorkspace` entries populated.

### Branch Name Convention

```go
func BranchName(ticketKey string) string {
    return "feature/" + strings.ToLower(ticketKey)
}
```

Example: `VC-42` → `feature/vc-42`

### Workspace Struct

```go
type Workspace struct {
    TicketKey string
    Root      string        // absolute path to the ticket directory
    Repos     []RepoWorkspace
}

type RepoWorkspace struct {
    Name       string
    Path       string       // absolute path to the cloned repo
    URL        string       // remote URL (SSH)
    Branch     string       // feature/vc-42
    BaseBranch string       // main (default) or from config
    IsNew      bool         // true if the branch was just created
}
```

## Stale Workspace Cleanup

```go
func CleanupStaleWorkspaces(cfg JiraConfig) (int, error)
```

Removes workspace directories whose **directory modification time** is older
than `cfg.CleanupAfterDays` days.

- If `CleanupAfterDays ≤ 0`, no cleanup is performed.
- Only directories directly under `WorkspaceRoot` are considered (non-recursive).
- Returns the count of directories removed.

The `nightshift daemon` runs `CleanupStaleWorkspaces` on each start and after
each Jira pipeline run.

## Config

```yaml
jira:
  workspace_root: "~/.local/share/nightshift/workspaces"
  cleanup_after_days: 14

  repos:
    - name: my-api
      url: "git@github.com:org/my-api.git"
      base_branch: main
    - name: my-frontend
      url: "git@github.com:org/my-frontend.git"
      base_branch: develop
```

### SSH URL Requirement

Repo URLs **must use SSH** (`git@github.com:org/repo.git`). HTTPS URLs fail
silently in non-interactive contexts:

```
fatal: could not read Username for 'https://github.com': No such device or address
```

SSH authentication uses the agent's key loaded into `ssh-agent`. Ensure the
key is added before running nightshift (`ssh-add ~/.ssh/id_ed25519`).

## Git Execution

`gitExec` is an internal helper:

```go
func gitExec(ctx context.Context, dir string, args ...string) ([]byte, error)
```

It runs `git <args>` with `cmd.Dir = dir` and returns combined stdout+stderr
on error for debugging. There is no `CommandRunner` interface here — tests
that need to avoid real git operations use a temporary directory with a real
git repo or skip with `t.Skip` when git is not needed.

## Concurrency

Each `ProcessTicket` call sets up its own workspace synchronously before
launching agent phases. Two tickets with different keys use different
subdirectories and never share a `RepoWorkspace` — no locking is needed.

If two tickets share the same key (impossible in normal operation, possible in
tests), they would conflict on `setupBranch`. Prevent this by ensuring each
test uses a unique ticket key.

## Error Handling

| Error | Cause | Resolution |
|-------|-------|-----------|
| `invalid ticket key` | Malformed key (lowercase, no project prefix) | Fix the ticket key format |
| `clone <name>: exit 128` | SSH auth failure or wrong URL | Check `ssh -T git@github.com`; verify `url` uses SSH format |
| `branch <name> in <repo>: ...` | Remote base branch doesn't exist | Check `base_branch` config matches the real branch name |
| `mkdir workspace: permission denied` | `workspace_root` not writable | Check directory permissions |
