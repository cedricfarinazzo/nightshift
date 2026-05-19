# Integrations

> **Note:** The `internal/integrations/` package was removed. It was never imported by production code.
> The archived version of this guide is in `docs/deprecated/integrations-dev.md`.

---

## How Context Is Injected Today

CLAUDE.md and other project-level context is passed directly as a `Hint` in
`cmd/nightshift/commands/daemon.go` when building the orchestrator's `RunMetadata`.
There is no generic `Reader` interface or `Manager` — context injection is handled
inline at the call site.

If you need to add a new context source, inject it at the point where `RunMetadata`
or the agent prompt is built in `daemon.go` or the relevant command file.
