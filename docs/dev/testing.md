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

Packages still using package-level vars (`ghExec` in `internal/jira/pr.go`, `gitExec` in `internal/orchestrator`, `gitExecFn`/`SetGitExecFn` in `internal/workspace`) are tracked for migration — see follow-up ticket VC-101.

### Legacy: ghExec (internal/jira/pr.go — not yet migrated)

PR creation tests stub the `gh` CLI:

```go
oldExec := ghExec
defer func() { ghExec = oldExec }()
ghExec = func(ctx context.Context, args ...string) (string, error) {
    return `{"url":"https://github.com/x/y/pull/1"}`, nil
}
```

**Do not add `t.Parallel()` to tests using this pattern** — they race on the shared global. Migrate to interface injection (see above) before parallelising.

## Time control

For tests touching cooldowns, intervals, or scheduling, inject a `clock` via dependency injection rather than calling `time.Now()` in hot paths. Most current code uses `time.Now()` directly — refactor if you need determinism.

## Race detector

`make test-race` should pass clean. The orchestrator + state + scheduler have explicit concurrency contracts; any new shared state needs sync primitives.

## Test data placement

- Tiny fixtures inline as Go string literals
- Larger fixtures in `testdata/` subdir under the package (Go ignores `testdata/` during build)
