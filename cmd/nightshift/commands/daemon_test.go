package commands

import (
	"context"
	"testing"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/budget"
	"github.com/cedricfarinazzo/nightshift/internal/config"
	"github.com/cedricfarinazzo/nightshift/internal/db"
	"github.com/cedricfarinazzo/nightshift/internal/logging"
	"github.com/cedricfarinazzo/nightshift/internal/orchestrator"
	"github.com/cedricfarinazzo/nightshift/internal/state"
	"github.com/cedricfarinazzo/nightshift/internal/tasks"
	"github.com/cedricfarinazzo/nightshift/internal/workspace"
)

// setupDaemonDB creates a temp DB and state for daemon tests.
func setupDaemonDB(t *testing.T) (*db.DB, *state.State) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	database, err := db.Open(home + "/nightshift.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	st, err := state.New(database)
	if err != nil {
		t.Fatalf("init state: %v", err)
	}
	return database, st
}

// noopGitExec is a workspace.gitExecFn replacement that always succeeds.
func noopGitExec(_ context.Context, _ string, _ ...string) (string, error) {
	return "", nil
}

// TestRunScheduledTasks_WorkspaceMode verifies that when workspace config is set,
// runScheduledTasks routes to the workspace code path (clones repos via gitExecFn)
// instead of the project-path-based path.
func TestRunScheduledTasks_WorkspaceMode(t *testing.T) {
	tmp := t.TempDir()
	database, st := setupDaemonDB(t)

	// Pre-mark the repo as processed today by name.
	// This causes the workspace loop to skip it — no agent execution needed.
	st.RecordProjectRun("test-repo")

	gitCalled := false
	restore := workspace.SetGitExecFn(func(ctx context.Context, dir string, args ...string) (string, error) {
		gitCalled = true
		return noopGitExec(ctx, dir, args...)
	})
	defer workspace.SetGitExecFn(restore)

	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{
			Root: tmp,
			Repos: []config.WorkspaceRepoConfig{
				{URL: "git@github.com:org/repo.git", Name: "test-repo"},
			},
		},
		Providers: config.ProvidersConfig{
			Claude: config.ProviderConfig{Enabled: true},
		},
		Budget:   config.BudgetConfig{MaxPercent: 75},
		Schedule: config.ScheduleConfig{MaxTasks: 1},
	}

	log := logging.Component("test")
	_ = runScheduledTasks(context.Background(), cfg, database, log)

	if !gitCalled {
		t.Fatal("workspace mode not entered: gitExecFn was never called")
	}
}

// TestRunScheduledTasks_NonWorkspaceMode verifies that without workspace config,
// runScheduledTasks does NOT call gitExecFn (no workspace cloning).
func TestRunScheduledTasks_NonWorkspaceMode(t *testing.T) {
	database, _ := setupDaemonDB(t)

	gitCalled := false
	restore := workspace.SetGitExecFn(func(ctx context.Context, dir string, args ...string) (string, error) {
		gitCalled = true
		return noopGitExec(ctx, dir, args...)
	})
	defer workspace.SetGitExecFn(restore)

	cfg := &config.Config{
		Providers: config.ProvidersConfig{},
		Budget:    config.BudgetConfig{MaxPercent: 75},
		Schedule:  config.ScheduleConfig{MaxTasks: 1},
	}

	log := logging.Component("test")
	_ = runScheduledTasks(context.Background(), cfg, database, log)

	if gitCalled {
		t.Fatal("non-workspace mode erroneously called gitExecFn")
	}
}

// TestRunScheduledWorkspacedTasks_StateKey verifies that WasProcessedToday uses
// rw.Name (not rw.Path) as the state key. We pre-record rw.Name in state; if the
// function correctly uses rw.Name, the repo is skipped with no agent execution.
// If it mistakenly used rw.Path, WasProcessedToday would return false and task
// execution would be attempted (resulting in an error with no real agent in PATH).
func TestRunScheduledWorkspacedTasks_StateKey(t *testing.T) {
	tmp := t.TempDir()
	database, st := setupDaemonDB(t)

	const repoName = "my-repo"
	// Pre-mark the repo by name, not by any filesystem path.
	st.RecordProjectRun(repoName)

	restore := workspace.SetGitExecFn(noopGitExec)
	defer workspace.SetGitExecFn(restore)

	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{
			Root: tmp,
			Repos: []config.WorkspaceRepoConfig{
				{URL: "git@github.com:org/repo.git", Name: repoName},
			},
		},
		Providers: config.ProvidersConfig{
			Claude: config.ProviderConfig{Enabled: true},
		},
		Budget:   config.BudgetConfig{MaxPercent: 75},
		Schedule: config.ScheduleConfig{MaxTasks: 1},
	}

	budgetMgr := budget.NewManagerWithTracking(cfg)
	report := newRunReport(time.Now())
	log := logging.Component("test")

	err := runScheduledWorkspacedTasks(context.Background(), cfg, database, log, st, budgetMgr, report)
	if err != nil {
		t.Fatalf("runScheduledWorkspacedTasks returned unexpected error: %v", err)
	}
	// Success: the repo was skipped because WasProcessedToday(repoName) == true.
	// If state key were rw.Path (a UUID-suffixed temp path), WasProcessedToday
	// would return false and task execution would be attempted without a provider.
}

// TestRunRepoTasks_PerRepoTaskFilter verifies that allowedTasks filters out task
// types not in the list. When the per-repo filter excludes all selected tasks,
// runRepoTasks returns 0 tasks run without invoking the orchestrator.
func TestRunRepoTasks_PerRepoTaskFilter(t *testing.T) {
	st := newTestRunState(t)
	cfg := newTestRunConfig()
	selector := tasks.NewSelector(cfg, st)

	// Build a minimal orchestrator — RunTask will not be called because the
	// per-repo filter will eliminate all selected tasks.
	orch := orchestrator.New(
		orchestrator.WithLogger(logging.Component("test")),
	)

	// allowedTasks contains a task type that is unlikely to be in the
	// default task catalog; the filter will exclude all real tasks.
	run, ok, fail := runRepoTasks(context.Background(), repoTasksParams{
		cfg:          cfg,
		st:           st,
		selector:     selector,
		orch:         orch,
		choice:       &providerChoice{name: "claude"},
		repoPath:     t.TempDir(),
		stateKey:     "filter-test-repo",
		allowedTasks: []string{"__nonexistent_task_type__"},
		maxTasks:     5,
		log:          logging.Component("test"),
		projectStart: time.Now(),
	})

	if run != 0 || ok != 0 || fail != 0 {
		t.Fatalf("expected (0,0,0) with per-repo filter, got (%d,%d,%d)", run, ok, fail)
	}
}
