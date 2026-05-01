# Database Guide

Nightshift uses SQLite via `modernc.org/sqlite` (pure Go — no CGO). All database code lives exclusively in `internal/db/`.

---

## Location

```
~/.local/share/nightshift/nightshift.db
```

The directory is created with `0700` permissions (owner only).

Override with `--db-path` flag or `storage.db_path` in config.

---

## Opening the Database

`db.Open(path string)` does everything in order:

1. Creates the directory with `0700` permissions
2. Opens the SQLite connection
3. Applies PRAGMA settings
4. Runs all pending migrations
5. Imports legacy state (if upgrading from pre-DB versions)

```go
db, err := db.Open("")          // uses DefaultPath()
db, err := db.Open(":memory:")  // in-memory (tests)
db, err := db.Open("/tmp/x.db") // custom path
```

Always use `db.Open()` — never `sql.Open("sqlite", ...)` directly.

---

## Schema

Current schema (migration 005):

```sql
-- Projects scanned by nightshift
CREATE TABLE projects (
    path        TEXT PRIMARY KEY,
    last_run    DATETIME,
    run_count   INTEGER NOT NULL DEFAULT 0
);

-- Per-task history per project
CREATE TABLE task_history (
    project_path TEXT NOT NULL,
    task_type    TEXT NOT NULL,
    last_run     DATETIME NOT NULL,
    PRIMARY KEY (project_path, task_type)
);

-- Per-run records
CREATE TABLE run_history (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    project_path TEXT NOT NULL,
    task_type    TEXT NOT NULL,
    started_at   DATETIME NOT NULL,
    duration_ms  INTEGER,
    success      BOOLEAN,
    output       TEXT,
    provider     TEXT NOT NULL DEFAULT '',
    branch       TEXT
);

-- Token usage snapshots (for budget calibration)
CREATE TABLE snapshots (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    provider            TEXT NOT NULL,
    tokens_used         INTEGER NOT NULL,
    captured_at         DATETIME NOT NULL,
    session_reset_time  TEXT,
    weekly_reset_time   TEXT
);

-- Assigned tasks for scheduling
CREATE TABLE assigned_tasks (
    project_path TEXT NOT NULL,
    task_type    TEXT NOT NULL,
    assigned_at  DATETIME NOT NULL,
    PRIMARY KEY (project_path, task_type)
);

-- Bus factor analysis results
CREATE TABLE bus_factor_results (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    component    TEXT NOT NULL,
    timestamp    DATETIME NOT NULL,
    metrics      TEXT NOT NULL,         -- JSON
    contributors TEXT NOT NULL,         -- JSON
    risk_level   TEXT NOT NULL,
    report_path  TEXT
);

CREATE INDEX idx_bus_factor_component_time ON bus_factor_results(component, timestamp DESC);
```

---

## Migrations

Migrations are in `internal/db/migrations.go` as versioned SQL strings. They are applied automatically by `db.Open()` — no manual migration step needed.

### How they work

```go
type Migration struct {
    Version     int
    Description string
    SQL         string
}
```

`Migrate(db)` finds the current schema version (stored in a `schema_version` table), runs all migrations with a version number higher than current, and updates the version. It is transactional — a failed migration rolls back.

### Adding a new migration

1. Add a new `const migrationNNNSQL` string with your SQL:

```go
const migration006SQL = `
ALTER TABLE run_history ADD COLUMN cost_cents INTEGER NOT NULL DEFAULT 0;
`
```

2. Append it to the `migrations` slice in `migrations.go`:

```go
{
    Version:     6,
    Description: "add cost_cents to run_history",
    SQL:         migration006SQL,
},
```

**Rules:**
- Never modify an existing migration — it may have already been applied in production
- Versions must be strictly ascending integers
- Prefer `ALTER TABLE ADD COLUMN` with a `DEFAULT` value to avoid backfill issues
- New tables should use `CREATE TABLE IF NOT EXISTS`
- New indexes should use `CREATE INDEX IF NOT EXISTS`

---

## PRAGMA Settings

Applied on every open:

```sql
PRAGMA journal_mode=WAL;       -- Write-ahead logging for better concurrency
PRAGMA synchronous=NORMAL;     -- Balance durability/performance
PRAGMA foreign_keys=ON;        -- Enforce FK constraints
PRAGMA busy_timeout=5000;      -- Wait 5s before returning SQLITE_BUSY
```

---

## Rules

- **All SQL lives in `internal/db/`** — no raw SQL strings anywhere else in the codebase
- Use `modernc.org/sqlite` only — never `mattn/go-sqlite3` (requires CGO)
- Pass `context.Context` to all query methods
- Wrap errors with context: `fmt.Errorf("inserting run: %w", err)`
