# ── Stage 1: build the nightshift Go binary ──────────────────────────────────
FROM golang:1.24-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /nightshift ./cmd/nightshift

# ── Stage 2: runtime image ────────────────────────────────────────────────────
FROM debian:bookworm-slim

# Grab uv and bun binaries from their official images
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv
COPY --from=oven/bun:latest /usr/local/bin/bun /usr/local/bin/bun

# System packages + Node.js 24 (via NodeSource) + gh CLI
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        gnupg \
    && install -m 0755 -d /etc/apt/keyrings \
    # Node.js 24
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
       | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_24.x nodistro main" \
       > /etc/apt/sources.list.d/nodesource.list \
    # GitHub CLI
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
       | dd of=/etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
       > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends nodejs gh \
    && rm -rf /var/lib/apt/lists/*

# Claude Code — standalone installer (installs to ~/.local/bin, move to system path)
RUN curl -fsSL https://claude.ai/install.sh | sh \
    && mv /root/.local/bin/claude /usr/local/bin/claude

# GitHub Copilot — standalone installer
RUN curl -fsSL https://gh.io/copilot-install | bash \
    && (mv /root/.local/bin/copilot /usr/local/bin/copilot 2>/dev/null \
        || mv /usr/local/bin/.copilot-install/copilot /usr/local/bin/copilot 2>/dev/null \
        || true)

# Codex — npm (no standalone installer available)
RUN npm install -g --no-fund --no-audit @openai/codex \
    && npm cache clean --force

# nightshift binary
COPY --from=builder /nightshift /usr/local/bin/nightshift

# Non-root user for runtime
RUN useradd -m -u 1000 nightshift
USER nightshift
WORKDIR /home/nightshift

ENTRYPOINT ["nightshift"]
