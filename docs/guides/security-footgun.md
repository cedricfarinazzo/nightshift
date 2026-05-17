# Security Foot-Gun Finder

This document describes the automated security scanning and triage added by the security-footgun task.

It runs:
- go vet
- staticcheck
- gosec
- gitleaks
- targeted grep for common insecure patterns

Reports are saved under the `reports/` directory. A GitHub Actions workflow `.github/workflows/security-scan.yml` runs the scan and uploads artifacts.

