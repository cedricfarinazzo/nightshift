# Running Nightshift as a Kubernetes CronJob

This guide explains how to deploy nightshift as a Kubernetes CronJob. In this mode the CronJob
schedule replaces nightshift's built-in daemon/systemd timer — nightshift is invoked as a
one-shot `nightshift run --yes` command on each execution.

## Prerequisites

- A running Kubernetes cluster (1.21+)
- `kubectl` configured with cluster access
- A container image of nightshift (see [Building the image](#building-the-image))
- At least one AI provider credential (API key or pre-authenticated CLI volume)
- Git credentials that allow pushing branches / opening PRs from inside the cluster

## Directory layout

```
deploy/kubernetes/
├── namespace.yaml        # nightshift namespace
├── serviceaccount.yaml   # dedicated ServiceAccount
├── pvc.yaml              # PersistentVolumeClaim for SQLite DB and logs
├── configmap.yaml        # nightshift config.yaml baked in as a ConfigMap
├── secrets.yaml          # Secret template (fill before applying)
├── cronjob.yaml          # The CronJob itself
└── kustomization.yaml    # Kustomize entry-point
```

## Quick start

### 1. Build or pull the image

nightshift does not yet publish an official container image. Build one from source:

```dockerfile
# Example minimal Dockerfile (place at repo root)
FROM golang:1.24 AS builder
WORKDIR /src
COPY . .
RUN go build -o /nightshift ./cmd/nightshift

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y git ca-certificates && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 nightshift
COPY --from=builder /nightshift /usr/local/bin/nightshift
USER nightshift
ENTRYPOINT ["nightshift"]
```

Build and push to your registry:

```bash
docker build -t ghcr.io/<your-org>/nightshift:latest .
docker push ghcr.io/<your-org>/nightshift:latest
```

Then update the `image:` field in `cronjob.yaml` to point at your registry.

### 2. Configure

Edit `configmap.yaml` to match your projects, providers, and task selection. Key fields:

| Field | Description |
|-------|-------------|
| `budget.db_path` | Must be on the PVC mount (`/data/nightshift.db`) |
| `logging.path` | Must be on the PVC mount (`/data/logs`) |
| `providers.*.data_path` | Provider CLI credential directory inside the container |
| `providers.*.dangerously_skip_permissions` | Set `true` for unattended runs |
| `projects[].path` | Path to the repo inside the container (mount it or clone on start) |

### 3. Supply secrets

**API key authentication** (simplest):

```bash
kubectl create namespace nightshift

kubectl create secret generic nightshift-secrets \
  --namespace nightshift \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-... \
  --from-literal=OPENAI_API_KEY=sk-...
```

**Subscription CLI authentication** (Claude Code / Codex local login):

Pre-authenticate the provider CLIs on a machine, then copy the credential directories
into a Secret or a PVC. Mount them at the paths set in `providers.*.data_path`
(e.g., `/home/nightshift/.claude`). Remove the API key env vars from `cronjob.yaml`.

### 4. Supply Git credentials

Nightshift pushes branches and opens PRs, so the container needs Git credentials.
The recommended approach is a GitHub token stored as a `.gitconfig`-style credential helper:

```bash
kubectl create secret generic nightshift-git-credentials \
  --namespace nightshift \
  --from-literal=.gitconfig="[credential \"https://github.com\"]
  helper = store
[user]
  email = nightshift-bot@example.com
  name = Nightshift Bot"
```

Or use a Git credentials store file and mount it at `/home/nightshift/.git-credentials`.

For fine-grained access, create a GitHub App or fine-grained PAT with:
- `contents: write` — to push branches
- `pull_requests: write` — to open PRs

### 5. Mount your repositories

Nightshift needs access to the source repositories it will modify. Options:

**Option A — Clone at runtime (init container)**

Add an init container that clones the target repos into an `emptyDir` volume:

```yaml
initContainers:
  - name: clone-repos
    image: alpine/git:latest
    env:
      - name: GITHUB_TOKEN
        valueFrom:
          secretKeyRef:
            name: nightshift-git-credentials
            key: GITHUB_TOKEN
    command:
      - sh
      - -c
      - |
        git clone https://x-access-token:${GITHUB_TOKEN}@github.com/your-org/my-project /repos/my-project
    volumeMounts:
      - name: repos
        mountPath: /repos
```

Add a `repos` `emptyDir` volume and mount it at `/repos` in the main container.

**Option B — Persistent volume with a Git sync sidecar**

Use a tool like [`git-sync`](https://github.com/kubernetes/git-sync) as a sidecar to keep
repos up to date on a shared PVC.

### 6. Adjust the schedule

Edit the `schedule` field in `cronjob.yaml`:

```yaml
schedule: "0 2 * * *"   # 02:00 UTC daily
```

Use [crontab.guru](https://crontab.guru) to build your cron expression. Common choices:

| Expression | Meaning |
|------------|---------|
| `0 2 * * *` | Every day at 02:00 UTC |
| `0 2 * * 1-5` | Weekdays at 02:00 UTC |
| `0 22 * * *` | Every day at 22:00 UTC |

### 7. Deploy

Using Kustomize:

```bash
kubectl apply -k deploy/kubernetes/
```

Or apply each file individually:

```bash
kubectl apply -f deploy/kubernetes/namespace.yaml
kubectl apply -f deploy/kubernetes/serviceaccount.yaml
kubectl apply -f deploy/kubernetes/pvc.yaml
kubectl apply -f deploy/kubernetes/configmap.yaml
kubectl apply -f deploy/kubernetes/secrets.yaml
kubectl apply -f deploy/kubernetes/cronjob.yaml
```

### 8. Verify

```bash
# Trigger a manual run immediately
kubectl create job --from=cronjob/nightshift nightshift-manual -n nightshift

# Watch the job
kubectl get jobs -n nightshift -w

# Stream logs
kubectl logs -n nightshift -l app=nightshift -f

# Check CronJob status
kubectl get cronjob nightshift -n nightshift
```

## Environment variables

| Variable | Description |
|----------|-------------|
| `NIGHTSHIFT_CONFIG` | Path to config file inside the container |
| `NIGHTSHIFT_LOG_LEVEL` | Log level: `debug`, `info`, `warn`, `error` |
| `NIGHTSHIFT_LOG_PATH` | Override log directory |
| `ANTHROPIC_API_KEY` | Anthropic API key for Claude (API auth mode) |
| `OPENAI_API_KEY` | OpenAI API key for Codex (API auth mode) |

## Production tips

- **Pin the image tag** to a specific release (e.g., `:v0.3.1`) rather than `:latest`.
- **Use a secrets manager** such as External Secrets Operator or Vault Agent Injector
  instead of storing secrets directly in Kubernetes Secrets.
- **Set resource limits** appropriate for your workload; defaults in the manifest are
  conservative.
- **Tune `activeDeadlineSeconds`** if your runs consistently take longer than the default 6 hours.
- **Enable RBAC** if your cluster policy requires it — the ServiceAccount needs no special
  cluster permissions; all access is via provider CLIs and Git over HTTPS/SSH.

## Helm chart

A Helm chart is not included yet. Contributions welcome — see
[CONTRIBUTING.md](../../CONTRIBUTING.md) if one exists, or open a PR.
