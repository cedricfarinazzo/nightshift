#!/usr/bin/env bash
set -euo pipefail
export PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin:$HOME/go/bin"
mkdir -p reports
echo "Branch: $(git rev-parse --abbrev-ref HEAD)" > reports/scan-meta.txt
# attempt to install pinned tool versions (best-effort)
echo "Installing tools (best-effort)..."
go install github.com/securego/gosec/v2/cmd/gosec@v2.7.0 2>/dev/null || true
go install honnef.co/go/tools/cmd/staticcheck@2022.1.3 2>/dev/null || true
go install github.com/zricethezav/gitleaks/v8@v8.18.0 2>/dev/null || true
# run go vet
echo "Running go vet..."
go vet ./... > reports/govet.txt 2>&1 || true
# staticcheck
if command -v staticcheck >/dev/null 2>&1; then
  staticcheck ./... > reports/staticcheck.txt 2>&1 || true
else
  echo "staticcheck: not found" > reports/staticcheck.txt
fi
# gosec
if command -v gosec >/dev/null 2>&1; then
  gosec -fmt=json -out=reports/gosec.json ./... || true
else
  echo '{"error":"gosec not found"}' > reports/gosec.json
fi
# gitleaks
if command -v gitleaks >/dev/null 2>&1; then
  gitleaks detect --source . --report-path reports/gitleaks.json --report-format json || true
else
  echo '{"error":"gitleaks not found"}' > reports/gitleaks.json
fi
# grep
grep -Rn --line-number --exclude-dir=reports -e "InsecureSkipVerify" -e "TLSClientConfig" -e "exec.Command.*sh -c" -e "Chmod(0777)" -e "math/rand" -E "(?i)(apikey|api_key|secret|token|password)" > reports/regex-findings.txt || true
# tests
go test ./... > reports/gotest.txt 2>&1 || true
# summary
cat > reports/security-footgun-summary.md <<EOF
# Security Foot-Gun Scan Summary

Branch: $(git rev-parse --abbrev-ref HEAD)
Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")

## Reports
- govet: reports/govet.txt
- staticcheck: reports/staticcheck.txt
- gosec: reports/gosec.json
- gitleaks: reports/gitleaks.json
- regex findings: reports/regex-findings.txt
- tests: reports/gotest.txt

EOF
