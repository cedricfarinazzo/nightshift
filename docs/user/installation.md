# Installation

## Homebrew (recommended)

```bash
brew install marcus/tap/nightshift
```

## Pre-built binaries

Download from [GitHub releases](https://github.com/cedricfarinazzo/nightshift/releases). Builds for macOS + Linux (amd64, arm64). Extract the tarball and place `nightshift` somewhere on `$PATH`.

## From source

Requires Go 1.24+.

```bash
go install github.com/cedricfarinazzo/nightshift/cmd/nightshift@latest
```

Or clone + build:

```bash
git clone https://github.com/cedricfarinazzo/nightshift.git
cd nightshift
make build
sudo mv nightshift /usr/local/bin/
```

## Verify

```bash
nightshift --version
nightshift doctor
```

`nightshift doctor` reports missing agent binaries, config issues, and missing credentials.

## Agent Prerequisites

At least one agent CLI must be installed + authenticated.

| Agent | Install | Auth |
|-------|---------|------|
| Claude Code | `npm i -g @anthropic-ai/claude-code` | `claude /login` or `ANTHROPIC_API_KEY` |
| Codex | `npm i -g @openai/codex` | `codex --login` or `OPENAI_API_KEY` |
| GitHub Copilot | `gh extension install github/gh-copilot` | `gh auth login` (Copilot subscription) |

See [Agents](agents.md) for per-agent setup details.

## Uninstall

```bash
nightshift uninstall                                   # removes systemd unit
rm -rf ~/.config/nightshift ~/.local/share/nightshift  # config + data
rm "$(which nightshift)"                               # binary
```
