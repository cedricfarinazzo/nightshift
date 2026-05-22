package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SchedulerFailure records a single scheduler job failure.
type SchedulerFailure struct {
	ID        int64
	JobName   string
	FailedAt  time.Time
	ErrorText string
}

// RecordSchedulerFailure inserts a failure row for jobName.
func RecordSchedulerFailure(ctx context.Context, database *DB, jobName string, failedAt time.Time, errText string) error {
	_, err := database.sql.ExecContext(ctx,
		`INSERT INTO scheduler_job_failures(job_name, failed_at, error_text) VALUES(?,?,?)`,
		jobName, failedAt.UTC().Format(time.RFC3339Nano), errText,
	)
	if err != nil {
		return fmt.Errorf("record scheduler failure: %w", err)
	}
	return nil
}

// LastSchedulerFailure returns the most recent failure for jobName.
// ok is false when no failure exists.
func LastSchedulerFailure(ctx context.Context, database *DB, jobName string) (failedAt time.Time, errText string, ok bool, err error) {
	row := database.sql.QueryRowContext(ctx,
		`SELECT failed_at, error_text FROM scheduler_job_failures WHERE job_name=? ORDER BY failed_at DESC LIMIT 1`,
		jobName,
	)
	var ts string
	if scanErr := row.Scan(&ts, &errText); scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return time.Time{}, "", false, nil
		}
		return time.Time{}, "", false, fmt.Errorf("last scheduler failure: %w", scanErr)
	}
	failedAt, err = time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		// Try fallback RFC3339
		failedAt, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			return time.Time{}, "", false, fmt.Errorf("parse failed_at: %w", err)
		}
	}
	return failedAt, errText, true, nil
}

// RecentSchedulerFailures returns up to limit failures for jobName, newest first.
func RecentSchedulerFailures(ctx context.Context, database *DB, jobName string, limit int) ([]SchedulerFailure, error) {
	rows, err := database.sql.QueryContext(ctx,
		`SELECT id, job_name, failed_at, error_text FROM scheduler_job_failures WHERE job_name=? ORDER BY failed_at DESC LIMIT ?`,
		jobName, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("recent scheduler failures: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []SchedulerFailure
	for rows.Next() {
		var f SchedulerFailure
		var ts string
		if err := rows.Scan(&f.ID, &f.JobName, &ts, &f.ErrorText); err != nil {
			return nil, fmt.Errorf("scan scheduler failure: %w", err)
		}
		f.FailedAt, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			f.FailedAt, err = time.Parse(time.RFC3339, ts)
			if err != nil {
				return nil, fmt.Errorf("parse failed_at: %w", err)
			}
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// CountSchedulerFailuresSince returns the number of failures for jobName since the given time.
func CountSchedulerFailuresSince(ctx context.Context, database *DB, jobName string, since time.Time) (int, error) {
	row := database.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM scheduler_job_failures WHERE job_name=? AND failed_at >= ?`,
		jobName, since.UTC().Format(time.RFC3339Nano),
	)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("count scheduler failures: %w", err)
	}
	return n, nil
}

// DistinctSchedulerJobNames returns all job names that have recorded failures.
func DistinctSchedulerJobNames(ctx context.Context, database *DB) ([]string, error) {
	rows, err := database.sql.QueryContext(ctx,
		`SELECT DISTINCT job_name FROM scheduler_job_failures ORDER BY job_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("distinct scheduler job names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan job name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
