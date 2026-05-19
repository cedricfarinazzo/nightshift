# Agents

Nightshift spawns external agent CLIs to do actual work. Three are supported.

| Agent | Binary | Provider | Auth |
|-------|--------|----------|------|
| Claude Code | `claude` | Anthropic | Subscription or `ANTHROPIC_API_KEY` |
| Codex | `codex` | OpenAI | Subscription or `OPENAI_API_KEY` |
| GitHub Copilot | `gh copilot` / `copilot` | GitHub | `gh auth login` + Copilot sub |

All three must be on `$PATH`. `nightshift doctor` verifies.

## Claude Code

```bash
npm install -g @anthropic-ai/claude-code
claude              # then /login
# OR: export ANTHROPIC_API_KEY=sk-ant-...
```

Invocation:

```
claude --print [--dangerously-skip-permissions] [--model <m>] <directive>
```

The full prompt is written to a temp file; only a short directive is passed as the CLI arg (avoids ARG_MAX).

```yaml
providers:
  claude:
    enabled: true
    data_path: "~/.claude"
    dangerously_skip_permissions: true             # required for autonomous file writes
    dangerously_bypass_approvals_and_sandbox: true # required for Jira implement
```

Common models: `claude-sonnet-4-5`, `claude-opus-4-5`, `claude-haiku-4-5`.

## Codex

```bash
npm install -g @openai/codex
codex --login
# OR: export OPENAI_API_KEY=sk-...
```

Invocation:

```
codex exec [--dangerously-bypass-approvals-and-sandbox] [--model <m>] <directive>
```

```yaml
providers:
  codex:
    enabled: true
    data_path: "~/.codex"
    dangerously_bypass_approvals_and_sandbox: true
```

Common models: `codex-mini-latest`, `gpt-4o`, `o4-mini`.

## GitHub Copilot

```bash
brew install gh
gh extension install github/gh-copilot
gh auth login
# OR (standalone): npm install -g @github/copilot-cli
```

Invocation (via gh):

```
gh copilot -- -p <directive> --no-ask-user --silent [--model <m>] \
            [--allow-all-tools --allow-all-urls]
```

```yaml
providers:
  copilot:
    enabled: true
    binary_path: "copilot"                # optional: standalone binary
    dangerously_skip_permissions: true    # adds --allow-all-tools --allow-all-urls
```

Models accessible via Copilot subscription: `gpt-5.4-mini`, `gpt-5.4`, `claude-sonnet-4.6`, `claude-opus-4-5`.

## Provider Selection

For task runs, providers are tried in `preference` order; first one with budget wins:

```yaml
providers:
  preference: [claude, codex, copilot]
```

For the Jira pipeline each phase specifies its provider explicitly — `preference` is ignored.

## Troubleshooting

- **Permission prompts halt runs.** Set `dangerously_skip_permissions` (Claude) or `dangerously_bypass_approvals_and_sandbox` (Codex).
- **Copilot `ask_user` interrupts.** `--no-ask-user` is always passed; if you still see it, `gh extension upgrade copilot`.
- **Auth expired.** Re-run `claude /login`, `codex --login`, or `gh auth login`.
- **Wrong model.** Use `nightshift preview` to see which model would be used before running.
