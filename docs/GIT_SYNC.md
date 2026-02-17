# Git Repository Synchronization

Nightshift can optionally synchronize your git repository before running tasks. This ensures agents always work from the latest code on your primary branch.

## Configuration

Add to your `~/.config/nightshift/config.yaml`:

```yaml
git:
  sync_before_run: true      # Enable git sync before tasks (default: false)
  primary_branch: "main"     # Primary branch name (auto-detected if empty)
```

## Behavior

When `sync_before_run` is enabled, before executing any task, nightshift will:

1. **Detect the primary branch** (if not explicitly configured)
   - Checks `git symbolic-ref refs/remotes/origin/HEAD`
   - Falls back to checking if `main` or `master` exists
   
2. **Verify clean working tree**
   - Checks for uncommitted changes with `git status --porcelain`
   - **Refuses to sync** if working tree is dirty

3. **Checkout the primary branch**
   - Runs `git checkout <primary_branch>`

4. **Pull latest changes**
   - Runs `git pull --ff-only`
   - Uses fast-forward only to avoid merge conflicts

## Safety

- **Non-destructive**: Only runs if working tree is clean
- **Fast-forward only**: `--ff-only` prevents accidental merges
- **Logged warnings**: Sync failures are logged but don't stop task execution
- **Agent responsibility**: Agents still create feature branches for their work

## Use Cases

Enable this when:
- Running nightshift on a schedule (daemon/cron)
- Working across multiple machines
- Want to ensure agents always work from latest code

Disable this when:
- Testing local changes
- Working on a feature branch
- Repository is frequently dirty

## Example

```bash
# In your config
git:
  sync_before_run: true
  primary_branch: "develop"  # if using gitflow

# Or use auto-detection
git:
  sync_before_run: true
  # primary_branch defaults to detected value
```

## Troubleshooting

**"working tree has uncommitted changes"**
- Commit or stash your changes before running nightshift
- Or disable `sync_before_run` temporarily

**"could not detect primary branch"**
- Set `primary_branch` explicitly in config
- Ensure remote origin is configured: `git remote -v`

**"git pull: CONFLICT"**
- This should never happen (we use `--ff-only`)
- If it does, manually resolve conflicts and reset your branch
