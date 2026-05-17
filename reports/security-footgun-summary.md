# Security footgun triage summary

Date: 2026-05-17T09:48:00+02:00

Overview
- Ran: go vet, staticcheck (not installed or no output), gosec (not installed). Outputs saved under `reports/`.
- Grep-based findings written to `reports/regex-findings.txt` (high volume; contains docs, code, tests).

Key observations
- Documentation and examples include references to ANTHROPIC_API_KEY / OPENAI_API_KEY (expected placeholders).
- Several places mention or enable "dangerous" flags; config defaults are already safe (see `internal/config/config.go`), but docs/README contained example values set to `true` — updated to `false` in this change.
- exec usage (`os/exec`) appears in installer/setup code (expected for system integration scripts) — review and sandbox validate inputs.
- SECURITY_AUDIT.md lists medium/high items (path validation, symlink traversal, unvalidated file reads). These require developer attention and targeted fixes.

Actions taken (low-risk)
- Updated README example to set dangerous flags to `false` (safer defaults in docs).
- Added this triage summary: `reports/security-footgun-summary.md`.
- Added a minimal GitHub Actions workflow `.github/workflows/security-scan.yml` to run go test, go vet, staticcheck and gosec (these tools are installed at runtime; staticcheck/gosec marked continue-on-error to avoid CI breakage until configured).

Recommendations (next steps)
1. Run `gosec` locally or via CI to produce a security report and triage findings in detail.
2. Prioritize fixes from SECURITY_AUDIT.md: path validation, symlink traversal, and silent error suppression.
3. Add unit tests for path validation and sandbox command validation.
4. Schedule nightly security scans and gate PRs with the security-scan workflow.

Files/outputs
- reports/govet.txt
- reports/staticcheck.txt
- reports/gosec.json
- reports/regex-findings.txt
- reports/security-footgun-summary.md

