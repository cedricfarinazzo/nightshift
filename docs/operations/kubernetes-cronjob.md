# Kubernetes CronJob Deployment

> **Doc track note**: This guide lives under `docs/operations/` (alongside `daemon.md` and `systemd-install.md`) rather than `docs/guides/` as specified in the ticket. `docs/guides/` is not an existing documentation track; placing here keeps taxonomy consistent.

---

## Overview

Running nightshift as a Kubernetes CronJob suits teams that already operate a cluster and want:

- Declarative, GitOps-friendly scheduling
- Automatic retries and history via `successfulJobsHistoryLimit` / `failedJobsHistoryLimit`
- Isolation from the host OS (no systemd, no cron on the node)
- Horizontal tenancy: run one CronJob per project namespace

If you run nightshift on a single machine, the systemd approach (`docs/operations/systemd-install.md`) is simpler.

---

## Quick Start

```bash
# 1. Create namespace
kubectl create namespace nightshift

# 2. Create the credentials Secret out-of-band (secrets.yaml is a template; do not apply it directly).
#    Omit keys for disabled providers.
kubectl create secret generic nightshift-secrets \
  --namespace nightshift \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-... \
  --from-literal=OPENAI_API_KEY=sk-... \
  --from-literal=GITHUB_TOKEN=ghp_... \
  --from-literal=NIGHTSHIFT_JIRA_TOKEN=...

# 3. Apply all manifests via Kustomize (secrets.yaml is excluded from the default kustomization)
kubectl apply -k deploy/kubernetes/

# 4. Verify
kubectl -n nightshift get cronjob,pvc,configmap,secret
```

The CronJob fires at 02:00 UTC every day by default (`spec.timeZone: "Etc/UTC"` in `cronjob.yaml`; requires Kubernetes ≥ 1.27). Trigger a manual run:

```bash
kubectl -n nightshift create job --from=cronjob/nightshift nightshift-manual-$(date +%s)
kubectl -n nightshift logs -f job/nightshift-manual-...
```

---

## Container Image

The manifests reference `ghcr.io/cedricfarinazzo/nightshift:latest`. For production, pin to a specific tag and override via Kustomize:

```yaml
# kustomization.yaml overlay
images:
  - name: ghcr.io/cedricfarinazzo/nightshift
    newTag: v0.3.4
```

### Building Your Own Image

Example multi-stage Dockerfile. The image must bundle `nightshift` plus any agent CLIs you enable (`claude`, `codex`, `gh`).

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.24 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /nightshift ./cmd/nightshift

# Install agent CLIs in a separate stage for layer caching.
FROM debian:bookworm-slim AS tools
RUN apt-get update && apt-get install -y --no-install-recommends \
      curl ca-certificates git gh && \
    # Install Claude CLI (replace with actual install method when available)
    # curl -fsSL https://claude.ai/install.sh | sh && \
    rm -rf /var/lib/apt/lists/*

# Use distroless/base (not /static) — git and gh are dynamically linked and require glibc.
FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=builder /nightshift /usr/local/bin/nightshift
COPY --from=tools /usr/bin/git /usr/bin/git
COPY --from=tools /usr/bin/gh /usr/bin/gh
# Copy claude/codex CLIs if installed in the tools stage.

ENTRYPOINT ["nightshift"]
```

> **Note**: `claude` and `codex` CLIs must be in the image's PATH. Nightshift's agent layer calls these as external processes (see CLAUDE.md gotcha "Agent binaries must be in PATH"). Never call `exec.Command` with a CLI that may be absent.

---

## Authentication

### API Keys (Recommended for Kubernetes)

Inject credentials via the `nightshift-secrets` Secret. All Nightshift providers read credentials from environment variables only — never from config files on disk.

| Env Var | Provider |
|---|---|
| `ANTHROPIC_API_KEY` | Claude (API mode) |
| `OPENAI_API_KEY` | Codex |
| `GITHUB_TOKEN` | GitHub / Copilot |
| `NIGHTSHIFT_JIRA_TOKEN` | Jira integration |

The `envFrom.secretRef` in `cronjob.yaml` mounts all Secret keys as env vars automatically.

### Subscription CLI Auth (Not Supported in Kubernetes)

`claude` and `codex` CLIs in subscription (non-API) mode read credentials interactively from `~/.config` and may require browser-based OAuth. This is incompatible with Kubernetes pods (no TTY, no browser). **Use API key mode for all providers when running in Kubernetes.**

If you need subscription auth, the systemd / daemon approach on a desktop machine is a better fit.

---

## Git Credentials

Nightshift clones and pushes repositories on your behalf. Configure one of:

### SSH Key (Recommended)

1. Create an SSH key pair without a passphrase: `ssh-keygen -t ed25519 -f nightshift_id_ed25519 -N ""`
2. Add the public key to GitHub as a deploy key with write access.
3. Store the private key in a Secret:
   ```bash
   kubectl create secret generic nightshift-ssh \
     --namespace nightshift \
     --from-file=id_ed25519=./nightshift_id_ed25519
   ```
4. Mount into the pod at `/data/home/.ssh/id_ed25519` (since `HOME=/data/home` in the CronJob):
   ```yaml
   # patch in kustomization.yaml
   - name: ssh-key
     secret:
       secretName: nightshift-ssh
       defaultMode: 0400
   ```
   ```yaml
   volumeMounts:
     - name: ssh-key
       mountPath: /data/home/.ssh
       readOnly: true
   ```
5. Set repo URLs to SSH format (`git@github.com:org/repo.git`) — HTTPS remotes fail silently without a TTY (see CLAUDE.md gotcha "Jira repo URL must use SSH").

### HTTPS Personal Access Token

Add `GIT_ASKPASS` or configure `~/.netrc` via a Secret-mounted file at `/data/home/.netrc`:

```
machine github.com login x-access-token password <PAT>
```

---

## Repo Mounting Strategies

### (a) Ephemeral Clone per Run (Default)

Set `workspace.root: /data/workspaces` in `configmap.yaml`. Each run clones repos into `/data/workspaces/<name>_<runID>/` and cleans up stale clones after `ttl_days`. This is the simplest approach and works well for the Jira pipeline.

```yaml
workspace:
  root: /data/workspaces
  ttl_days: 7
```

### (b) PVC-Backed Long-Lived Workspaces

Same as (a) but with a larger PVC and `ttl_days` set high. Subsequent runs reuse existing clones (fetching + rebasing) instead of re-cloning. Faster for large repos; uses more storage.

> **Mtime caveat**: `CleanupStaleWorkspaces` uses file mtime to detect staleness. Some CSI drivers reset mtime on PVC attach. If workspaces are deleted unexpectedly, lower `ttl_days` or disable cleanup and manage manually.

### (c) git-sync Sidecar

For read-only access to a single repo, add a [git-sync](https://github.com/kubernetes/git-sync) sidecar that keeps a shared emptyDir up to date. Nightshift tasks then run against the synced directory. Not suitable for workflows that push (Jira pipeline, PR creation).

---

## Environment Variables Reference

| Variable | Required | Description |
|---|---|---|
| `ANTHROPIC_API_KEY` | If Claude enabled | Claude API key |
| `OPENAI_API_KEY` | If Codex enabled | OpenAI API key |
| `GITHUB_TOKEN` | If Copilot / PR creation | GitHub PAT or app token |
| `NIGHTSHIFT_JIRA_TOKEN` | If Jira enabled | Jira API token |
| `NIGHTSHIFT_CONFIG` | Recommended | Path to config file (default: `~/.config/nightshift/config.yaml`) |
| `HOME` | Set in manifest | Must point to a writable directory (`/data/home` in the provided manifest) |

---

## Production Tips

### Resource Sizing

Default limits (`cpu: 1000m`, `memory: 1Gi`) suit small projects. For large repos or many parallel agent runs, increase memory. Agent CLIs (`claude`, `codex`) are the dominant memory consumers.

### PVC Backup

Snapshot `/data` to protect the SQLite database and logs:

```bash
kubectl -n nightshift exec -it <pod> -- nightshift stats  # check DB health first
# Then take a VolumeSnapshot or use your CSI driver's backup mechanism.
```

### Log Shipping

Logs write to `/data/logs` by default. To ship to stdout for cluster log aggregation:

1. Set `logging.path: -` in `configmap.yaml` (if supported by your nightshift version).
2. Or add a sidecar container running `tail -F /data/logs/nightshift-*.log`.

### Monitoring CronJob Success

Use [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) with Prometheus:

```promql
# Alert if the last successful CronJob run is more than 26 hours ago.
time() - kube_cronjob_status_last_successful_time{cronjob="nightshift"} > 93600
```

### Image Pinning

Never use `:latest` in production. Pin to a digest or specific tag:

```yaml
images:
  - name: ghcr.io/cedricfarinazzo/nightshift
    newTag: v0.3.4
```

### Secrets Management

The provided `secrets.yaml` is a **template only** — it contains placeholder strings and must not be committed with real values. For production:

- [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets): encrypt secrets at rest in Git.
- [External Secrets Operator](https://external-secrets.io/): sync from Vault, AWS Secrets Manager, etc.
- Avoid `kubectl create secret ... --from-literal` with real keys in shell history; use `--from-file` or pipe from a password manager.

### Concurrency and SQLite

`concurrencyPolicy: Forbid` prevents concurrent CronJob runs. This is mandatory — `modernc.org/sqlite` (pure Go, no CGO) is safe from CGO locking issues, but SQLite's `ReadWriteOnce` PVC access mode still means only one pod can write at a time. Do not remove `concurrencyPolicy: Forbid`.
