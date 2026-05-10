// Package db provides SQLite-backed storage for nightshift state and snapshots.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection and path.
type DB struct {
	sql  *sql.DB
	path string
}

// DefaultPath returns the default database path.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "nightshift", "nightshift.db")
}

// Open opens or creates the database, applies pragmas, and runs migrations.
func Open(dbPath string) (*DB, error) {
	if dbPath == "" {
		dbPath = DefaultPath()
	}

	resolved := expandPath(dbPath)
	// SECURITY: Use 0700 (rwx------) for database directory to restrict access to owner only
	if err := os.MkdirAll(filepath.Dir(resolved), 0700); err != nil {
		return nil, fmt.Errorf("creating db dir: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", resolved)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if err := applyPragmas(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	if err := Migrate(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	if err := importLegacyState(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return &DB{sql: sqlDB, path: resolved}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	if d == nil || d.sql == nil {
		return nil
	}
	return d.sql.Close()
}

// SQL returns the raw *sql.DB for advanced usage.
func (d *DB) SQL() *sql.DB {
	if d == nil {
		return nil
	}
	return d.sql
}

func applyPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("setting pragma %q: %w", pragma, err)
		}
	}
	return nil
}

func expandPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if path == "~" {
		return home
	}

	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}

	return path
}

// --- Jira run history ---

// JiraRun holds a summary row for a single nightshift jira run CLI invocation.
type JiraRun struct {
	RunID            string
	StartedAt        time.Time
	EndedAt          *time.Time
	ProjectKey       string
	TicketsProcessed int
	TicketsCompleted int
	TicketsFailed    int
}

// JiraTicketResult holds the final outcome of a single ticket within a run.
type JiraTicketResult struct {
	ID           int64
	RunID        string
	TicketKey    string
	Status       string
	DurationMs   int64
	PhaseReached string
	PRURL        string
	ErrorMsg     string
}

// JiraPhaseLog holds data for a single phase execution within a ticket run.
type JiraPhaseLog struct {
	ID         int64
	RunID      string
	TicketKey  string
	Phase      string
	Provider   string
	Model      string
	StartedAt  time.Time
	DurationMs int64
	ExitOk     bool
	Output     string
	Error      string
}

// SaveJiraRun inserts a new jira_runs row. Call before the ticket processing loop.
func (d *DB) SaveJiraRun(ctx context.Context, runID, projectKey string, startedAt time.Time) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("db not open")
	}
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO jira_runs (run_id, started_at, project_key) VALUES (?, ?, ?)`,
		runID, startedAt.UTC().Format(time.RFC3339), projectKey,
	)
	return err
}

// UpdateJiraRun updates the ended_at and ticket counters of a jira_runs row.
func (d *DB) UpdateJiraRun(ctx context.Context, runID string, endedAt time.Time, processed, completed, failed int) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("db not open")
	}
	_, err := d.sql.ExecContext(ctx,
		`UPDATE jira_runs SET ended_at=?, tickets_processed=?, tickets_completed=?, tickets_failed=? WHERE run_id=?`,
		endedAt.UTC().Format(time.RFC3339), processed, completed, failed, runID,
	)
	return err
}

// SaveJiraTicketResult inserts a jira_ticket_results row.
func (d *DB) SaveJiraTicketResult(ctx context.Context, r JiraTicketResult) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("db not open")
	}
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO jira_ticket_results (run_id, ticket_key, status, duration_ms, phase_reached, pr_url, error_msg)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.RunID, r.TicketKey, r.Status, r.DurationMs, r.PhaseReached, nullableString(r.PRURL), nullableString(r.ErrorMsg),
	)
	return err
}

// SaveJiraPhaseLog inserts a jira_phase_logs row.
func (d *DB) SaveJiraPhaseLog(ctx context.Context, l JiraPhaseLog) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("db not open")
	}
	exitOk := 0
	if l.ExitOk {
		exitOk = 1
	}
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO jira_phase_logs (run_id, ticket_key, phase, provider, model, started_at, duration_ms, exit_ok, output, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.RunID, l.TicketKey, l.Phase, nullableString(l.Provider), nullableString(l.Model),
		l.StartedAt.UTC().Format(time.RFC3339), l.DurationMs, exitOk,
		nullableString(l.Output), nullableString(l.Error),
	)
	return err
}

// GetLatestJiraRunID returns the run_id of the most recently started jira run.
// Returns ("", nil) when no runs exist.
func (d *DB) GetLatestJiraRunID(ctx context.Context) (string, error) {
	if d == nil || d.sql == nil {
		return "", fmt.Errorf("db not open")
	}
	row := d.sql.QueryRowContext(ctx, `SELECT run_id FROM jira_runs ORDER BY started_at DESC LIMIT 1`)
	var runID string
	if err := row.Scan(&runID); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return runID, nil
}

// GetJiraTicketResults returns all jira_ticket_results rows for the given run.
func (d *DB) GetJiraTicketResults(ctx context.Context, runID string) ([]JiraTicketResult, error) {
	if d == nil || d.sql == nil {
		return nil, fmt.Errorf("db not open")
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, run_id, ticket_key, status, duration_ms, phase_reached, COALESCE(pr_url,''), COALESCE(error_msg,'')
		 FROM jira_ticket_results WHERE run_id=? ORDER BY id`,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []JiraTicketResult
	for rows.Next() {
		var r JiraTicketResult
		if err := rows.Scan(&r.ID, &r.RunID, &r.TicketKey, &r.Status, &r.DurationMs, &r.PhaseReached, &r.PRURL, &r.ErrorMsg); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetJiraPhaseLogs returns jira_phase_logs rows filtered by runID, and optionally by ticketKey and phase.
// Empty ticketKey or phase means "no filter on that field".
func (d *DB) GetJiraPhaseLogs(ctx context.Context, runID, ticketKey, phase string) ([]JiraPhaseLog, error) {
	if d == nil || d.sql == nil {
		return nil, fmt.Errorf("db not open")
	}
	query := `SELECT id, run_id, ticket_key, phase, COALESCE(provider,''), COALESCE(model,''),
	           started_at, duration_ms, exit_ok, COALESCE(output,''), COALESCE(error,'')
	           FROM jira_phase_logs WHERE run_id=?`
	args := []any{runID}
	if ticketKey != "" {
		query += " AND ticket_key=?"
		args = append(args, ticketKey)
	}
	if phase != "" {
		query += " AND phase=?"
		args = append(args, phase)
	}
	query += " ORDER BY id"

	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []JiraPhaseLog
	for rows.Next() {
		var l JiraPhaseLog
		var startedAtStr string
		var exitOk int
		if err := rows.Scan(&l.ID, &l.RunID, &l.TicketKey, &l.Phase, &l.Provider, &l.Model,
			&startedAtStr, &l.DurationMs, &exitOk, &l.Output, &l.Error); err != nil {
			return nil, err
		}
		l.ExitOk = exitOk != 0
		if t, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
			l.StartedAt = t
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// GetJiraRun returns the jira_runs row for the given run_id.
func (d *DB) GetJiraRun(ctx context.Context, runID string) (*JiraRun, error) {
	if d == nil || d.sql == nil {
		return nil, fmt.Errorf("db not open")
	}
	row := d.sql.QueryRowContext(ctx,
		`SELECT run_id, started_at, ended_at, project_key, tickets_processed, tickets_completed, tickets_failed
		 FROM jira_runs WHERE run_id=?`, runID)
	var r JiraRun
	var startedStr string
	var endedStr sql.NullString
	if err := row.Scan(&r.RunID, &startedStr, &endedStr, &r.ProjectKey,
		&r.TicketsProcessed, &r.TicketsCompleted, &r.TicketsFailed); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339, startedStr); err == nil {
		r.StartedAt = t
	}
	if endedStr.Valid && endedStr.String != "" {
		if t, err := time.Parse(time.RFC3339, endedStr.String); err == nil {
			r.EndedAt = &t
		}
	}
	return &r, nil
}

// nullableString returns nil for empty strings so they become SQL NULL.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
