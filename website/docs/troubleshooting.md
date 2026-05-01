---
sidebar_position: 10
title: Troubleshooting
---

# Troubleshooting

## Common Issues

**"Something feels off"**
- Run `nightshift doctor` to check config, schedule, and provider health

**"No config file found"**
```bash
nightshift init           # Create project config
nightshift init --global  # Create global config
```

**"Insufficient budget"**
- Check current budget: `nightshift budget`
- Increase `max_percent` in config
- Wait for budget reset (check reset time in output)

**"Calibration confidence is low"**
- Run `nightshift budget snapshot` a few times to collect samples
- Ensure tmux is installed so usage percentages are available
- Keep snapshots running for at least a few days

**"tmux not found"**
- Install tmux or set `budget.billing_mode: api` if you pay per token

**"Week boundary looks wrong"**
- Set `budget.week_start_day` to `monday` or `sunday`

**"Provider not available"**
- Ensure Claude/Codex CLI is installed and in PATH
- Check API key environment variables are set

## Debug Mode

Enable verbose logging:

```bash
nightshift run --verbose
```

Or set log level in config:

```yaml
logging:
  level: debug    # debug | info | warn | error
```

## Jira Pipeline Issues

**"Ticket not picked up"**
- Ensure the ticket has the `nightshift` label (or the label configured in `jira.label`)
- Check the ticket is in the "To Do" status (or equivalent configured in `jira.statuses.todo`)
- Run `nightshift jira preview` to see what would be processed

**"Push rejected: fetch first"**
- Another process pushed to the branch before this run. Run again — `SetupWorkspace` will pull and rebase automatically.

**"Ticket stays In Progress across runs"**
- This is expected: the pipeline resumes from the furthest completed phase. If validation and plan succeeded but implement failed, the next run skips directly to implement.
- To force a full restart, move the ticket back to "To Do" status in Jira.

**"Workspace directory conflicts"**
- Each ticket gets its own isolated directory: `{workspace_root}/{TICKET-KEY}/{repo-name}/`
- Stale workspaces are cleaned up after `cleanup_after_days` (default 14 days)
- Manually remove: `rm -rf ~/.local/share/nightshift/jira-workspaces/PROJ-123`

**"Cannot authenticate to Jira"**
- Set `NIGHTSHIFT_JIRA_TOKEN` as an environment variable (not in config)
- Token must have read/write access to the project

**"SSH authentication failure during clone"**
- Repo URLs must use SSH (`git@github.com:org/repo.git`). HTTPS URLs fail silently in non-interactive contexts.

**"Blocked ticket shows as ready"**
- Done blockers are ignored (tickets are considered unblocked when their blocker is done)
- If a blocker is incorrectly shown as done, check the Jira status category on the blocking ticket

**"Validation fails every time"**
- Use `nightshift jira run --skip-validation` to bypass LLM validation
- Or improve the ticket description: add acceptance criteria, clear problem statement, and definition of done

## Getting Help

```bash
nightshift --help
nightshift <command> --help
```

Report issues: https://github.com/marcus/nightshift/issues
