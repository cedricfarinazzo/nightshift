---
sidebar_position: 2
title: Installation
---

# Installation

## Homebrew (Recommended)

```bash
brew install marcus/tap/nightshift
```

## Binary Downloads

Pre-built binaries are available on the [GitHub releases page](https://github.com/marcus/nightshift/releases) for macOS and Linux (Intel and ARM).

## From Source

Requires Go 1.24+:

```bash
go install github.com/marcus/nightshift/cmd/nightshift@latest
```

Or build from the repository:

```bash
git clone https://github.com/marcus/nightshift.git
cd nightshift
go build -o nightshift ./cmd/nightshift
sudo mv nightshift /usr/local/bin/
```

## Verify Installation

```bash
nightshift --version
nightshift --help
```

## Prerequisites

At least one AI agent CLI must be installed and authenticated. Nightshift supports three:

### Claude Code

```bash
npm install -g @anthropic-ai/claude-code
claude
/login
# or: export ANTHROPIC_API_KEY=sk-ant-...
```

### Codex

```bash
npm install -g @openai/codex
codex --login
# or: export OPENAI_API_KEY=sk-...
```

### GitHub Copilot

```bash
# Install gh CLI and the Copilot extension
brew install gh
gh extension install github/gh-copilot
gh auth login
```

Requires a GitHub account with an active Copilot subscription.

See [Agent Integrations](/docs/agents) for detailed configuration options for each provider.
