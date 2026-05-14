# Budget Internals

How Nightshift decides whether a provider has capacity for a run.

## Overview

The `internal/budget` package owns all budget enforcement. It sits above the
providers layer (raw API usage) and below the scheduler/orchestrator (which
decides whether to run and which tasks to include).

```
Config (max_percent)
       │
       ▼
   budget.Manager
       │
  ClaudeUsageProvider   ← Anthropic Usage API (five_hour / seven_day / monthly)
  CodexUsageProvider    ← OpenAI Usage API    (primary / secondary windows)
  CopilotUsageProvider  ← GitHub Copilot API  (premium_interactions / monthly)
       │
       ▼
  HourlyCapacityResult  ← gate + display value per provider
```

## Config Reference

```yaml
budget:
  max_percent: 90   # Max % of any quota window to consume (default: 90)
```

All other former fields (`mode`, `billing_mode`, `reserve_percent`,
`weekly_tokens`, etc.) have been removed. Enforcement is purely
percentage-based — no token arithmetic.

## How the Gate Works

`Manager.CheckProviders(providers, ignoreBudget)` returns one `EnforcementResult`
per provider.

```go
type EnforcementResult struct {
    Provider  string
    OK        bool             // false → skip this provider
    Reason    string           // human-readable when not OK
    Allowance *AllowanceResult // nil only on internal error
}
```

**Gate condition:** `OK = ignoreBudget || hcr.Capacity > 0`

Capacity is zero only when a provider has hit its quota limit. Any positive
value (even 1%) is considered runnable.

```go
type AllowanceResult struct {
    HourlyCapacity    float64          // same as HourlyCapacityResult.Capacity
    BottleneckWindow  string           // name of the most-constraining window
    BottleneckUsedPct float64          // usage % in that window
    MaxPercent        int              // limit from config
    Source            string           // "api", "cache", "none"
    Windows           []WindowCapacity // per-window breakdown for display
}
```

## Hourly Capacity

`GetHourlyCapacity(ctx, maxPercent)` is the core call. Each provider fetches
live usage from its API and returns:

```go
type HourlyCapacityResult struct {
    Capacity          float64          // 0–1; overall capacity (min across windows)
    BottleneckWindow  string           // window driving the minimum
    BottleneckUsedPct float64          // usage in that window
    Windows           []WindowCapacity // per-window detail
    Source            string           // "api", "cache", "none"
}

type WindowCapacity struct {
    Name     string
    UsedPct  float64
    ResetIn  time.Duration
    Capacity float64
}
```

**Bottleneck rule:** `Capacity = min(capacity across all windows)`. The most
constraining window determines whether a run is allowed.

## Per-Provider Windows

| Provider | Windows | Source field |
|----------|---------|-------------|
| Claude   | `five_hour` (5h), `seven_day` (168h), `monthly_limit` (720h) | Anthropic Usage API |
| Codex    | `primary` (from `LimitWindowSeconds`), `secondary` (secondary window) | OpenAI Usage API |
| Copilot  | `premium_interactions` (720h monthly) | GitHub Copilot API |

## Hourly Capacity Formula

`computeWindowCapacity(usedPct, maxPct, windowHours, resetHours) float64`

Evaluated independently per window, then minimised. Returns a value in `[0, 1]`.

### Regimes (evaluated in order)

**1. Exhausted** — `remaining ≤ 0`
```
capacity = 0
```
Provider has hit or exceeded `maxPct`. No runs allowed.

**2. Expiring** — `resetHours < windowHours / 4`
```
capacity = min(1, remainingFrac × windowHours / resetHours)
```
Window resets soon. Boost to reflect that the full window rate is achievable
before reset. Takes priority over nearly-depleted: no point conserving
budget that expires in minutes.

Example: 70% used, max=75%, 5h window, 1h to reset →
`(5/75) × 5/1 = 0.33 = 33%`

**3. Nearly depleted** — `remaining < 15 percentage points`
```
capacity = remainingFrac   (= remaining / maxPct)
```
Budget is low and reset is far away. Return the raw fraction, no boost.
Prevents a false sense of capacity when you're close to the wall with
hours still remaining in the window.

Example: 72% used, max=75%, 5h window, 3h to reset →
`3/75 = 4%`

**4. Normal (pace-based)** — all other cases
```
idealRate    = maxPct / windowHours          (target pct/hour to reach maxPct by reset)
headroomRate = remaining / resetHours        (pct/hour you can still sustain)
paceFactor   = max(1, idealRate / headroomRate)
capacity     = min(1, remainingFrac × paceFactor)
```
Boosts capacity when you are behind the ideal consumption pace. If you are
ahead of or on pace, `paceFactor = 1` and capacity equals `remainingFrac`.

Example: 51% used, max=75%, 168h window, 79h to reset →
`idealRate=0.45, headroomRate=0.30, paceFactor=1.5, (24/75)×1.5 = 48%`

### Threshold Values

| Constant | Value | Meaning |
|----------|-------|---------|
| `nearlyDepletedThreshold` | 15.0 pct points | Below this → regime 3 (if not expiring) |
| `windowHours / 4` | varies | Below this reset time → regime 2 |

### Regime Decision Tree

```
remaining ≤ 0?          → 0 (exhausted)
resetHours < window/4?  → min(1, remainingFrac × window/reset)  (expiring, highest priority)
remaining < 15?         → remainingFrac  (nearly depleted, no boost)
default                 → min(1, remainingFrac × max(1, idealRate/headroomRate))  (pace-based)
```

## Testing

Mock providers implement the `ClaudeUsageProvider` / `CodexUsageProvider` /
`CopilotUsageProvider` interfaces and return a synthetic `HourlyCapacityResult`.
`computeWindowCapacity` is unexported but testable from within the `budget`
package.

Use `wantMin`/`wantMax` ranges in table-driven tests to cover all four regimes
and their boundary conditions. The key boundaries to test:

- `remaining` exactly 0 and slightly above 0
- `remaining` exactly at `nearlyDepletedThreshold` (15.0) vs just below
- `resetHours` exactly at `windowHours/4` vs just below
- expiring + nearly-depleted combined (expiring wins)
- normal pace ahead vs behind ideal

## Centralized Enforcement

All commands that run agents go through `Manager.CheckProviders`. No budget
logic is duplicated per command.

| Command | Providers checked | Outcome when exhausted |
|---------|------------------|----------------------|
| `nightshift run` | priority list from config | skip exhausted providers, use first OK |
| `nightshift preview` | priority list | display status for each |
| `nightshift task run` | single `--provider` flag | error |
| `nightshift jira run` | phase providers per project | skip project |
| `nightshift jira preview` | all phase providers across projects | display per-provider capacity |

### `--ignore-budget` Flag

All commands accept `--ignore-budget`. When set, `CheckProviders` returns
`OK=true` for every provider regardless of usage. A warning is printed.
