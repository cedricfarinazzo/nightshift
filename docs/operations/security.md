# Security Model

## Credentials

**Env vars only.** `CredentialManager` in `internal/security/credentials.go` reads:

- `ANTHROPIC_API_KEY`
- `OPENAI_API_KEY`
- `NIGHTSHIFT_JIRA_TOKEN`
- `gh` token (managed by `gh` CLI itself)

Never put secrets in `config.yaml`. Nightshift scans config files at load time for patterns like `sk-`, `token:`, `password:`, `secret:` (`CheckConfigForCredentials`) and refuses to start if it finds them.

For systemd, use `Environment=` directives in a `.service.d/env.conf` drop-in or `EnvironmentFile=` pointing at a `0600` file.

## Audit Log

Append-only JSONL at `~/.local/share/nightshift/audit/audit-YYYY-MM-DD.jsonl`. Permissions `0700` on the directory.

Events captured:

- `agent_start`, `agent_complete`, `agent_error`
- `file_read`, `file_write`, `file_delete`
- `git_commit`, `git_push`
- `security_check`, `security_denied`
- `config_change`
- `budget_check`

Never overwritten. Rotate with logrotate if disk pressure becomes a concern.

## Sandbox

`internal/security/sandbox.go` applies execution restrictions to spawned agent processes. Today this is:

- Restricted working directory (refuse `$HOME`)
- `validateGitRepo` — refuse non-git workdirs
- Branch save/restore in `RunTask` to prevent stacked branches

Agent binaries themselves enforce file-write approval; bypass flags (`dangerously_skip_permissions`, `dangerously_bypass_approvals_and_sandbox`) are explicit opt-in.

## Working Directory Validation

Before executing any task, `orchestrator.RunTask()` calls `validateGitRepo(ctx, workDir)`. Refuses to run if:

- workDir is not inside a git repo
- workDir's repo root is `$HOME`

Prevents accidental `git init` on the wrong directory.

## Branch Save/Restore

`RunTask()` captures `CurrentBranch()` before executing and defers a `git checkout <orig>` to restore it on exit. Skipped if the captured branch is `HEAD` (detached) or if `CurrentBranch` errors.

## PR Hygiene

- Nightshift never force-pushes.
- It never targets `main` directly.
- All changes land on `type/short-description` branches (e.g. `feat/lint-fix-2026-05-19`).

## Git Conventions Enforced

- Conventional Commits (`type(scope): summary`)
- Atomic commits — one logical change per commit
- Never amend; new commits only
- `Refs: TICKET-KEY` in commit body for Jira-driven work

See [CLAUDE.md](../../CLAUDE.md) for the full convention list.

## Filing Vulnerabilities

Email cedric.farinazzo@gmail.com with subject `NIGHTSHIFT SECURITY` rather than opening a public issue.
