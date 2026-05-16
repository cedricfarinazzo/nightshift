# Contributing

## Branching & PR workflow
- Base: feat/prompt-tempfile-compression (or repository default).
- Create feature branch: git checkout -b feat/your-topic <base>
- Keep commits focused; rebase before merge.
- Open PR against base branch.

## Commit message format
- Format: type(scope): summary
- Include trailers in body:
  Nightshift-Task: docs-backfill
  Nightshift-Ref: https://github.com/marcus/nightshift
- Add Co-authored-by when applicable.

## Running tests & linters
- Prefer Makefile targets:
  make test
  make lint
- Or go test ./... (Go projects)

## Creating a PR (gh)
git push --set-upstream origin feat/docs-backfill-nightshift
gh pr create --base feat/prompt-tempfile-compression --head feat/docs-backfill-nightshift --title "docs(backfill): add CONTRIBUTING.md" --body "Minimal docs backfill: CONTRIBUTING.md + README link. Nightshift task: docs-backfill."
