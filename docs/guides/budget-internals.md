# Budget Internals

How Nightshift calculates the token budget available for each run.

## Overview

The `internal/budget` package owns all budget calculations. It sits above the
providers layer (which tracks raw usage) and below the scheduler/orchestrator
(which decides whether to run and how many tasks to include).

```
Config / Calibrator
       │
       ▼
   budget.Manager
       │
  ClaudeUsageProvider
  CodexUsageProvider
  CopilotUsageProvider
       │
       ▼
  Available tokens for this run
```

## Budget Modes

Set `budget.billing_mode` in config:

| Mode | Description |
|------|-------------|
| `subscription` (default) | Uses a fixed weekly token pool shared across the week |
| `api` | Uses a per-run token budget drawn from direct API credits |

### Daily Mode vs Weekly Mode

Within subscription mode, set `budget.mode`:

| Value | How available tokens are calculated |
|-------|-------------------------------------|
| `daily` | `(weekly_budget / 7) − today_used − reserve` |
| `weekly` | `weekly_budget − week_used − reserve` |

### Aggressive End-of-Week

When `budget.aggressive_end_of_week: true` and it is Thursday–Friday (the last
two days of the billing week), the reserve is ignored and all remaining tokens
are available:

```
available = remaining_weekly_budget − daytime_reserve
```

This lets Nightshift use up leftover tokens before the weekly counter resets.

## Reserve

`budget.reserve_percent` (default `0.1` = 10%) carves out a fraction of the
base budget that Nightshift will not touch. This protects against overspending
when usage estimates are slightly off.

```
reserve = base_budget × reserve_percent
available = base_budget − reserve − daytime_prediction
```

## Daytime Usage Prediction

`TrendAnalyzer.PredictDaytimeUsage` estimates how many tokens you will spend
during business hours and subtracts that from the overnight budget. This
prevents Nightshift from consuming tokens you need for interactive work during
the day.

The field `AllowanceNoDaytime` on `BudgetResult` gives the raw figure before
subtracting the daytime prediction.

## Budget Result Fields

```go
type BudgetResult struct {
    Available         int64   // Final available tokens for this run
    AllowanceNoDaytime int64  // Before daytime prediction deduction
    WeeklyBudget      int64   // Full weekly token budget used as base
    BudgetBase        int64   // Base budget (daily slice or remaining weekly)
    RemainingBudget   int64   // Remaining weekly budget (weekly mode only)
    UsedPercent       float64 // Current usage percentage
    UsedPercentSource string  // Where the usage figure came from
    ReserveAmount     int64   // Tokens reserved
}
```

## Calibration

When `budget.calibrate: true`, the calibrator (`internal/calibrator`) infers
the weekly budget from past snapshots rather than reading it from config.

### Calibration Pipeline

```
Usage snapshots  →  Calibrator.Calibrate(provider)
                          │
                    CalibrationResult{
                      InferredBudget, Confidence,
                      SampleCount, Variance, Source
                    }
                          │
              budget.Manager (via WithBudgetSource)
```

Confidence levels:

| Level | Meaning |
|-------|---------|
| `high` | API billing mode — budget read directly from config |
| `medium` | ≥4 snapshots, low variance |
| `low` | 1–3 snapshots, or high variance |
| `none` | Calibration disabled — falls back to config value |

## Budget Source Priority

When `Manager` resolves the weekly budget, it checks in order:

1. Injected `BudgetSource` (calibrator or external) — `WithBudgetSource`
2. Config value: `providers.<name>.weekly_budget`

## Copilot Budget

Copilot uses **monthly premium request limits** rather than weekly token counts.
`CopilotUsageProvider.GetUsedPercent(mode, monthlyLimit)` returns the fraction
of the monthly cap already consumed. The budget manager treats this identically
to token budgets in terms of the available/reserve math.

## Manager Interfaces

```go
type ClaudeUsageProvider interface {
    Name() string
    GetUsedPercent(mode string, weeklyBudget int64) (float64, error)
}

type CodexUsageProvider interface {
    Name() string
    GetUsedPercent(mode string, weeklyBudget int64) (float64, error)
    GetResetTime(mode string) (time.Time, error)
}

type CopilotUsageProvider interface {
    Name() string
    GetUsedPercent(mode string, monthlyLimit int64) (float64, error)
    GetResetTime(mode string) (time.Time, error)
}
```

## Config Reference

```yaml
budget:
  mode: daily          # daily | weekly
  billing_mode: subscription   # subscription | api
  reserve_percent: 0.10        # fraction to keep in reserve
  aggressive_end_of_week: false
  calibrate: true              # infer budget from snapshots
  weekly_budget: 70000         # fallback if calibration off
  copilot_monthly_limit: 300   # max premium Copilot requests/month
```

## Testing

Inject a `nowFunc` on `Manager` via the internal field to control time:

```go
mgr := budget.NewManager(cfg, claude, codex, copilot)
mgr.nowFunc = func() time.Time { return fixedTime }
```

The `BudgetSource` interface lets tests inject a fixed weekly budget without
needing real snapshots:

```go
type fixedBudget struct{ tokens int64 }
func (f fixedBudget) GetBudget(string) (budget.BudgetEstimate, error) {
    return budget.BudgetEstimate{WeeklyTokens: f.tokens, Source: "test"}, nil
}
mgr := budget.NewManager(cfg, ..., budget.WithBudgetSource(fixedBudget{70_000}))
```
