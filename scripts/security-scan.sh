#!/usr/bin/env bash
set -euo pipefail
outdir="$(pwd)/reports"
mkdir -p "$outdir"
echo "Running go vet..."
go vet ./... > "$outdir/govet.txt" 2>&1 || true
echo "Running staticcheck (installing if missing)..."
if ! command -v staticcheck >/dev/null 2>&1; then
  echo "staticcheck not found; attempting go install honnef.co/go/tools/cmd/staticcheck@latest" > "$outdir/staticcheck.txt"
  if go install honnef.co/go/tools/cmd/staticcheck@latest; then
    export PATH="$(go env GOPATH)/bin:$PATH"
  else
    echo "staticcheck install failed" >> "$outdir/staticcheck.txt"
  fi
fi
if command -v staticcheck >/dev/null 2>&1; then
  staticcheck ./... > "$outdir/staticcheck.txt" 2>&1 || true
fi
echo "Running gosec (installing if missing)..."
if ! command -v gosec >/dev/null 2>&1; then
  echo "gosec not found; attempting go install github.com/securego/gosec/v2/cmd/gosec@latest" > "$outdir/gosec.txt"
  if go install github.com/securego/gosec/v2/cmd/gosec@latest; then
    export PATH="$(go env GOPATH)/bin:$PATH"
  else
    echo "gosec install failed" >> "$outdir/gosec.txt"
  fi
fi
if command -v gosec >/dev/null 2>&1; then
  gosec ./... > "$outdir/gosec.txt" 2>&1 || true
fi
echo "Grepping for common insecure patterns..."
grep -RInE --binary-files=without-match --exclude-dir=.git --exclude-dir=vendor --exclude-dir=node_modules -e "InsecureSkipVerify" -e "TLSClientConfig" -e "exec\\.Command.*sh -c" -e "Chmod\\s*\\(0?777|0777" -e "math/rand" -e "(apikey|api_key|secret|token|password)" . > "$outdir/regex-findings.txt" || true
# Redact long potential secrets in findings
sed -E 's/[A-Za-z0-9_\\-]{20,}/[REDACTED_SECRET]/g' "$outdir/regex-findings.txt" > "$outdir/regex-findings-redacted.txt" || true
# Create combined summary
echo "# Security Foot-Gun Summary" > "$outdir/security-footgun-summary.md"

echo "" >> "$outdir/security-footgun-summary.md"
echo "Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")" >> "$outdir/security-footgun-summary.md"
echo "" >> "$outdir/security-footgun-summary.md"
echo "## Counts" >> "$outdir/security-footgun-summary.md"
echo "- go vet issues: $(wc -l < "$outdir/govet.txt" 2>/dev/null || echo 0)" >> "$outdir/security-footgun-summary.md"
echo "- staticcheck issues: $(wc -l < "$outdir/staticcheck.txt" 2>/dev/null || echo 0)" >> "$outdir/security-footgun-summary.md"
echo "- gosec issues: $(wc -l < "$outdir/gosec.txt" 2>/dev/null || echo 0)" >> "$outdir/security-footgun-summary.md"
echo "- regex findings (raw): $(wc -l < "$outdir/regex-findings.txt" 2>/dev/null || echo 0)" >> "$outdir/security-footgun-summary.md"
echo "" >> "$outdir/security-footgun-summary.md"
echo "## Top regex findings (redacted)" >> "$outdir/security-footgun-summary.md"
head -n 200 "$outdir/regex-findings-redacted.txt" >> "$outdir/security-footgun-summary.md" || true
