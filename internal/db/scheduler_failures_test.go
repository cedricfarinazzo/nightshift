package db

import (
	"context"
	"testing"
	"time"
)

func openSchedulerTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestRecordAndLastSchedulerFailure(t *testing.T) {
	database := openSchedulerTestDB(t)
	ctx := context.Background()

	ts1 := time.Now().UTC().Truncate(time.Second)
	ts2 := ts1.Add(time.Minute)

	if err := RecordSchedulerFailure(ctx, database, "job-a", ts1, "first error"); err != nil {
		t.Fatalf("RecordSchedulerFailure: %v", err)
	}
	if err := RecordSchedulerFailure(ctx, database, "job-a", ts2, "second error"); err != nil {
		t.Fatalf("RecordSchedulerFailure: %v", err)
	}

	failedAt, errText, ok, err := LastSchedulerFailure(ctx, database, "job-a")
	if err != nil {
		t.Fatalf("LastSchedulerFailure: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if errText != "second error" {
		t.Errorf("errText = %q, want %q", errText, "second error")
	}
	if !failedAt.Equal(ts2) {
		t.Errorf("failedAt = %v, want %v", failedAt, ts2)
	}
}

func TestLastSchedulerFailure_NoRows(t *testing.T) {
	database := openSchedulerTestDB(t)
	ctx := context.Background()

	_, _, ok, err := LastSchedulerFailure(ctx, database, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for empty table")
	}
}

func TestRecentSchedulerFailures_Ordering(t *testing.T) {
	database := openSchedulerTestDB(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		if err := RecordSchedulerFailure(ctx, database, "job-b", base.Add(time.Duration(i)*time.Minute), "err"); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	results, err := RecentSchedulerFailures(ctx, database, "job-b", 3)
	if err != nil {
		t.Fatalf("RecentSchedulerFailures: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	// Newest first.
	if !results[0].FailedAt.After(results[1].FailedAt) {
		t.Errorf("results not ordered newest-first")
	}
}

func TestCountSchedulerFailuresSince(t *testing.T) {
	database := openSchedulerTestDB(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)
	// 3 recent + 2 old
	for i := 0; i < 3; i++ {
		_ = RecordSchedulerFailure(ctx, database, "job-c", base.Add(time.Duration(i)*time.Minute), "err")
	}
	for i := 0; i < 2; i++ {
		_ = RecordSchedulerFailure(ctx, database, "job-c", base.Add(-time.Duration(i+1)*time.Hour), "old err")
	}

	count, err := CountSchedulerFailuresSince(ctx, database, "job-c", base.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("CountSchedulerFailuresSince: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestDistinctSchedulerJobNames(t *testing.T) {
	database := openSchedulerTestDB(t)
	ctx := context.Background()

	ts := time.Now().UTC()
	_ = RecordSchedulerFailure(ctx, database, "alpha", ts, "e")
	_ = RecordSchedulerFailure(ctx, database, "beta", ts, "e")
	_ = RecordSchedulerFailure(ctx, database, "alpha", ts.Add(time.Second), "e")

	names, err := DistinctSchedulerJobNames(ctx, database)
	if err != nil {
		t.Fatalf("DistinctSchedulerJobNames: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2: %v", len(names), names)
	}
	if names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("names = %v, want [alpha beta]", names)
	}
}
