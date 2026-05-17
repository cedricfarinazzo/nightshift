# Security Foot-Gun Summary

Generated: 2026-05-17T07:53:14Z

## Counts
- go vet issues: 0
- staticcheck issues: 0
- gosec issues: 834
- regex findings (raw): 594

## Top regex findings (redacted)
./cmd/nightshift/commands/run_output.go:254:				s.Muted.Render(fmt.Sprintf("(score=%.1f, cost=%s, ~%dk-%dk tokens)", st.Score, st.Definition.CostTier, minTok/1000, maxTok/1000)))
./cmd/nightshift/commands/run_output.go:330:			s.Muted.Render(fmt.Sprintf("(score=%.1f, cost=%s, tokens=%d-%d)", st.Score, st.Definition.CostTier, minTok, maxTok)))
./cmd/nightshift/commands/preview.go:201:	MinTokens int    `json:"min_tokens,omitempty"`
./cmd/nightshift/commands/preview.go:202:	MaxTokens int    `json:"max_tokens,omitempty"`
./cmd/nightshift/commands/report.go:770:				line += fmt.Sprintf(" · %s tokens", formatTokensCompact(task.TokensUsed))
./cmd/nightshift/commands/report.go:880:	tokensUsed    int
./cmd/nightshift/commands/report.go:901:		agg.tokensUsed += summary.TokensUsed
./cmd/nightshift/commands/report.go:1028:func formatTokensCompact(tokens int) string {
./cmd/nightshift/commands/report.go:1030:	case tokens >= 1_000_000:
./cmd/nightshift/commands/report.go:1031:		return fmt.Sprintf("%.1fm", float64(tokens)/1_000_000)
./cmd/nightshift/commands/report.go:1032:	case tokens >= 10_000:
./cmd/nightshift/commands/report.go:1033:		return fmt.Sprintf("%.0fk", float64(tokens)/1_000)
./cmd/nightshift/commands/report.go:1034:	case tokens >= 1_000:
./cmd/nightshift/commands/report.go:1035:		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
./cmd/nightshift/commands/report.go:1037:		return fmt.Sprintf("%d", tokens)
./cmd/nightshift/commands/report.go:1121:		case strings.HasSuffix(part, " tokens"):
./cmd/nightshift/commands/report.go:1122:			task.TokensUsed = parseTokenString(strings.TrimSuffix(part, " tokens"))
./cmd/nightshift/commands/preview_output.go:481:	MinTokens       int     `json:"min_tokens"`
./cmd/nightshift/commands/preview_output.go:482:	MaxTokens       int     `json:"max_tokens"`
./cmd/nightshift/commands/stats.go:24:Shows run counts, task outcomes, token usage, budget projections,
./cmd/nightshift/commands/stats.go:184:			// Accumulate tokens from tasks if report-level budget is missing
./cmd/nightshift/commands/stats.go:289:	fmt.Printf("  Total used:   %s tokens\n", formatTokens64(int64(result.TotalTokensUsed)))
./cmd/nightshift/commands/stats.go:291:		fmt.Printf("  Avg per run:  %s tokens\n", formatTokens64(int64(result.AvgTokensPerRun)))
./cmd/nightshift/commands/setup_test.go:101:		t.Fatal("expected new path token to be present")
./cmd/nightshift/commands/setup_test.go:117:		t.Fatal("expected no change when exact path token exists")
./cmd/nightshift/commands/jira_preview.go:33:	jiraPreviewCmd.Flags().Bool("validate", false, "Run LLM validation on each ticket (costs tokens)")
./cmd/nightshift/commands/budget.go:119:			fmt.Printf("  (configure GitHub token via 'gh auth' to enable active tracking)\n")
./cmd/nightshift/commands/budget.go:234:func formatTokens64(tokens int64) string {
./cmd/nightshift/commands/budget.go:235:	if tokens >= 1000000 {
./cmd/nightshift/commands/budget.go:236:		return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
./cmd/nightshift/commands/budget.go:238:	if tokens >= 1000 {
./cmd/nightshift/commands/budget.go:239:		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
./cmd/nightshift/commands/budget.go:241:	return fmt.Sprintf("%d", tokens)
./cmd/nightshift/commands/run.go:564:			_, _ = fmt.Fprintf(w, "     - %s (score=%.1f, cost=%s, ~%dk-%dk tokens)\n",
./cmd/nightshift/commands/run.go:672:				fmt.Printf("  %d. %s (score=%.1f, cost=%s, tokens=%d-%d)\n",
./cmd/nightshift/commands/setup_jira_test.go:528:	if err := os.Setenv(envKey, "test-token"); err != nil {
./cmd/nightshift/commands/task.go:31:and estimated token range.
./cmd/nightshift/commands/task.go:269:	fmt.Printf("Est:      %s-%s tokens\n", formatK(min), formatK(max))
./cmd/nightshift/commands/task.go:424:func formatK(tokens int) string {
./cmd/nightshift/commands/task.go:425:	if tokens >= 1_000_000 {
./cmd/nightshift/commands/task.go:426:		return fmt.Sprintf("%.0fM", float64(tokens)/1_000_000)
./cmd/nightshift/commands/task.go:428:	if tokens >= 1_000 {
./cmd/nightshift/commands/task.go:429:		return fmt.Sprintf("%dk", tokens/1_000)
./cmd/nightshift/commands/task.go:431:	return fmt.Sprintf("%d", tokens)
./cmd/nightshift/commands/task.go:442:	MinTokens   int    `json:"min_tokens"`
./cmd/nightshift/commands/task.go:443:	MaxTokens   int    `json:"max_tokens"`
./cmd/nightshift/commands/task.go:475:	MinTokens   int    `json:"min_tokens"`
./cmd/nightshift/commands/task.go:476:	MaxTokens   int    `json:"max_tokens"`
./cmd/nightshift/commands/setup_compat_test.go:209:// correctly extracts and matches path tokens.
./cmd/nightshift/commands/setup.go:759:		b.WriteString("Compress prompts via LLM before sending to agents (reduces ARG_MAX risk + token cost).\n\n")
./cmd/nightshift/commands/setup.go:1715:	tokens := strings.FieldsFunc(line, func(r rune) bool {
./cmd/nightshift/commands/setup.go:1726:	for _, token := range tokens {
./cmd/nightshift/commands/setup.go:1727:		if filepath.Clean(token) == target {
./cmd/nightshift/commands/setup.go:2452:		v.Set("jira.token_env", cfg.Jira.TokenEnv)
./cmd/nightshift/commands/setup.go:2491:		v.Set("jira.token_env", "")
./cmd/nightshift/commands/setup.go:3384:		b.WriteString("API token environment variable\n")
./cmd/nightshift/commands/setup.go:3385:		b.WriteString(styleNote.Render("Name of the env var holding your Jira API token (default: JIRA_API_TOKEN)"))
./cmd/nightshift/commands/status.go:148:func formatTokens(tokens int) string {
./cmd/nightshift/commands/status.go:149:	if tokens >= 1000000 {
./cmd/nightshift/commands/status.go:150:		return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
./cmd/nightshift/commands/status.go:152:	if tokens >= 1000 {
./cmd/nightshift/commands/status.go:153:		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
./cmd/nightshift/commands/status.go:155:	return fmt.Sprintf("%d", tokens)
./website/package-lock.json:267:        "js-tokens": "^4.0.0",
./website/package-lock.json:2003:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:2045:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:2072:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:2094:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:2097:    "node_modules/@csstools/css-tokenizer": {
./website/package-lock.json:2099:      "resolved": "https://registry.npmjs.org/@csstools/css-tokenizer/-/css-tokenizer-3.0.4.tgz",
./website/package-lock.json:2136:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:2157:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2247:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2276:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2305:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2334:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2362:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2391:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2420:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:2473:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:2500:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2529:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2667:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2785:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2813:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2840:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2919:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:2994:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:3021:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:3048:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:3115:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:3142:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:3167:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:3193:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:3246:        "@csstools/css-tokenizer": "^3.0.4"
./website/package-lock.json:6867:    "node_modules/[REDACTED_SECRET]": {
./website/package-lock.json:6869:      "resolved": "https://registry.npmjs.org/[REDACTED_SECRET]/-/[REDACTED_SECRET].0.3.tgz",
./website/package-lock.json:9307:        "[REDACTED_SECRET]": "^2.0.0",
./website/package-lock.json:9316:        "[REDACTED_SECRET]": "^2.0.0",
./website/package-lock.json:9335:        "[REDACTED_SECRET]": "^2.0.0",
./website/package-lock.json:9343:        "[REDACTED_SECRET]": "^2.0.0",
./website/package-lock.json:9360:        "[REDACTED_SECRET]": "^2.0.0",
./website/package-lock.json:9363:        "[REDACTED_SECRET]": "^2.0.0",
./website/package-lock.json:9392:        "[REDACTED_SECRET]": "^2.0.0",
./website/package-lock.json:9395:        "[REDACTED_SECRET]": "^2.0.0"
./website/package-lock.json:10281:    "node_modules/js-tokens": {
./website/package-lock.json:10283:      "resolved": "https://registry.npmjs.org/js-tokens/-/js-tokens-4.0.0.tgz",
./website/package-lock.json:10514:        "js-tokens": "^3.0.0 || ^4.0.0"
./website/package-lock.json:11090:        "[REDACTED_SECRET]": "^2.0.0",
./website/package-lock.json:11124:        "[REDACTED_SECRET]": "^2.0.0",
./website/package-lock.json:12718:    "node_modules/[REDACTED_SECRET]": {
./website/package-lock.json:12720:      "resolved": "https://registry.npmjs.org/[REDACTED_SECRET]/-/[REDACTED_SECRET].1.0.tgz",
./website/package-lock.json:12740:    "node_modules/[REDACTED_SECRET]/node_modules/[REDACTED_SECRET]": {
./website/package-lock.json:13403:        "registry-auth-token": "^5.0.1",
./website/package-lock.json:13754:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:13869:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:13897:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:13926:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:14228:        "@csstools/css-tokenizer": "^3.0.4",
./website/package-lock.json:15649:    "node_modules/registry-auth-token": {
./website/package-lock.json:15651:      "resolved": "https://registry.npmjs.org/registry-auth-token/-/[REDACTED_SECRET].1.1.tgz",
./website/package-lock.json:16723:    "node_modules/[REDACTED_SECRET]": {
./website/package-lock.json:16725:      "resolved": "https://registry.npmjs.org/[REDACTED_SECRET]/-/[REDACTED_SECRET].0.2.tgz",
./website/src/pages/index.js:83:          token budget to find dead code, doc drift, test gaps, security issues, and
./website/src/pages/index.js:192:  { icon: 'icon-moon', label: 'Sleep', desc: 'At 2 AM, nightshift picks up your remaining tokens and gets to work.', detail: '2:00 AM', color: 'ns-stepPurple' },
./website/docs/jira.md:60:export [REDACTED_SECRET]="your-api-token"
./website/docs/jira.md:63:Find or create a token at: https://id.atlassian.com/manage-profile/security/api-tokens
./website/docs/jira.md:71:  token_env: [REDACTED_SECRET]
./website/docs/jira.md:109:  token_env: [REDACTED_SECRET]
./website/docs/intro.md:11:Nightshift is a Go CLI tool that runs AI-powered maintenance tasks on your codebase overnight, using your remaining daily token budget from Claude Code, Codex, or GitHub Copilot subscriptions. Wake up to a cleaner codebase without unexpected costs.
./website/docs/intro.md:13:Your tokens get reset every week — you might as well use them. Nightshift runs overnight to find dead code, doc drift, test gaps, security issues, and 59 other things silently accumulating while you ship features.
./website/docs/configuration.md:59:Control how much of your token budget Nightshift uses:
./website/docs/configuration.md:183:  token_env: [REDACTED_SECRET]   # env var holding the API token
./website/docs/configuration.md:233:  token_env: [REDACTED_SECRET]
./website/docs/configuration.md:248:| `[REDACTED_SECRET]` | Jira API token (preferred over config) |
./website/docs/budget.md:8:Nightshift is designed to use tokens you'd otherwise waste. It tracks your remaining budget and never exceeds your configured limits.
./website/docs/budget.md:44:Nightshift infers subscription budgets by correlating local token counts with provider usage percentages.
./website/docs/budget.md:46:Formula: `inferred_budget = local_tokens / (scraped_pct / 100)`
./website/docs/budget.md:69:For API-billed accounts, set explicit token limits:
./website/docs/budget.md:74:  weekly_tokens: 1000000
./website/docs/budget.md:80:`weekly_tokens` and `per_provider` are authoritative for `billing_mode: api`. For subscription users, they act as a fallback until calibration has enough snapshots.
./website/docs/task-reference.md:121:| Low | 10–50k tokens | 5 |
./website/docs/task-reference.md:122:| Medium | 50–150k tokens | 39 |
./website/docs/task-reference.md:123:| High | 150–500k tokens | 13 |
./website/docs/task-reference.md:124:| Very High | 500k+ tokens | 2 |
./website/docs/cli-reference.md:17:| `nightshift budget` | Check token budget status |
./website/docs/cli-reference.md:115:| `--skip-validation` | Skip LLM ticket validation step (saves tokens; preflight and progress output show validation as skipped) |
./website/docs/cli-reference.md:125:| `--validate` | Run LLM validation on each ticket (costs tokens) |
./website/docs/troubleshooting.md:30:- Install tmux or set `budget.billing_mode: api` if you pay per token
./scripts/security-scan.sh:32:grep -RInE --binary-files=without-match --exclude-dir=.git --exclude-dir=vendor --exclude-dir=node_modules -e "InsecureSkipVerify" -e "TLSClientConfig" -e "exec\\.Command.*sh -c" -e "Chmod\\s*\\(0?777|0777" -e "math/rand" -e "(apikey|api_key|secret|token|password)" . > "$outdir/regex-findings.txt" || true
./scripts/security-scan.sh:33:# Redact long potential secrets in findings
./README.md:9:Your tokens get reset every week, you might as well use them. Nightshift runs overnight to find dead code, doc drift, test gaps, security issues, and 59 built-in tasks silently accumulating while you ship features. Like a Roomba for your codebase — runs overnight, worst case you close the PR.
./README.md:199:For non-interactive/daemon usage ensure the GitHub CLI is authenticated (`gh auth login`) and the account or machine token has a Copilot subscription. Nightshift uses the `gh` credential store for Copilot operations; verify with `gh auth status`. For CI or headless runs, provide an authenticated machine user or set a `GH_TOKEN` with appropriate scopes. Nightshift does not store secrets in config files.
./reports/gosec.txt:82:[gosec] 2026/05/17 09:53:13 Checking file: /home/sed/doc/github/nightshift/internal/usage/anthropic_token.go
./reports/gosec.txt:146:[[97;41m/home/sed/doc/github/nightshift/internal/tasks/selector.go:298[0m] - G404 (CWE-338): Use of weak random number generator (math/rand or math/rand/v2 instead of crypto/rand) (Confidence: MEDIUM, Severity: HIGH)
./CLAUDE.md:5:Nightshift is a CLI tool that orchestrates AI coding agents (Claude Code, Codex, GitHub Copilot) to run tasks overnight. It manages token budgets, schedules runs, coordinates parallel agent execution, and generates pull-request-based reports. It finds issues you forgot to look for.
./CLAUDE.md:41:      budget.go         # `nightshift budget` — show token budget usage
./CLAUDE.md:140:    provider.go         # Provider interface: Name(), Execute(), Cost() (inputCents, outputCents per 1K tokens)
./CLAUDE.md:155:                        # masks values for display; never stores secrets
./CLAUDE.md:237:  - GitHub token — Copilot (via `gh auth`)
./CLAUDE.md:238:  - Never put secrets in config files or commit them.
./.plan:8:    "Add package doc comment to internal/trends/analyzer.go: Package trends analyzes historical usage patterns to predict token availability",
./go.sum:164:modernc.org/token v1.1.0 h1:[REDACTED_SECRET]/DM5qcLcYlA8ys6Y=
./go.sum:165:modernc.org/token v1.1.0/go.mod h1:[REDACTED_SECRET]/XTDM=
./.github/workflows/release.yml:31:          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
./.github/workflows/release.yml:32:          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
./.github/workflows/deploy-docs.yml:14:  id-token: write
./SECURITY_AUDIT.md:54:Database directory created with permissions `0755` (rwxr-xr-x), making it world-readable. The database may contain sensitive execution history, token usage data, and task metadata.
./SECURITY_AUDIT.md:228:Files passed to agent are read without validation. Could leak secrets if agent receives unvetted file list.
./.claude/skills/nightshift-release/SKILL.md:79:| Homebrew tap not updated | Check `HOMEBREW_TAP_TOKEN` secret is set in repo settings |
./docs/guides/logging.md:85:**Rule**: default level is `info`. `debug` is for investigating problems. Never log at `debug` in hot paths (tight loops, per-token operations).
./docs/guides/reporting.md:77:      "tokens_used": 8200,
./docs/guides/testing.md:178:export [REDACTED_SECRET]=your-api-token
./docs/guides/testing.md:182:E2e tests are **never run in CI** (no token in CI environment).
./docs/guides/integrations-dev.md:121:    Token   string `mapstructure:"token_env" yaml:"token_env"` // env var name for auth
./docs/guides/adding-tasks.md:29:    CostTier:        CostMedium,       // Estimated token usage
./docs/guides/adding-tasks.md:96:        Scan for hardcoded credentials and secrets.
./docs/guides/state-and-snapshots.md:3:How Nightshift tracks run history, staleness, and token usage over time.
./docs/guides/state-and-snapshots.md:12:| `internal/snapshots` | Periodic token usage snapshots from each provider |
./docs/guides/state-and-snapshots.md:13:| `internal/calibrator` | Infers the weekly token budget from snapshot history |
./docs/guides/state-and-snapshots.md:87:provider's token usage.
./docs/guides/state-and-snapshots.md:96:    LocalTokens      int64      // tokens counted locally
./docs/guides/state-and-snapshots.md:97:    LocalDaily       int64      // tokens counted today
./docs/guides/state-and-snapshots.md:124:in the interactive UI — this is more accurate than local token counting alone.
./docs/guides/state-and-snapshots.md:138:reserve tokens before Nightshift runs overnight.
./docs/guides/state-and-snapshots.md:144:The calibrator answers: *"What is the user's weekly token budget?"* without
./docs/guides/state-and-snapshots.md:158:    InferredBudget int64    // estimated weekly token budget
./docs/guides/architecture.md:45:A provider tracks how many tokens were used and at what cost. An agent is what actually runs. The Jira orchestrator uses agents exclusively; the task orchestrator uses providers for budget tracking and agents for execution.
./docs/guides/architecture.md:104:The single config knob is `budget.max_percent` (default 90). No token arithmetic, no calibration, no modes. See `docs/guides/budget-internals.md` for the formula detail.
./docs/guides/architecture.md:127:- `run_history` — historical runs with token counts, provider, branch
./docs/guides/architecture.md:128:- `snapshots` — token usage snapshots for calibration
./docs/guides/architecture.md:143:- **Config scanning**: `[REDACTED_SECRET]` scans config YAML for patterns like `sk-`, `token:`, `password:`, `secret:`. Returns an error if found.
./docs/guides/architecture.md:171:8. **One credentials home** — all secret access in `internal/security/credentials.go`
./docs/guides/budget-internals.md:33:`weekly_tokens`, etc.) have been removed. Enforcement is purely
./docs/guides/budget-internals.md:34:percentage-based — no token arithmetic.
./docs/guides/database.md:76:    tokens_used         INTEGER NOT NULL,
./docs/guides/security.md:29:| GitHub token (via `gh auth login`) | PR creation, Copilot |
./docs/guides/security.md:139:2. **Credentials in env only**: no secrets on disk outside standard OS credential stores
./docs/guides/tasks-internals.md:30:Estimates the token range for a single run of the task:
./docs/guides/tasks-internals.md:135:1. Budget — skip tasks whose `CostTier.max` exceeds available tokens
