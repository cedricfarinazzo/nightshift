# Testing

## Targets

```bash
make test          # go test ./...
make test-verbose  # -v
make test-race     # -race
make coverage      # coverage report
```

## Conventions

- Table-driven tests preferred
- `_test.go` files alongside the code they test
- Use the `CommandRunner` interface to test agent code — never call `exec.Command` directly in production paths
- No global test state; each test is self-contained

## `CommandRunner` / `MockRunner`

```go
type CommandRunner interface {
    Run(ctx context.Context, name string, args []string, stdin io.Reader) (stdout, stderr string, exitCode int, err error)
}
```

In production: default impl wraps `exec.CommandContext`.

In tests:

```go
runner := &agents.MockRunner{
    Output: `{"status":"ok","tokens":123}`,
    ExitCode: 0,
}
agent := agents.NewClaudeAgent(agents.WithRunner(runner))
```

`MockRunner` records calls so you can assert flags, model, prompt path, etc.

## Jira tests

`internal/jira/orchestrator_test.go` uses `stubJiraClient`, a struct satisfying the `jiraClient` interface in `Orchestrator`:

```go
type stubJiraClient struct {
    issues map[string]Ticket
    transitions []string
    comments []string
}
```

Inject via `orchestrator.WithClient(stub)`.

`*Client` satisfies `jiraClient` implicitly — no embedding required.

## httptest for go-atlassian

`go-atlassian/v2` allows pointing at a custom base URL. Tests use `httptest.NewServer` to serve canned JSON for `/rest/api/2/...` and `/rest/agile/1.0/...` endpoints.

Non-obvious endpoint shapes:

- `Project.Statuses` returns nested issue-type buckets
- `SearchJQL` uses `/rest/api/2/search/jql` (not `/search`)
- `Board.Issues` returns ALL board items (including backlog)
- `Board.Backlog` returns backlog only — subtract to get on-board issues

## e2e tests

`internal/jira/e2e_test.go` and `cmd/nightshift/commands/jira_run_e2e_test.go` hit real Jira if `NIGHTSHIFT_JIRA_TOKEN` is set:

```go
func TestE2E(t *testing.T) {
    if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
        t.Skip("NIGHTSHIFT_JIRA_TOKEN not set")
    }
    ...
}
```

Required for full coverage but skipped by default — CI runs them only on tagged branches with the secret.

## Coverage target

`internal/jira/` requires ≥75% unit coverage (excludes e2e). Check with:

```bash
go test -cover -run 'Test[^E]' ./internal/jira/...
```

## Exec runner injection

Preferred pattern for injecting external-command dependencies. `internal/analysis` is the reference implementation.

Define a narrow interface in the package that uses it:

```go
type GitRunner interface {
    Run(repoPath string, args ...string) ([]byte, error)
}
```

Default production impl:

```go
type execGitRunner struct{}
func (execGitRunner) Run(repoPath string, args ...string) ([]byte, error) {
    cmd := exec.Command("git", args...)
    cmd.Dir = repoPath
    return cmd.Output()
}
```

Wire via functional option so existing callers keep compiling:

```go
type Option func(*GitParser)

func WithGitRunner(r GitRunner) Option {
    return func(gp *GitParser) { gp.runner = r }
}

func NewGitParser(repoPath string, opts ...Option) *GitParser {
    gp := &GitParser{repoPath: repoPath, runner: execGitRunner{}}
    for _, o := range opts { o(gp) }
    return gp
}
```

In tests — no global mutation, safe for `t.Parallel()`:

```go
type fakeGitRunner struct{ out []byte; err error }
func (f fakeGitRunner) Run(_ string, _ ...string) ([]byte, error) { return f.out, f.err }

func TestSomething(t *testing.T) {
    t.Parallel()
    fake := fakeGitRunner{out: []byte("Alice|alice@a.com\n")}
    parser := analysis.NewGitParser("/fake", analysis.WithGitRunner(fake))
    ...
}
```

Packages migrated to this pattern:
- `internal/analysis` — `GitRunner` / `WithGitRunner` (VC-100, reference implementation)
- `internal/jira` — `GHRunner` / `WithGHRunner` / `PRClient` (VC-106); `t.Parallel()` is safe in most PR tests. Exception: `TestFetchPRReviewComments_ReviewThreadsError` captures `os.Stderr` to assert warning log output and must remain non-parallel.
- `internal/workspace` — `GitRunner` / `WithGitRunner` as a `SetupWorkspace` option (VC-107); `t.Parallel()` is safe.
- `internal/orchestrator` — `GitRunner` / `WithGitRunner` + `GhRunner` / `WithGhRunner` on the `Orchestrator` struct (VC-108); `t.Parallel()` is safe. `validateGitRepo`, `checkoutBranch`, `currentBranch` are unexported methods using `o.git`. The exported `CurrentBranch` package function uses a local `execGitRunner{}` for `cmd/` callers.

All `internal/*` packages above are now seam-free — no package-level func-valued vars used as test seams remain.

### internal/jira PR tests

Use `NewPRClient(WithGHRunner(fakeGHRunner(...)))` and call the method under test:

```go
type fakeGHRunner func(ctx context.Context, repoPath string, args ...string) (string, error)

func (f fakeGHRunner) Run(ctx context.Context, repoPath string, args ...string) (string, error) {
    return f(ctx, repoPath, args...)
}

func TestSomething(t *testing.T) {
    t.Parallel()
    pc := NewPRClient(WithGHRunner(fakeGHRunner(func(_ context.Context, _ string, args ...string) (string, error) {
        return `[{"number":1,"url":"https://github.com/x/y/pull/1"}]`, nil
    })))
    pr, err := pc.findExistingPR(context.Background(), "/repo", "feature/branch")
    ...
}
```

## Time control

For tests touching cooldowns, intervals, or scheduling, inject a `clock` via dependency injection rather than calling `time.Now()` in hot paths. Most current code uses `time.Now()` directly — refactor if you need determinism.

## Race detector

`make test-race` should pass clean. The orchestrator + state packages have explicit concurrency contracts; any new shared state needs sync primitives.

## Test data placement

- Tiny fixtures inline as Go string literals
- Larger fixtures in `testdata/` subdir under the package (Go ignores `testdata/` during build)
