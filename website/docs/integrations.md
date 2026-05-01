---
sidebar_position: 9
title: Integrations
---

# Integrations

Nightshift integrates with your existing development workflow.

## Claude Code

Nightshift uses the Claude Code CLI to execute tasks. See [Agent Integrations](/docs/agents#claude-code) for installation, authentication, CLI flags, and config options.

## Codex

Nightshift supports OpenAI's Codex CLI as an alternative provider. See [Agent Integrations](/docs/agents#codex) for setup details.

## GitHub Copilot

Nightshift supports GitHub Copilot via `gh copilot` or standalone `copilot` binary. See [Agent Integrations](/docs/agents#github-copilot) for setup details.


## GitHub

All output is PR-based. Nightshift creates branches and pull requests for its findings. The GitHub integration also supports sourcing tasks from issues:

```yaml
integrations:
  github_issues:
    enabled: true
    label: "nightshift"
```

Issues labeled `nightshift` are picked up as tasks. The agent reads the issue body and comments for context.

## Jira Autonomous Pipeline

Nightshift can autonomously implement Jira tickets. Enable by configuring the `jira:` block:

```yaml
jira:
  site: "https://yourorg.atlassian.net"
  project: "PROJ"
  label: "nightshift"
  repos:
    - name: myrepo
      url: "git@github.com:org/myrepo.git"
```

Set `NIGHTSHIFT_JIRA_TOKEN` as an environment variable.

Tickets labeled `nightshift` move through: **validate → plan → implement → commit → PR → status transition**. Progress is posted as comments on the Jira ticket. See [Jira Pipeline](/docs/jira) for full documentation.

## td (Task Management)

Nightshift can source tasks from [td](https://td.haplab.com) — task management for AI-assisted development. Tasks tagged with `nightshift` in td will be picked up automatically.

```yaml
integrations:
  task_sources:
    - td:
        enabled: true
        teach_agent: true   # Include td usage + core workflow in prompts
```

## CLAUDE.md / AGENTS.md

Nightshift reads project-level instruction files to understand context when executing tasks. Place a `CLAUDE.md` or `AGENTS.md` in your repo root to give Nightshift project-specific guidance. Tasks mentioned in these files get a priority bonus (+2).

```yaml
integrations:
  claude_md: true
```
