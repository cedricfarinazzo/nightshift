# Security Model

This guide describes how Nightshift manages credentials, restricts agent execution, and maintains an audit trail.

---

## Credentials

**Rule: all credentials come from environment variables only. Never from config files. Never from disk.**

`internal/security/credentials.go` enforces this:

```go
const (
    EnvAnthropicKey = "ANTHROPIC_API_KEY"
    EnvOpenAIKey    = "OPENAI_API_KEY"
)
```

`CredentialManager` reads these at runtime, validates they are present, and masks them for display (`sk-ant-...abc` format). It never stores values beyond the process lifetime.

Required environment variables:

| Variable | Required for |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Claude provider |
| `OPENAI_API_KEY` | Codex provider |
| `NIGHTSHIFT_JIRA_TOKEN` | Jira pipeline |
| GitHub token (via `gh auth login`) | PR creation, Copilot |

At startup, `nightshift doctor` runs `CredentialManager.ValidateAll()` and reports the status of each.

### What NOT to do

- Do not add credential fields to `Config` struct in `internal/config/config.go`
- Do not read credentials in any package other than `internal/security/credentials.go`
- Do not log credential values — always mask before logging

---

## Audit Log

Every significant operation is written to an append-only JSONL audit log:

```
~/.local/share/nightshift/audit/audit-YYYY-MM-DD.jsonl
```

Directory permissions: `0700` (owner read/write/execute only).

### Event types

| Event | When |
|-------|------|
| `agent_start` | Agent subprocess launched |
| `agent_complete` | Agent exited successfully |
| `agent_error` | Agent exited with error |
| `file_read` / `file_write` / `file_delete` | File operations by agents |
| `git_commit` / `git_push` / `git_operation` | Git operations |
| `security_check` / `security_denied` | Access control decisions |
| `config_change` | Config file modified |
| `budget_check` | Budget evaluated |

### Log entry structure

```json
{
  "timestamp": "2026-05-01T13:00:00Z",
  "event_type": "agent_start",
  "agent": "claude",
  "task_id": "lint-fix",
  "project": "/home/user/myrepo",
  "action": "execute",
  "request_id": "abc123",
  "session_id": "def456"
}
```

### Writing audit events

Use `AuditLogger` from `internal/security/audit.go`:

```go
logger, err := security.NewAuditLogger(auditDir)
if err != nil { return err }

logger.Log(security.AuditEvent{
    EventType: security.AuditAgentStart,
    Agent:     "claude",
    TaskID:    task.Name,
    Project:   project.Path,
    Action:    "execute",
})
```

---

## Sandbox

`internal/security/sandbox.go` provides an isolated execution environment for agents.

```go
type SandboxConfig struct {
    WorkDir      string
    AllowNetwork bool            // default: false
    AllowedPaths []string
    DeniedPaths  []string
    MaxDuration  time.Duration   // default: 30 minutes
    MaxMemoryMB  int             // default: 0 (unlimited)
    Environment  map[string]string
    Cleanup      bool            // default: true
}
```

The sandbox:
- Creates a temp working directory
- Controls which environment variables are passed through
- Kills processes (entire process group) on timeout
- Cleans up temp files after execution

The default config (`DefaultSandboxConfig()`) disallows network access and enforces the 30-minute timeout.

For the Jira implement phase, `AllowNetwork: true` is required (agent needs to read GitHub/docs).

---

## Security Audit

`SECURITY_AUDIT.md` in the root tracks known security findings, mitigations, and open items. Update it whenever:
- A new security-relevant feature is added
- A vulnerability is found and fixed
- A mitigation is put in place

---

## Principles

1. **Least privilege**: agents run with the minimum permissions needed for their task
2. **Credentials in env only**: no secrets on disk outside standard OS credential stores
3. **Immutable audit trail**: audit log is append-only; never delete or modify it
4. **No eval / no dynamic commands**: all agent subprocess commands are built from static arg lists, never from user-controlled string interpolation
5. **Process isolation**: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` ensures timeout kills the whole process tree, not just the shell wrapper
