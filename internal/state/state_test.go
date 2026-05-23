package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/db"
)

func TestNewNilDB(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestProjectRunTracking(t *testing.T) {
	s := newTestState(t)

	project := "/path/to/project"

	if s.WasProcessedToday(project) {
		t.Error("WasProcessedToday() = true for new project, want false")
	}

	if err := s.RecordProjectRun(project); err != nil {
		t.Fatalf("RecordProjectRun() error = %v", err)
	}

	if !s.WasProcessedToday(project) {
		t.Error("WasProcessedToday() = false after recording run, want true")
	}

	lastRun := s.LastProjectRun(project)
	if time.Since(lastRun) > time.Second {
		t.Errorf("LastProjectRun() = %v, expected recent time", lastRun)
	}
}

func TestTaskRunTracking(t *testing.T) {
	s := newTestState(t)

	project := "/path/to/project"
	taskType := "lint"

	lastRun := s.LastTaskRun(project, taskType)
	if !lastRun.IsZero() {
		t.Errorf("LastTaskRun() = %v, want zero time for untracked task", lastRun)
	}

	if err := s.RecordTaskRun(project, taskType); err != nil {
		t.Fatalf("RecordTaskRun() error = %v", err)
	}

	lastRun = s.LastTaskRun(project, taskType)
	if time.Since(lastRun) > time.Second {
		t.Errorf("LastTaskRun() = %v, expected recent time", lastRun)
	}
}

func TestDaysSinceLastRun(t *testing.T) {
	s := newTestState(t)

	project := "/path/to/project"
	taskType := "lint"

	days := s.DaysSinceLastRun(project, taskType)
	if days != -1 {
		t.Errorf("DaysSinceLastRun() = %d for never-run task, want -1", days)
	}

	if err := s.RecordTaskRun(project, taskType); err != nil {
		t.Fatalf("RecordTaskRun() error = %v", err)
	}

	days = s.DaysSinceLastRun(project, taskType)
	if days != 0 {
		t.Errorf("DaysSinceLastRun() = %d for today, want 0", days)
	}
}

func TestStalenessBonus(t *testing.T) {
	s := newTestState(t)

	project := "/path/to/project"
	taskType := "lint"

	bonus := s.StalenessBonus(project, taskType)
	if bonus != 3.0 {
		t.Errorf("StalenessBonus() = %f for never-run task, want 3.0", bonus)
	}

	if err := s.RecordTaskRun(project, taskType); err != nil {
		t.Fatalf("RecordTaskRun() error = %v", err)
	}

	bonus = s.StalenessBonus(project, taskType)
	if bonus != 0.0 {
		t.Errorf("StalenessBonus() = %f for today, want 0.0", bonus)
	}
}

func TestAssignedTasks(t *testing.T) {
	s := newTestState(t)

	taskID := "task-123"
	project := "/path/to/project"
	taskType := "lint"

	if s.IsAssigned(taskID) {
		t.Error("IsAssigned() = true for new task, want false")
	}

	if err := s.MarkAssigned(taskID, project, taskType); err != nil {
		t.Fatalf("MarkAssigned() error = %v", err)
	}

	if !s.IsAssigned(taskID) {
		t.Error("IsAssigned() = false after marking, want true")
	}

	info, ok := s.GetAssigned(taskID)
	if !ok {
		t.Error("GetAssigned() ok = false, want true")
	}
	if info.TaskID != taskID {
		t.Errorf("GetAssigned().TaskID = %s, want %s", info.TaskID, taskID)
	}
	if info.TaskType != taskType {
		t.Errorf("GetAssigned().TaskType = %s, want %s", info.TaskType, taskType)
	}

	if err := s.ClearAssigned(taskID); err != nil {
		t.Fatalf("ClearAssigned() error = %v", err)
	}

	if s.IsAssigned(taskID) {
		t.Error("IsAssigned() = true after clearing, want false")
	}
}

func TestClearAllAssigned(t *testing.T) {
	s := newTestState(t)

	if err := s.MarkAssigned("task-1", "/project", "lint"); err != nil {
		t.Fatalf("MarkAssigned() error = %v", err)
	}
	if err := s.MarkAssigned("task-2", "/project", "docs"); err != nil {
		t.Fatalf("MarkAssigned() error = %v", err)
	}

	if err := s.ClearAllAssigned(); err != nil {
		t.Fatalf("ClearAllAssigned() error = %v", err)
	}

	if s.IsAssigned("task-1") || s.IsAssigned("task-2") {
		t.Error("IsAssigned() = true after ClearAllAssigned(), want false")
	}
}

func TestListAssigned(t *testing.T) {
	s := newTestState(t)

	if err := s.MarkAssigned("task-1", "/project", "lint"); err != nil {
		t.Fatalf("MarkAssigned() error = %v", err)
	}
	if err := s.MarkAssigned("task-2", "/project", "docs"); err != nil {
		t.Fatalf("MarkAssigned() error = %v", err)
	}

	tasks := s.ListAssigned()
	if len(tasks) != 2 {
		t.Errorf("ListAssigned() returned %d tasks, want 2", len(tasks))
	}
}

func TestPersistence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")

	db1, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	defer func() { _ = db1.Close() }()

	s1, err := New(db1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	project := "/path/to/project"
	if err := s1.RecordProjectRun(project); err != nil {
		t.Fatalf("RecordProjectRun() error = %v", err)
	}
	if err := s1.RecordTaskRun(project, "lint"); err != nil {
		t.Fatalf("RecordTaskRun() error = %v", err)
	}
	if err := s1.MarkAssigned("task-123", project, "lint"); err != nil {
		t.Fatalf("MarkAssigned() error = %v", err)
	}

	if err := db1.Close(); err != nil {
		t.Fatalf("close db1: %v", err)
	}

	db2, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer func() { _ = db2.Close() }()

	s2, err := New(db2)
	if err != nil {
		t.Fatalf("New() second instance error = %v", err)
	}

	if !s2.WasProcessedToday(project) {
		t.Error("Persistence: WasProcessedToday() = false, want true")
	}

	lastRun := s2.LastTaskRun(project, "lint")
	if lastRun.IsZero() {
		t.Error("Persistence: LastTaskRun() is zero, want recorded time")
	}

	if !s2.IsAssigned("task-123") {
		t.Error("Persistence: IsAssigned() = false, want true")
	}
}

func TestPathNormalization(t *testing.T) {
	s := newTestState(t)

	if err := s.RecordProjectRun("/path/to/project/"); err != nil {
		t.Fatalf("RecordProjectRun() error = %v", err)
	}

	if !s.WasProcessedToday("/path/to/project") {
		t.Error("Path normalization: trailing slash not normalized")
	}
}

func TestClearStaleAssignments(t *testing.T) {
	s := newTestState(t)

	if err := s.MarkAssigned("task-1", "/project", "lint"); err != nil {
		t.Fatalf("MarkAssigned() error = %v", err)
	}

	cleared, err := s.ClearStaleAssignments(0)
	if err != nil {
		t.Fatalf("ClearStaleAssignments() error = %v", err)
	}
	if cleared != 1 {
		t.Errorf("ClearStaleAssignments() = %d, want 1", cleared)
	}

	if s.IsAssigned("task-1") {
		t.Error("IsAssigned() = true after clearing stale, want false")
	}
}

func TestIsSameDay(t *testing.T) {
	tests := []struct {
		name string
		t1   time.Time
		t2   time.Time
		want bool
	}{
		{
			name: "same day same time",
			t1:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			t2:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "same day different time",
			t1:   time.Date(2024, 1, 15, 2, 0, 0, 0, time.UTC),
			t2:   time.Date(2024, 1, 15, 23, 59, 59, 0, time.UTC),
			want: true,
		},
		{
			name: "different day",
			t1:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			t2:   time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "different month",
			t1:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			t2:   time.Date(2024, 2, 15, 10, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "different year",
			t1:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			t2:   time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSameDay(tt.t1, tt.t2); got != tt.want {
				t.Errorf("isSameDay() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProjectCount(t *testing.T) {
	s := newTestState(t)

	if s.ProjectCount() != 0 {
		t.Errorf("ProjectCount() = %d, want 0", s.ProjectCount())
	}

	if err := s.RecordProjectRun("/project1"); err != nil {
		t.Fatalf("RecordProjectRun() error = %v", err)
	}
	if err := s.RecordProjectRun("/project2"); err != nil {
		t.Fatalf("RecordProjectRun() error = %v", err)
	}

	if s.ProjectCount() != 2 {
		t.Errorf("ProjectCount() = %d, want 2", s.ProjectCount())
	}
}

func TestRunHistoryProviderPersisted(t *testing.T) {
	s := newTestState(t)

	start := time.Now().Add(-2 * time.Minute)
	record := RunRecord{
		ID:         "run-provider-test",
		StartTime:  start,
		EndTime:    start.Add(45 * time.Second),
		Provider:   "codex",
		Project:    "/tmp/project",
		Tasks:      []string{"docs-backfill"},
		TokensUsed: 50000,
		Status:     "success",
	}

	if err := s.AddRunRecord(record); err != nil {
		t.Fatalf("AddRunRecord() error = %v", err)
	}

	runs := s.GetRunHistory(1)
	if len(runs) != 1 {
		t.Fatalf("GetRunHistory() returned %d runs, want 1", len(runs))
	}
	if runs[0].Provider != "codex" {
		t.Fatalf("run provider = %q, want %q", runs[0].Provider, "codex")
	}
	if runs[0].TokensUsed != 50000 {
		t.Fatalf("run tokens = %d, want %d", runs[0].TokensUsed, 50000)
	}
}

func TestRunHistoryBranchPersisted(t *testing.T) {
	s := newTestState(t)

	start := time.Now().Add(-2 * time.Minute)
	record := RunRecord{
		ID:         "run-branch-test",
		StartTime:  start,
		EndTime:    start.Add(45 * time.Second),
		Provider:   "claude",
		Project:    "/tmp/project",
		Tasks:      []string{"lint-fix"},
		TokensUsed: 30000,
		Status:     "success",
		Branch:     "develop",
	}

	if err := s.AddRunRecord(record); err != nil {
		t.Fatalf("AddRunRecord() error = %v", err)
	}

	runs := s.GetRunHistory(1)
	if len(runs) != 1 {
		t.Fatalf("GetRunHistory() returned %d runs, want 1", len(runs))
	}
	if runs[0].Branch != "develop" {
		t.Fatalf("run branch = %q, want %q", runs[0].Branch, "develop")
	}
}

func TestRunHistoryBranchEmpty(t *testing.T) {
	s := newTestState(t)

	start := time.Now().Add(-2 * time.Minute)
	record := RunRecord{
		ID:         "run-no-branch-test",
		StartTime:  start,
		EndTime:    start.Add(30 * time.Second),
		Provider:   "claude",
		Project:    "/tmp/project",
		Tasks:      []string{"lint-fix"},
		TokensUsed: 20000,
		Status:     "success",
	}

	if err := s.AddRunRecord(record); err != nil {
		t.Fatalf("AddRunRecord() error = %v", err)
	}

	runs := s.GetRunHistory(1)
	if len(runs) != 1 {
		t.Fatalf("GetRunHistory() returned %d runs, want 1", len(runs))
	}
	if runs[0].Branch != "" {
		t.Fatalf("run branch = %q, want empty", runs[0].Branch)
	}
}

// TestRecordTaskRun_DBFailureReturnsError verifies that a DB failure returns an error.
func TestRecordTaskRun_DBFailureReturnsError(t *testing.T) {
	s, closeDB := newFailingState(t)
	defer closeDB()

	if err := s.RecordTaskRun("/project", "lint"); err == nil {
		t.Fatal("RecordTaskRun() expected error after DB closed, got nil")
	}
}

// TestRecordProjectRun_DBFailureReturnsError verifies that a DB failure returns an error.
func TestRecordProjectRun_DBFailureReturnsError(t *testing.T) {
	s, closeDB := newFailingState(t)
	defer closeDB()

	if err := s.RecordProjectRun("/project"); err == nil {
		t.Fatal("RecordProjectRun() expected error after DB closed, got nil")
	}
}

// TestAddRunRecord_DBFailureReturnsError verifies that a DB failure returns an error.
func TestAddRunRecord_DBFailureReturnsError(t *testing.T) {
	s, closeDB := newFailingState(t)
	defer closeDB()

	err := s.AddRunRecord(RunRecord{
		ID:      "test-run",
		Project: "/project",
		Status:  "success",
	})
	if err == nil {
		t.Fatal("AddRunRecord() expected error after DB closed, got nil")
	}
}

// TestMarkAssigned_DBFailureReturnsError verifies that a DB failure returns an error.
func TestMarkAssigned_DBFailureReturnsError(t *testing.T) {
	s, closeDB := newFailingState(t)
	defer closeDB()

	if err := s.MarkAssigned("task-x", "/project", "lint"); err == nil {
		t.Fatal("MarkAssigned() expected error after DB closed, got nil")
	}
}

// newFailingState creates a State backed by a DB that is immediately closed,
// causing all write operations to fail. Returns the state and a no-op closer
// (DB is already closed; the returned func is for deferred symmetry in tests).
func newFailingState(t *testing.T) (*State, func()) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	s, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Close the underlying DB so all subsequent SQL operations fail.
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	return s, func() {} // already closed; no-op for caller symmetry
}

func newTestState(t *testing.T) *State {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	dbPath := filepath.Join(home, "nightshift.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	s, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return s
}
