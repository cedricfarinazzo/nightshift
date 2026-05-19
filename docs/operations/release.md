# Release Process

Cut a new version using goreleaser. The `.github/workflows/release.yml` workflow builds + publishes when you push a `v*` tag.

## Prerequisites

- Working tree clean
- `goreleaser` installed locally (`brew install goreleaser`)
- Push access to `cedricfarinazzo/nightshift`

## Steps

### 1. Pick version

```bash
git tag --sort=-v:refname | head -1
```

Decide major/minor/patch bump.

### 2. Bump in source

Edit `cmd/nightshift/commands/root.go`:

```go
Version = "X.Y.Z"   // no v prefix
```

### 3. Validate locally

```bash
goreleaser check                      # YAML syntax
goreleaser release --snapshot --clean # full local build, no publish
ls dist/                              # confirm darwin+linux × amd64+arm64
rm -rf dist/
```

### 4. Commit, tag, push

```bash
git add cmd/nightshift/commands/root.go CHANGELOG.md
git commit -m "chore(release): vX.Y.Z"
git tag vX.Y.Z
git push origin main vX.Y.Z
```

Tag push triggers the release workflow.

### 5. Verify

```bash
gh run list --workflow=release.yml --limit 1
gh run watch
gh release view vX.Y.Z
```

Confirm:

- Workflow run is green
- Release has 4 tarballs (darwin/linux × amd64/arm64) + `checksums.txt`
- Auto-generated changelog excludes `docs:`/`test:`/`ci:`/`chore:` commits (filter rules in `.goreleaser.yml`)

### 6. Update Homebrew tap (manual)

Homebrew formula at `marcus/homebrew-tap` is manually maintained (builds from source — avoids macOS Gatekeeper warnings). Bump the formula version + sha256 there.

## Troubleshooting

| Problem | Fix |
|---------|-----|
| `goreleaser check` fails | Fix `.goreleaser.yml` per the error |
| Snapshot builds but CI fails | Compare `go version` with `go.mod`; ensure `CGO_ENABLED=0` |
| Release missing artifacts | `gh release delete vX.Y.Z`, `git push --delete origin vX.Y.Z && git tag -d vX.Y.Z`, re-tag and push |
| Tap not updated | `HOMEBREW_TAP_TOKEN` repo secret missing or expired |

## Skill

There's also a Claude Code skill at `.claude/skills/nightshift-release/SKILL.md` that walks through this same flow interactively.
