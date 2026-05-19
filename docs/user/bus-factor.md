# Bus Factor

Measures how concentrated code ownership is in a repo. Low bus factor = high "if X leaves, we're stuck" risk.

## Run

```bash
nightshift busfactor                          # all configured projects
nightshift busfactor --project ~/code/repo
nightshift busfactor --since 6m               # last 6 months only
nightshift busfactor --json
```

## What It Outputs

| Metric | Meaning |
|--------|---------|
| **Bus factor** | Min contributors whose absence cripples the project |
| **HHI** (Herfindahl) | Concentration index; 0 = perfectly distributed, 10000 = single owner |
| **Gini** | Inequality coefficient; 0 = equal, 1 = monopoly |
| **Risk level** | `low` / `medium` / `high` / `critical` |
| **Contributor share** | Top-N committers + their share of LOC touched |
| **Remediation suggestions** | Areas with thinnest coverage, pairing recommendations |

## Risk Levels

- **Critical**: 1–2 contributors own most code; immediate action
- **High**: <5 active contributors or heavy concentration
- **Medium**: healthy but improvable
- **Low**: well-distributed

## How It Works

`internal/analysis/analyzer.go` parses `git log` for committer counts + line changes, then computes HHI/Gini in `metrics.go`. Results persist to SQLite (`bus_factor_results`) for trend tracking.

## See Also

- [Bus Factor Internals](../dev/bus-factor.md)
