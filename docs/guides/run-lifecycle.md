# Nightshift Run Lifecycle

This guide explains what happens from a scheduled trigger to a finished run, including logging and reports.

## High-Level Flow

1. **Scheduler triggers a run**
   - cron/launchd/systemd executes `nightshift run`
   - or the daemon scheduler calls `runScheduledTasks`
2. **Nightshift loads config + initializes logging**
3. **Budget + provider selection**
4. **Task selection**
5. **Plan → Implement → Review loop**
6. **Run record + summary + report saved**

## Where Output Goes

- **Structured logs**: `~/.local/share/nightshift/logs/nightshift-YYYY-MM-DD.log`
- **Audit log**: `~/.local/share/nightshift/audit/audit-YYYY-MM-DD.jsonl` (append-only JSONL; records agent start/complete/error, file operations, git operations, security events)
- **Run report**: `~/.local/share/nightshift/reports/run-YYYY-MM-DD-HHMMSS.md`
- **Daily summary** (if `reporting.morning_summary: true`):
  `~/.local/share/nightshift/summaries/summary-YYYY-MM-DD.md`
- **Status**: `nightshift status --today` for a quick recap

## Sequence Diagram

![Run lifecycle sequence](run-lifecycle.png)

```mermaid
sequenceDiagram
  autonumber
  participant S as Scheduler (cron/launchd/systemd/daemon)
  participant NS as nightshift run
  participant C as Config/Logging
  participant DB as SQLite State
  participant B as Budget Manager
  participant P as Provider Selector
  participant O as Orchestrator
  participant R as Reports

  S->>NS: Start run
  NS->>C: Load config + init logging
  NS->>DB: Open DB + load state
  NS->>B: Calculate allowance(s)
  NS->>P: Choose provider (preference + budget)
  NS->>O: Plan → Implement → Review
  O->>DB: Record task + project runs
  NS->>R: Save run report + daily summary
  NS-->>S: Exit
```
