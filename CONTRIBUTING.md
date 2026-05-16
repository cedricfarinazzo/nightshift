# Contributing

## Branching & PR workflow
- Branch from feat/prompt-tempfile-compression:
  git checkout feat/prompt-tempfile-compression && git pull --ff-only
- Create feature branch:
  git checkout -b feat/docs-backfill-nightshift
- Push branch:
  git push --set-upstream origin feat/docs-backfill-nightshift
- Open PR targeting feat/prompt-tempfile-compression using the gh CLI (example below).

## Commit message format
- Use Conventional Commits for subject, e.g. `docs: add CONTRIBUTING.md`
- Include these git trailers in commit body (exact):
  Nightshift-Task: docs-backfill
  Nightshift-Ref: https://github.com/marcus/nightshift
- Add Co-authored-by trailer when applicable:
  Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>

## Running tests & linters
- Run Go tests: `make test` or `go test ./...`
- Format: `gofmt -w .`
- Vet: `go vet ./...`

## How to open a PR (gh)
Create PR from local branch:
```
gh pr create --base feat/prompt-tempfile-compression --head feat/docs-backfill-nightshift \
  --title "docs(backfill): add CONTRIBUTING.md" \
  --body "Minimal docs backfill: CONTRIBUTING.md + README link. Nightshift task: docs-backfill."
```

