# Getting Started

A minimal quickstart to build, configure and run Nightshift locally.

Prerequisites
- Go 1.24
- Git
- GitHub CLI (gh) for Copilot integration (optional)

Build

```bash
make build
# or
go build -o nightshift ./cmd/nightshift
```

Install

```bash
make install
# or
go install github.com/marcus/nightshift/cmd/nightshift@latest
```

Configure

```bash
export ANTHROPIC_API_KEY=your_anthropic_key
export OPENAI_API_KEY=your_openai_key
# authenticate gh for Copilot (optional)
gh auth login
```

Note: Nightshift stores runtime data under `~/.local/share/nightshift` (database, reports, logs). For non-interactive Copilot runs ensure `gh` is authenticated and the account has a Copilot subscription.

Quick run

```bash
nightshift setup          # interactive onboarding
nightshift preview --plain
nightshift run --yes      # non-interactive run
```

Run tests

```bash
make test
# or
go test ./...
```

Where to get help
- Project docs: website/docs
- Developer guides: docs/guides
