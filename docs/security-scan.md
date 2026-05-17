# Security scan: Security Foot-Gun Finder

This adds automated scanning (go vet, staticcheck, gosec) and a grep-based rule set to detect common anti-patterns.
Run locally: make security-scan
CI: .github/workflows/security-scan.yml uploads reports/ as artifact.

Minimal approach: the workflow collects findings; reviewers triage and apply fixes. Long secrets are redacted in regex-findings-redacted.txt.
