# Quick Start

Two flavours — pick what you came for.

- **Jira pipeline** — autonomous ticket implementation. Start here if that's the goal.
- **Maintenance tasks** — run lint / docs / tests / bug-finder on a schedule.

---

## Jira Pipeline (Primary Workflow)

### 1. Install

```bash
brew install marcus/tap/nightshift
```

### 2. Authenticate everything

```bash
# Pick at least one coding agent
claude /login                              # OR: export ANTHROPIC_API_KEY=sk-ant-...
codex --login                              # OR: export OPENAI_API_KEY=sk-...
gh auth login && gh extension install github/gh-copilot

# Jira
export NIGHTSHIFT_JIRA_TOKEN=...           # https://id.atlassian.com/manage-profile/security/api-tokens
```

### 3. Setup

```bash
nightshift setup
```

The wizard walks through providers, schedule, budget, and the Jira block (site, email, project keys, repo SSH URLs, per-phase models).

### 4. Label your tickets

In Jira, add the `nightshift` label to every ticket you want auto-implemented. They must be in a todo-class status.

### 5. Dry-run

```bash
nightshift jira preview                    # which tickets, what phases, budget cost
nightshift jira preview --validate         # also run the LLM scoring on each ticket
nightshift jira preview --explain          # full per-phase token estimate
```

No Jira mutation, no git push. Safe to run any time.

### 6. Go

```bash
nightshift jira run                        # process all labeled todo tickets
nightshift jira run --ticket VC-42         # single ticket
nightshift jira run --max-tickets 5        # cap
nightshift jira run --skip-validation      # save tokens, skip the scoring LLM
```

Or let the systemd timer handle it on schedule:

```bash
nightshift install systemd
systemctl --user enable --now nightshift-jira.timer
```

### 7. Wake up to PRs

```bash
nightshift report list                     # last few runs
nightshift report show <run-id>            # ticket-by-ticket detail
```

Per ticket processed:
- `feature/<KEY>` branch pushed
- PR opened linking the ticket
- Ticket transitioned → "In Review"
- `🤖` comments on the ticket logging each completed phase

See [Jira Pipeline](jira-pipeline.md) for the full lifecycle, config reference, and failure modes.

---

## Maintenance Tasks

### 1–3 same as above (install + auth + setup, no Jira required)

### 4. Preview

```bash
nightshift preview
nightshift budget
```

### 5. Run

```bash
nightshift run                # interactive: preflight + confirm + execute
nightshift run --dry-run      # preflight only
nightshift run --yes          # skip confirm (cron-friendly)
```

### 6. Inspect

```bash
nightshift status --today
nightshift logs --tail
nightshift report list
```

Every change lands as a PR. Merge the keepers, close the rest.

---

## Next

- [Jira Pipeline](jira-pipeline.md) — full Jira reference + failure modes
- [Configuration](configuration.md) — YAML reference
- [Tasks](tasks.md) — what's in the maintenance catalog
- [CLI Reference](cli-reference.md) — every flag
