# Changelog

All notable changes to nightshift are documented in this file.

## [v1.0.0] - 2026-05-23

### Breaking Changes

- **Removed `nightshift daemon` command (VC-102)** — the in-process scheduler daemon is removed.
  Scheduling is now delegated to the OS via systemd timer + `Type=oneshot` service (or cron/launchd on
  macOS). Run `nightshift install systemd` to install the unit files.

  **Migration:**
  ```bash
  # Stop and remove old daemon
  systemctl --user disable --now nightshift.service 2>/dev/null || true
  # Install new timer pair
  nightshift install systemd
  systemctl --user enable --now nightshift.timer
  # Optional: Jira timer
  nightshift install --systemd-jira
  systemctl --user enable --now nightshift-jira.timer
  ```

  The `schedule:` YAML block is retained for back-compat but is no longer acted on at runtime.
  Set your actual schedule in the systemd `OnCalendar=` expression.

- **Removed `internal/scheduler/` package** — delete any code importing
  `github.com/cedricfarinazzo/nightshift/internal/scheduler`.

### Features

- `nightshift run` and `nightshift jira run` abort with a clear error if an old daemon unit is still
  active (`assertDaemonNotActive()` preflight guard).
- `nightshift doctor` checks `nightshift.timer` status instead of daemon PID.
- `nightshift install systemd` now generates `Type=oneshot` service units.

## [Unreleased]

### Features

- **feat(deploy): Kubernetes CronJob manifests + deployment guide (VC-1)** — added `deploy/kubernetes/` with Kustomize-driven manifests (CronJob, ConfigMap, PVC, Secret template, Namespace, ServiceAccount) and `docs/operations/kubernetes-cronjob.md` covering container image, auth, Git credentials, repo mounting strategies, and production tips.
- **feat(jira): single-instance lock prevents concurrent jira run on same host (VC-101)** — atomic PID lock at `~/.local/share/nightshift/jira-run.lock`; second invocation exits non-zero with "another jira run in progress, PID N, started at T"; `--wait <duration>` blocks with exponential backoff; stale locks (dead PID) auto-reclaimed; shared `PidLock` helper also used by daemon.

### Refactoring

- **agents: deduplicate option constructors (VC-92)** — extracted shared `agentConfig` struct and
  unified `Option` type into `options.go`. All three agents now use `WithBinaryPath`, `WithDefaultTimeout`,
  `WithRunner`, `WithModel`, `WithEffort`, and `WithBypassPermissions`. Removed per-agent `extractJSON`
  method wrappers; `handleExecuteResult` calls `extractJSON` directly. Deprecated type aliases
  (`ClaudeOption`, `CodexOption`, `CopilotOption`) and function var aliases (`WithCodexRunner`, etc.)
  kept for one-release backward compatibility.

### Breaking Changes

- **Removed SMTP email reporting** — `reporting.email` config field and all `NIGHTSHIFT_SMTP_*` env vars
  are removed. The feature was unwanted and violated config/credential conventions. No replacement.

- **Removed deprecated flat Jira config fields** — `jira.project`, `jira.label`, and `jira.repos`
  top-level fields are removed. Migrate to `jira.projects` list before upgrading:
  ```yaml
  jira:
    projects:
      - key: VC
        label: nightshift
        repos:
          - name: myrepo
            url: git@github.com:org/repo.git
  ```

## [v0.3.3] - 2026-02-19

### Features
- **Branch selection support** — select which branch to run tasks against (#12, thanks @andrew-t-james-wc)

### Fixes
- **gofmt formatting** — fix gofmt formatting across multiple files (#17, thanks @cedricfarinazzo)

## [v0.3.2] - 2026-02-17

### Bug Fixes
- **Block task run in sensitive directories** — refuse to run when project path is `$HOME`, `/`, `/tmp`, `/var`, `/etc`, or `/usr` to prevent accidental credential exposure (#14, thanks @davemac)
- **Fix codex exec for non-interactive runs** — switch from removed `--quiet` flag to `exec` subcommand for Codex 0.98.0 compatibility (#11, thanks @brandon93s)

### Other
- Bus-factor analyzer for code ownership concentration
- Security audit improvements and linter fixes
- Extended test coverage for snapshots, budget, setup, and backward compatibility

## [v0.3.1] - 2026-02-08

### Security

#### Breaking Changes (Opt-In Required for Old Behavior)
- **Default behavior change:** `dangerously_skip_permissions` and `dangerously_bypass_approvals_and_sandbox` now default to `false` (secure)
  - In v0.3.0, these defaulted to `true`, which skipped security prompts
  - Users upgrading from v0.3.0 **who run unattended** (daemon, cron, CI) must explicitly set these flags to `true` in config, or use `--yes` flag
  - Users running **interactively** will now see security prompts (recommended)
  - See [Migration Guide](docs/MIGRATION-v0.3.0-to-v0.3.1.md) for details
- **Database directory permissions:** changed from `0755` to `0700`
  - Existing databases continue to work (no action required)
  - New databases now restrict access to owner only (security improvement)

#### Non-Breaking Improvements
- Shell path escaping improved in setup wizard
- Better security defaults for new installations

### Backward Compatibility
- All v0.3.0 configuration files load correctly in v0.3.1
- Configuration defaults (except dangerous flags) remain unchanged
- Existing databases work without migration
- Environment variable overrides still work
- CLI interface stable for scripts and automation
- Full backward compatibility testing added

### Improvements
- Homebrew formula now builds from source (avoids macOS Gatekeeper warnings)

## [v0.3.0] - 2026-02-01

### Features
- Initial public release
- Daemon mode with launchd/systemd integration
- Support for Claude Code and Codex CLI agents
- Budget-aware task selection
- Project and task configuration via YAML
- Doctor command for setup validation
- Comprehensive logging and reporting
