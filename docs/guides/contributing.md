# Contributing Guide

---

## Development Setup

```bash
git clone https://github.com/cedricfarinazzo/nightshift.git
cd nightshift

# Install pre-commit hooks (gofmt, go vet, go build on staged .go files)
make install-hooks

# Download dependencies
make deps

# Build
make build

# Run tests
make test
```

Requires **Go 1.24+**.

---

## Pre-commit Hooks

`scripts/pre-commit.sh` runs three checks on every commit against staged `.go` files:

| Check | Tool | Failure action |
|-------|------|----------------|
| Formatting | `gofmt -l` | Run `gofmt -w .` |
| Vet | `go vet ./...` | Fix the reported issue |
| Build | `go build ./...` | Fix compile errors |

Install with `make install-hooks`. Use `git commit --no-verify` only in emergencies.

---

## Git Conventions

### Branch naming

```
type/short-description
```

Examples: `feat/model-selection`, `fix/budget-overflow`, `docs/jira-guide`

Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `ci`

### Commit messages

Follow **Conventional Commits**:

```
type(scope): short summary

Optional body explaining the why, not the what.

Refs: PROJ-123     ← include Jira ticket ID when applicable
```

Examples:
```
feat(budget): add per-provider daily cap enforcement
fix(jira): skip closed PRs when finding existing PR
docs(agents): add Copilot invocation details
test(orchestrator): add resume-from-commit phase test
```

### Commit rules

- **Atomic commits** — one logical change per commit
- **No merge commits** on feature branches — rebase onto main
- **Never amend** pushed commits — create a new commit for follow-ups
- **Never force-push** to `main`

---

## Code Conventions

### Package structure

- `cmd/` — thin CLI layer only; all logic goes in `internal/`
- `internal/` — all business logic; nothing exported as a public API
- All SQL in `internal/db/` only
- All config access via `internal/config/` only
- All credential access via `internal/security/credentials.go`

### Go style

- Standard `gofmt` formatting (enforced by pre-commit hook)
- `context.Context` as first parameter for any function doing I/O
- Explicit initialization in `main`/setup — no `init()` functions
- Interfaces belong in the package that *uses* them
- Errors: always wrap with context — `fmt.Errorf("doing X: %w", err)`; never swallow; never `panic` in library code
- No unexported package-level variables (except test hooks)

### Logging

Use `rs/zerolog` via `internal/logging`. **Do not use** the zerolog chainable API directly in most packages. Use the `log.Infof` / `log.Errorf` helpers from `internal/logging`:

```go
// Good
log.Infof("ticket %s: phase %s completed", ticket.Key, phase)

// Bad — zerolog chain is internal
log.Info().Str("ticket", ticket.Key).Str("phase", string(phase)).Msg("completed")
```

Include structured fields, not string interpolation, in zerolog calls in packages that use it directly.

### Testing

- Table-driven tests in `_test.go` alongside the code under test
- Use `CommandRunner` interface + `MockRunner` for agent tests
- Use `jiraClient` interface + `stubJiraClient` for Jira orchestrator tests
- Use `:memory:` SQLite for DB tests
- See `docs/guides/testing.md` for full patterns

---

## Adding a New Package

1. Create `internal/newpkg/`
2. Add a one-line entry to the project structure in `CLAUDE.md`
3. Add a section to `docs/guides/architecture.md` if the package is non-trivial
4. Write tests alongside the code

---

## Adding a CLI Command

1. Create `cmd/nightshift/commands/mycommand.go`
2. Register it in `cmd/nightshift/commands/root.go` with `rootCmd.AddCommand(myCmd)`
3. Keep the command file thin — parse flags, call `internal/` functions
4. Update `website/docs/cli-reference.md`

---

## Adding a New Dependency

1. Search for the latest stable version online before adding
2. Prefer pure-Go packages (no CGO)
3. `go get github.com/org/pkg@vX.Y.Z`
4. `go mod tidy`
5. Document it in the PR description with the reason for adding it

---

## Pull Requests

- PRs should be **small and focused** — split large changes into sequential PRs
- Every PR must pass: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt`
- Update `CLAUDE.md` and relevant `docs/guides/` in the same PR as any architectural change
- Update `website/docs/` when user-facing behaviour changes
- Update `CHANGELOG.md` for any user-visible change
- Add to `SECURITY_AUDIT.md` if the PR has security implications

---

## Documentation

| What changed | What to update |
|-------------|----------------|
| New internal package | `CLAUDE.md` project structure + `docs/guides/architecture.md` |
| New user-facing feature | `website/docs/` (relevant page + cli-reference) + `README.md` |
| New CLI flag | `website/docs/cli-reference.md` |
| New Jira pipeline phase/behavior | `website/docs/jira.md` + `docs/guides/jira-pipeline.md` |
| Security finding | `SECURITY_AUDIT.md` |
| New gotcha discovered | `CLAUDE.md` Gotchas section |
