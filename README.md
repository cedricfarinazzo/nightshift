# Nightshift

> It finds what you forgot to look for.

![Nightshift logo](logo.png)

Nightshift is a Go CLI that orchestrates AI coding agents (Claude Code, Codex, GitHub Copilot) to work on your repos overnight using your remaining provider budget. It finds dead code, doc drift, test gaps, security issues, and dozens of other things silently accumulating while you ship features.

It also ships an **autonomous Jira pipeline**: point it at a project + label, and Nightshift fetches todo tickets, validates them with an LLM, resolves cross-ticket dependencies, sets up a workspace per repo, then drives each ticket through `validate → plan → implement → commit → PR → status transition` — fully overnight. The validate, plan, implement, PR, and status-change phases each post a `🤖` comment to the ticket (failures post an error comment instead); the commit phase is silent. On the next run, `detectResumeState` reads those markers and skips straight to the next unfinished phase — interrupted runs never restart from scratch. PR review comments on tickets in "In Review" are picked up the next run and fed back to the agent as a rework prompt.

Everything lands as a branch or PR. Nightshift never writes directly to your primary branch. Don't like a change? Close the PR. That's the entire rollback plan.

---

## Quick Start

```bash
brew install marcus/tap/nightshift
nightshift setup
nightshift preview
nightshift run
```

See [`docs/user/quick-start.md`](docs/user/quick-start.md) for the 2-minute walkthrough.

---

## Features

- **Budget-aware** — live provider Usage API checks gate every run; single knob `max_percent`
- **Multi-agent** — Claude Code, Codex, or GitHub Copilot; preference-ordered fallback
- **Multi-project** — point it at any number of repos
- **Zero-risk** — every change is a PR; merge what you like, close the rest
- **Autonomous Jira pipeline** — validates, plans, codes, commits, PRs, and transitions Jira tickets overnight
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

```bash
nightshift run                    # one run now (preflight + confirm + execute)
nightshift run --dry-run          # show what would run, no execution
nightshift run --yes              # skip confirmation

nightshift preview                # upcoming scheduled runs
nightshift budget                 # current provider capacity

nightshift task list              # browse the catalog
nightshift task show lint-fix
nightshift task run lint-fix --provider claude

nightshift jira run               # process all labeled Jira tickets
nightshift jira preview           # dry-run summary

nightshift busfactor              # code ownership analysis
nightshift daemon start           # background scheduler

nightshift doctor                 # check env + credentials + config
nightshift status                 # recent run state
nightshift report list            # browse run reports
```

Full reference: [`docs/user/cli-reference.md`](docs/user/cli-reference.md).

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
```

Full reference: [`docs/user/configuration.md`](docs/user/configuration.md). Run `nightshift setup` for an interactive wizard.

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
