# Nightshift

> It finds what you forgot to look for.

![Nightshift logo](logo.png)

Nightshift is a Go CLI that orchestrates AI coding agents (Claude Code, Codex, GitHub Copilot) to work on your repos overnight.

**Headline feature — autonomous Jira pipeline.** Point it at a project + label and Nightshift fetches todo tickets, validates each one with an LLM, resolves cross-ticket dependencies (topo-sorts via `BuildDependencyGraph`), sets up a workspace per repo, and drives every ticket through:

```
validate → plan → implement → commit → PR → status transition
```

Five of those six phases (all except commit) post a `🤖 <!-- nightshift:type=… -->` comment to the Jira ticket on success. Failures post a `🤖 error` comment instead. On the next run, `detectResumeState` reads those markers and resumes from the next unfinished phase — interrupted runs **never restart from scratch**. PR review comments on tickets already "In Review" are picked up automatically the next run and fed back to the agent as a rework prompt; `lastReworkAt` makes the feedback loop idempotent.

It also ships a catalog of ~60 maintenance tasks (lint, doc backfill, test gaps, bug-finder, security audits, bus-factor, dependency audits, ...) that run on a schedule against your repos using leftover provider budget.

Everything lands as a branch or PR. Nightshift never writes directly to your primary branch. Don't like a change? Close the PR. That's the entire rollback plan.

---

## Quick Start — Jira

```bash
brew install marcus/tap/nightshift
nightshift setup                       # walks through providers + jira block
export NIGHTSHIFT_JIRA_TOKEN=...        # atlassian API token
nightshift jira preview                # dry-run: see what would happen
nightshift jira run                    # process all labeled todo tickets
```

Run `nightshift daemon start` to do this on a schedule. Full walkthrough in [`docs/user/quick-start.md`](docs/user/quick-start.md); full Jira reference in [`docs/user/jira-pipeline.md`](docs/user/jira-pipeline.md).

## Quick Start — Maintenance Tasks

```bash
nightshift preview                     # next scheduled runs
nightshift run                         # preflight + confirm + execute
```

---

## Features

- **Autonomous Jira pipeline** — validate → plan → implement → commit → PR → status, end-to-end, resumable via `🤖` comment markers, PR-review feedback loop included
- **Multi-repo per project** — a single Jira project can target multiple repos; each gets its own workspace and branch
- **Budget-aware** — live provider Usage API checks gate every run; single knob `max_percent`
- **Multi-agent** — Claude Code, Codex, or GitHub Copilot; per-phase provider/model selection in Jira config
- **~60 maintenance tasks** — lint, docs, tests, refactors, bug-finder, security audits
- **Zero-risk** — every change is a branch + PR; merge what you like, close the rest
- **Bus-factor analysis** — quantifies code ownership concentration and flags single-contributor risk
- **No CGO, no cloud** — pure-Go SQLite, all state on disk

---

## Installation

| Method | Command |
|--------|---------|
| Homebrew | `brew install marcus/tap/nightshift` |
| Go install | `go install github.com/cedricfarinazzo/nightshift/cmd/nightshift@latest` |
| Source | `git clone https://github.com/cedricfarinazzo/nightshift && cd nightshift && make build` |
| Binary | [GitHub releases](https://github.com/cedricfarinazzo/nightshift/releases) — darwin/linux × amd64/arm64 |

Verify: `nightshift --version && nightshift doctor`.

Full guide: [`docs/user/installation.md`](docs/user/installation.md).

---

## Agent Authentication

At least one agent CLI must be installed + authenticated.

```bash
# Claude
claude /login                                # or: export ANTHROPIC_API_KEY=sk-ant-...

# Codex
codex --login                                # or: export OPENAI_API_KEY=sk-...

# GitHub Copilot
gh extension install github/gh-copilot
gh auth login                                # requires Copilot subscription
```

Details: [`docs/user/agents.md`](docs/user/agents.md).

---

## Common Commands

### Jira pipeline

```bash
nightshift jira preview                  # dry-run: tickets, phase plan, budget
nightshift jira preview --validate       # also run LLM scoring per ticket
nightshift jira preview --explain        # per-phase token estimate

nightshift jira run                      # process all labeled todo tickets
nightshift jira run --ticket VC-42       # one ticket
nightshift jira run --max-tickets 5
nightshift jira run --skip-validation    # save tokens, skip the LLM scoring phase
nightshift jira run --review-only        # only handle PR review feedback
nightshift jira run --todo-only          # skip review-feedback loop
```

### Maintenance tasks

```bash
nightshift run                    # one run now (preflight + confirm + execute)
nightshift run --dry-run          # show what would run, no execution
nightshift run --yes              # skip confirmation

nightshift preview                # upcoming scheduled runs
nightshift budget                 # current provider capacity

nightshift task list              # browse the catalog
nightshift task show lint-fix
nightshift task run lint-fix --provider claude
```

### Operations

```bash
nightshift daemon start           # background scheduler
nightshift doctor                 # check env + credentials + config
nightshift status                 # recent run state
nightshift report list            # browse run reports
nightshift busfactor              # code ownership analysis
```

Full reference: [`docs/user/cli-reference.md`](docs/user/cli-reference.md).

---

## Kubernetes / Cloud Deployment

Run nightshift as a Kubernetes CronJob using the included manifests:

```bash
# Create namespace and secrets
kubectl create namespace nightshift
kubectl create secret generic nightshift-secrets \
  --namespace nightshift \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-... \
  --from-literal=NIGHTSHIFT_JIRA_TOKEN=...

# Apply all manifests (Kustomize)
kubectl apply -k deploy/kubernetes/
```

Fires at 02:00 UTC daily. Override the schedule, image tag, or resource limits via a Kustomize overlay. Full guide: [`docs/operations/kubernetes-cronjob.md`](docs/operations/kubernetes-cronjob.md).

---

## Minimal Configuration

`~/.config/nightshift/config.yaml`:

```yaml
schedule:
  cron: "0 2 * * *"

budget:
  max_percent: 90

providers:
  preference: [claude, codex]
  claude:
    enabled: true
    dangerously_skip_permissions: true
  codex:
    enabled: true
    dangerously_bypass_approvals_and_sandbox: true

projects:
  - path: ~/code/myrepo

# Optional: autonomous Jira pipeline
jira:
  site: yourorg                              # yourorg.atlassian.net
  email: you@example.com
  token_env: NIGHTSHIFT_JIRA_TOKEN
  max_tickets: 10
  validation: { provider: copilot, model: gpt-5.4-mini,        timeout: 2m  }
  plan:       { provider: copilot, model: claude-sonnet-4.6,   timeout: 5m  }
  implement:  { provider: copilot, model: claude-sonnet-4.6,   timeout: 30m }
  review_fix: { provider: copilot, model: gpt-5.4-mini,        timeout: 20m }
  projects:
    - key: VC
      label: nightshift
      repos:
        - name: nightshift
          url: git@github.com:org/nightshift.git    # SSH required
          base_branch: main
```

Full reference: [`docs/user/configuration.md`](docs/user/configuration.md). Jira-specific reference: [`docs/user/jira-pipeline.md`](docs/user/jira-pipeline.md). Run `nightshift setup` for an interactive wizard.

---

## Documentation

All docs live in [`docs/`](docs/) as plain markdown.

**User guides** ([`docs/user/`](docs/user/)) — introduction, installation, quick start, configuration, CLI reference, tasks, agents, Jira pipeline, budget, scheduling, bus factor, troubleshooting.

**Operations** ([`docs/operations/`](docs/operations/)) — daemon mode, systemd install, logs and reports, data and backup, security model, release process.

**Developer / internal** ([`docs/dev/`](docs/dev/)) — architecture, run lifecycle, orchestrator, agents internals, Jira pipeline internals, workspace, database, budget internals, tasks internals, state and snapshots, scheduling, reporting, logging, bus factor, testing, contributing, debugging.

Index: [`docs/README.md`](docs/README.md).

---

## Development

```bash
make install-hooks     # symlinks scripts/pre-commit.sh
make build             # go build -o nightshift ./cmd/nightshift
make test              # go test ./...
make test-race
make lint              # requires golangci-lint
```

See [`docs/dev/contributing.md`](docs/dev/contributing.md) for conventions, commit style, and the PR checklist.

---

## Uninstalling

```bash
nightshift uninstall                                  # removes systemd unit
rm -rf ~/.config/nightshift ~/.local/share/nightshift # config + data
rm "$(which nightshift)"
```

---

## License

MIT — see [`LICENSE`](LICENSE).
