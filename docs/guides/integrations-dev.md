# Adding Integrations

How the integration system works and how to add a new integration source.

---

## Overview

Integrations feed two things into agent prompts:
- **Context** — freeform text injected into the prompt (e.g., `CLAUDE.md` content, project conventions)
- **Tasks** — work items from external sources (e.g., `td` tasks, GitHub issues)
- **Hints** — lightweight structured suggestions (task suggestions, conventions, constraints)

All integrations are coordinated by `internal/integrations/Manager`.

---

## The `Reader` Interface

```go
type Reader interface {
    Name()    string                                                       // e.g. "claude_md", "td"
    Enabled() bool                                                         // from config
    Read(ctx context.Context, projectPath string) (*Result, error)        // load data
}
```

`Read` returns `nil, nil` if the source doesn't exist (not an error — e.g. no `CLAUDE.md` file).

The `Result` type:

```go
type Result struct {
    Context  string            // Text injected into prompts
    Tasks    []TaskItem        // Work items
    Hints    []Hint            // Structured suggestions
    Metadata map[string]any    // Source-specific data
}
```

---

## Existing Integrations

| Reader | File | Config key | What it does |
|--------|------|-----------|-------------|
| `ClaudeMDReader` | `claudemd.go` | `integrations.claude_md` | Reads `CLAUDE.md` from project root |
| `AgentsMDReader` | `agentsmd.go` | — (always attempted) | Reads `AGENTS.md`; returns nil if absent |
| `TDReader` | `td.go` | `integrations.task_sources[].td` | Queries `td` CLI for tagged tasks |
| `GitHubReader` | `github.go` | `integrations.github_issues` | Reads labeled GitHub issues as tasks |

---

## Adding a New Integration

### 1. Create the reader file

Create `internal/integrations/myintegration.go`:

```go
package integrations

import (
    "context"
    "github.com/marcus/nightshift/internal/config"
)

// MyReader reads tasks from MyService.
type MyReader struct {
    cfg *config.Config
}

// NewMyReader creates a MyReader from config.
func NewMyReader(cfg *config.Config) *MyReader {
    return &MyReader{cfg: cfg}
}

// Name returns the integration identifier.
func (r *MyReader) Name() string { return "myservice" }

// Enabled returns true if the integration is configured.
func (r *MyReader) Enabled() bool {
    return r.cfg.Integrations.MyService.Enabled
}

// Read loads tasks from MyService for the given project.
func (r *MyReader) Read(ctx context.Context, projectPath string) (*Result, error) {
    if !r.Enabled() {
        return nil, nil
    }

    // ... fetch data from external service ...

    return &Result{
        Context: "Project tasks from MyService:\n" + summary,
        Tasks: []TaskItem{
            {
                ID:          "myservice-123",
                Title:       "Fix the thing",
                Description: "Details...",
                Priority:    5,
                Source:      "myservice",
            },
        },
    }, nil
}
```

### 2. Add config fields

In `internal/config/config.go`, add to `IntegrationsConfig`:

```go
type IntegrationsConfig struct {
    // ... existing fields ...
    MyService MyServiceConfig `mapstructure:"my_service" yaml:"my_service"`
}

type MyServiceConfig struct {
    Enabled bool   `mapstructure:"enabled" yaml:"enabled"`
    Token   string `mapstructure:"token_env" yaml:"token_env"` // env var name for auth
}
```

### 3. Register in `Manager`

In `internal/integrations/integrations.go`, add to `NewManager`:

```go
func NewManager(cfg *config.Config) *Manager {
    m := &Manager{config: cfg}
    m.readers = append(m.readers, NewClaudeMDReader(cfg))
    m.readers = append(m.readers, NewAgentsMDReader(cfg))
    m.readers = append(m.readers, NewTDReader(cfg))
    m.readers = append(m.readers, NewGitHubReader(cfg))
    m.readers = append(m.readers, NewMyReader(cfg))   // ← add here
    return m
}
```

### 4. Write tests

In `internal/integrations/myintegration_test.go`:

```go
func TestMyReader_Enabled(t *testing.T) { ... }
func TestMyReader_Read_disabled(t *testing.T) { ... }
func TestMyReader_Read_success(t *testing.T) { ... }
func TestMyReader_Read_sourceAbsent(t *testing.T) { ... }  // returns nil, nil
```

### 5. Update docs

- Add a row to the integration table in `docs/guides/integrations-dev.md`
- Add a section to `website/docs/integrations.md`
- Add the config block to `website/docs/configuration.md`
- Update `CLAUDE.md` project structure if adding a new file

---

## Hint Types

Use `Hint` to pass structured suggestions to the task selector — they influence task priority:

```go
Hints: []Hint{
    {
        Type:    HintTaskSuggestion,  // suggests a specific task type
        Content: "lint-fix",          // task type name
        Source:  "myservice",
    },
    {
        Type:    HintConvention,
        Content: "All functions must have docstrings",
        Source:  "myservice",
    },
},
```

A `HintTaskSuggestion` hint for a task type gives that task a priority bonus (+2 in the selector).

---

## How Context Is Injected

`Manager.ReadAll()` builds `AggregatedResult.CombinedContext` by concatenating each reader's context with a markdown `## ReaderName` header:

```
## claude_md
[contents of CLAUDE.md]

## td
[td task summaries]
```

This combined context is appended to the agent prompt by the task orchestrator.
