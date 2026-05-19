# Budget

Nightshift gates every run on live provider usage. One knob, no calibration.

## Config

```yaml
budget:
  max_percent: 90        # stop running once usage exceeds this %
```

That's it. No reserve, no daily/weekly mode, no token arithmetic.

## How It Works

For each enabled provider:

1. Call the provider's Usage API to get per-window utilisation. For Claude: `five_hour`, `seven_day`, `monthly_limit`.
2. For each window, compute remaining hourly capacity (`computeWindowCapacity`).
3. Take the minimum across windows — this is the provider's current capacity.
4. Run is gated: `OK = ignoreBudget || capacity > 0`.

Capacity > 0 means at least one window has headroom. The exact percent threshold is configurable via `max_percent`.

## Inspecting Budget

```bash
nightshift budget                  # all providers
nightshift budget --provider claude
```

Shows current utilisation per window + computed capacity.

## Snapshots

Nightshift periodically snapshots provider usage to SQLite (`snapshots` table). Used for historical visualisation:

```bash
nightshift budget snapshot --local-only
nightshift budget history -n 20
```

## Bypassing Budget

```bash
nightshift run --ignore-budget
```

Prints a warning. Useful for testing or known-cheap one-off tasks.

## Provider-specific Notes

- **Claude**: Anthropic Usage API surfaces 5h, 7d, monthly windows.
- **Codex**: OpenAI usage tracked via API.
- **Copilot**: Request-count tracking (monthly). No token API exposure.

## See Also

- [Budget Internals](../dev/budget-internals.md) — capacity formula, provider interfaces
