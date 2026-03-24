# ── Stage 1: build the nightshift Go binary ──────────────────────────────────
FROM golang:1.24-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /nightshift ./cmd/nightshift

# ── Stage 2: runtime image ────────────────────────────────────────────────────
# Node.js LTS is required to run all three provider CLIs
# (claude-code, codex, and copilot are npm packages).
FROM node:22-bookworm-slim

# System packages: git (branch push/PR), curl + gnupg (gh CLI apt key), ca-certs
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        gnupg \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
       | dd of=/etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
       > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# Provider CLIs — installed globally so they land in /usr/local/bin
#   claude  → @anthropic-ai/claude-code
#   codex   → @openai/codex
#   copilot → @github/copilot
RUN npm install -g --no-fund --no-audit \
        @anthropic-ai/claude-code \
        @openai/codex \
        @github/copilot \
    && npm cache clean --force

# nightshift binary
COPY --from=builder /nightshift /usr/local/bin/nightshift

# Non-root user for runtime
RUN useradd -m -u 1000 nightshift
USER nightshift
WORKDIR /home/nightshift

ENTRYPOINT ["nightshift"]
