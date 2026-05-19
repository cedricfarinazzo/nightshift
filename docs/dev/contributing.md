# Contributing

## Dev Setup

```bash
git clone https://github.com/cedricfarinazzo/nightshift.git
cd nightshift
make install-hooks      # symlinks scripts/pre-commit.sh into .git/hooks/
make build
make test
```

Pre-commit hook runs: `gofmt`, `go vet`, `go build` on staged `.go` files.

To bypass in a pinch: `git commit --no-verify`. Don't do this on PRs.

## Go Conventions

- `internal/` packages — nothing in `internal/` is public API
- Interfaces at the use site, not the implementor
- `context.Context` first arg for any I/O or blocking function
- No `init()` — explicit init in `main` / `setup` only
- Keep `cmd/` thin — all business logic in `internal/`
- All SQL in `internal/db/`
- All viper access in `internal/config/`
- All credential access in `internal/security/credentials.go`

## Error Wrapping

```go
return fmt.Errorf("failed to setup workspace for %s: %w", ticket.Key, err)
```

Never swallow. Never `panic` in library code.

## Logging

```go
log := logging.Component("orchestrator")
log.Infof("starting %s on %s", task, project)
log.Errorf("agent failed: %v", err)
```

Use printf-style helpers — the chainable zerolog API is internal to the logging package.

## Commits

**Conventional Commits**: `type(scope): summary`.

Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `ci`.

Examples:

```
feat(budget): add per-provider daily cap enforcement
fix(jira): use --state open when looking up existing PR
refactor(agents): extract shared post-run handler into util.go
```

Rules:

- Atomic — one logical change per commit
- Include `Refs: TICKET-KEY` in body when driven by a ticket
- **Never amend** — new commits only
- No merge commits on feature branches — rebase onto main
- No force-push to `main`

## Branches

`type/short-description`:

- `feat/budget-per-provider-cap`
- `fix/jira-existing-pr-state-filter`
- `docs/rewrite-tree`

## PRs

- Small + focused
- Clear title following Conventional Commits
- Description: what + why
- Test plan checklist
- Link related ticket

`/ultrareview` triggers a multi-agent cloud review on the current branch — see CLAUDE.md.

## CLAUDE.md Updates

Update `CLAUDE.md` when you:

- Add a new package or major file
- Discover a non-obvious gotcha
- Change a critical convention

The gotchas section is the most valuable — add to it as soon as you discover something.

## Where to ask

[github.com/cedricfarinazzo/nightshift/issues](https://github.com/cedricfarinazzo/nightshift/issues)
