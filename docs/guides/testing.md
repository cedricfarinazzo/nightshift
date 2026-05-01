# Testing Guide

This guide covers how to run the test suite, patterns used throughout the codebase, and how to write new tests.

---

## Running Tests

```bash
make test              # go test ./...
make test-verbose      # go test -v ./...
make test-race         # go test -race ./...  (race detector)
make coverage          # coverage report to stdout
make coverage-html     # open HTML coverage report
```

Run tests for a single package:

```bash
go test ./internal/jira/...
go test ./internal/agents/...
go test -run TestProcessTicket ./internal/jira/
```

---

## Test Patterns

### Table-driven tests

All tests use Go's standard table-driven pattern:

```go
func TestBudgetAvailable(t *testing.T) {
    tests := []struct {
        name     string
        budget   Budget
        expected int64
    }{
        {
            name:     "daily mode basic",
            budget:   Budget{Weekly: 100_000, Mode: "daily"},
            expected: 12_142,
        },
        {
            name:     "weekly mode no reserve",
            budget:   Budget{Weekly: 100_000, Mode: "weekly"},
            expected: 75_000,
        },
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            got := tc.budget.Available()
            if got != tc.expected {
                t.Errorf("got %d, want %d", got, tc.expected)
            }
        })
    }
}
```

### Mocking the CommandRunner

Agents use a `CommandRunner` interface to allow substitution in tests. Never call `exec.Command` directly in agent code.

```go
// MockRunner captures what was passed to it and returns canned output.
type MockRunner struct {
    Stdout   string
    Stderr   string
    ExitCode int
    Err      error
    Delay    time.Duration

    CapturedName  string
    CapturedArgs  []string
    CapturedDir   string
    CapturedStdin string
}

func (m *MockRunner) Run(ctx context.Context, name string, args []string, dir, stdin string) (string, string, int, error) {
    m.CapturedName  = name
    m.CapturedArgs  = args
    m.CapturedDir   = dir
    m.CapturedStdin = stdin
    if m.Delay > 0 { time.Sleep(m.Delay) }
    return m.Stdout, m.Stderr, m.ExitCode, m.Err
}
```

Usage:

```go
runner := &MockRunner{Stdout: "done", ExitCode: 0}
agent  := NewClaudeAgent(WithRunner(runner))
result, err := agent.Execute(ctx, ExecuteOptions{Prompt: "fix lint"})
// assert on result.Output, runner.CapturedArgs, etc.
```

### Mocking the Jira client

`Orchestrator` accepts a `jiraClient` interface, not the concrete `*Client`. Use `stubJiraClient` in tests:

```go
type stubJiraClient struct {
    postCommentCalls []NightshiftComment
    transitionCalls  []string
    postCommentErr   error
}

func (s *stubJiraClient) PostComment(_ context.Context, key string, c NightshiftComment) error {
    s.postCommentCalls = append(s.postCommentCalls, c)
    return s.postCommentErr
}
// implement remaining interface methods ...
```

Construct the orchestrator with the stub:

```go
o := &Orchestrator{
    cfg:         cfg,
    jiraClient:  &stubJiraClient{},
    planAgent:   agents.NewClaudeAgent(agents.WithRunner(runner)),
    implAgent:   agents.NewClaudeAgent(agents.WithRunner(runner)),
}
result := o.ProcessTicket(ctx, ticket, workspace)
```

### Mocking the PR/git functions

Package-level `var` hooks in `internal/jira/pr.go` and `internal/jira/branch.go` allow test substitution without subprocesses:

```go
// Save and restore in t.Cleanup
origGhExec := ghExec
t.Cleanup(func() { ghExec = origGhExec })
ghExec = func(args ...string) (string, error) {
    return `{"url":"https://github.com/org/repo/pull/1"}`, nil
}
```

---

## In-memory SQLite for DB tests

Use `:memory:` to avoid touching the filesystem:

```go
db, err := db.Open(":memory:")
if err != nil {
    t.Fatal(err)
}
defer db.Close()
```

`Open(":memory:")` still runs all migrations, giving you a fully-migrated schema in-memory.

---

## End-to-end Tests

E2e tests live in `internal/jira/e2e_test.go` and require real credentials. They are **skipped automatically** when `NIGHTSHIFT_JIRA_TOKEN` is not set:

```go
func e2eClient(t *testing.T) *Client {
    t.Helper()
    if os.Getenv("NIGHTSHIFT_JIRA_TOKEN") == "" {
        t.Skip("NIGHTSHIFT_JIRA_TOKEN not set; skipping e2e test")
    }
    // ...
}
```

To run them locally:

```bash
export NIGHTSHIFT_JIRA_TOKEN=your-api-token
go test -v -run TestE2E ./internal/jira/
```

E2e tests are **never run in CI** (no token in CI environment).

---

## Test File Placement

Tests live in `_test.go` files **alongside** the code they test, in the same package:

```
internal/agents/
  claude.go
  claude_test.go   ← same package
  codex.go
  codex_test.go
```

Do not create a separate `tests/` directory.

---

## What to Test

- **Happy path** — normal successful execution
- **Error paths** — command failures, timeouts, missing binaries
- **Edge cases** — empty input, nil fields, zero values
- **Idempotency** — running the same operation twice produces the same result (important for Jira resume logic)
- **Timeout propagation** — context cancellation reaches child processes

---

## Coverage

To find under-covered packages:

```bash
make coverage | sort -t% -k1 -n | head -20
```

Target: keep critical packages (`internal/jira/`, `internal/budget/`, `internal/agents/`) above 70%.
