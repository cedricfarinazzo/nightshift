# Jira Pipeline

Nightshift can autonomously implement Jira tickets: validate → plan → implement → commit → PR → status transition. Driven by `nightshift jira run`.

## What It Does

For each labeled ticket in TODO status:

1. **Validate** (LLM, score ≥6 = valid). Invalid tickets get a comment + transition to "Needs Info".
2. **Setup workspace.** Clone repo (or reuse), checkout feature branch.
3. **Plan.** LLM writes a structured plan; posted as a Jira comment with `🤖` prefix.
4. **Implement.** LLM edits files. Uses the plan as input.
5. **Commit.** Conventional commit, `Refs: TICKET-KEY` in body.
6. **PR.** Creates/updates GitHub PR via `gh`.
7. **Status.** Transitions ticket to "In Review".

For tickets already in review, `ProcessFeedback` fetches new PR review comments and produces a fix commit.

## Resume

Each phase posts a `🤖 <!-- nightshift:type=... -->` comment. On the next run, `detectResumeState` reads these markers and skips completed phases. To force a full restart, move the ticket back to TODO.

## Config

```yaml
jira:
  site: yourorg                          # yourorg.atlassian.net
  email: you@example.com
  token_env: NIGHTSHIFT_JIRA_TOKEN       # env var holding the API token

  validation:
    provider: copilot
    model: gpt-5.4-mini
    timeout: 2m
  plan:
    provider: copilot
    model: claude-sonnet-4.6
    timeout: 5m
  implement:
    provider: copilot
    model: claude-sonnet-4.6
    timeout: 30m
  review_fix:
    provider: copilot
    model: gpt-5.4-mini
    timeout: 20m

  budget_enabled: true
  max_tickets: 10

  projects:
    - key: VC
      label: nightshift
      repos:
        - name: nightshift
          url: git@github.com:org/nightshift.git    # SSH required
          base_branch: main

    - key: INFRA
      label: nightshift
      repos:
        - name: infra
          url: git@github.com:org/infra.git
          base_branch: trunk
      implement:
        timeout: 45m                                 # per-project override
```

> **SSH required.** HTTPS remotes fail silently in non-interactive contexts (`fatal: could not read Username`). Always use `git@github.com:...`.

## Sprint / Backlog Filtering

```yaml
jira:
  projects:
    - key: VC
      require_active_sprint: true       # only tickets in an active sprint
      board_type: kanban
      board_id: 42                      # required for kanban-board filtering
```

- `require_active_sprint: true` adds `AND sprint in openSprints()` to JQL. Requires Jira Software (not Work Management).
- For Kanban, the JQL alone can't separate board from backlog. `board_id` + `board_type: kanban` fetches via `/rest/agile/1.0/board/<id>/issue` minus `/board/<id>/backlog`.

## Environment

| Var | Purpose |
|-----|---------|
| `NIGHTSHIFT_JIRA_TOKEN` | Jira API token |
| `ANTHROPIC_API_KEY` | Claude provider |
| `OPENAI_API_KEY` | Codex provider |

## CLI

```bash
nightshift jira run                          # all labeled tickets
nightshift jira run --ticket VC-42           # single ticket
nightshift jira run --max-tickets 5
nightshift jira run --skip-validation        # skip LLM validation
nightshift jira run --todo-only              # skip review-feedback handling
nightshift jira run --review-only            # only handle review feedback

nightshift jira preview                      # dry-run summary
nightshift jira preview --validate           # also run validation LLM
nightshift jira preview --explain
```

## See Also

- [Jira Pipeline Internals](../dev/jira-pipeline.md) — phase state machine, comment markers, dependency resolution
- [Workspace Management](../dev/workspace.md) — clone/branch conventions
