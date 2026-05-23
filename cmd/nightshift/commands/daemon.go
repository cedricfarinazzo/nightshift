package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/budget"
	"github.com/cedricfarinazzo/nightshift/internal/config"
	"github.com/cedricfarinazzo/nightshift/internal/db"
	"github.com/cedricfarinazzo/nightshift/internal/logging"
	"github.com/cedricfarinazzo/nightshift/internal/orchestrator"
	"github.com/cedricfarinazzo/nightshift/internal/reporting"
	"github.com/cedricfarinazzo/nightshift/internal/scheduler"
	"github.com/cedricfarinazzo/nightshift/internal/state"
	"github.com/cedricfarinazzo/nightshift/internal/tasks"
	"github.com/cedricfarinazzo/nightshift/internal/workspace"
	"github.com/spf13/cobra"
)

const (
	pidFileName = "nightshift.pid"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage background daemon",
	Long:  `Start, stop, or check status of the nightshift background daemon.`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start background daemon",
	Long: `Start the nightshift daemon as a background process.

The daemon runs the scheduler loop, executing tasks according to
the configured schedule (cron or interval) and respecting time windows.`,
	RunE: runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop background daemon",
	Long:  `Stop the running nightshift daemon by sending SIGTERM.`,
	RunE:  runDaemonStop,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check daemon status",
	Long:  `Check if the nightshift daemon is running and show status information.`,
	RunE:  runDaemonStatus,
}

var daemonForegroundFlag bool
var daemonTimeoutFlag time.Duration

func init() {
	daemonStartCmd.Flags().BoolVarP(&daemonForegroundFlag, "foreground", "f", false, "Run in foreground (don't daemonize)")
	daemonStartCmd.Flags().DurationVar(&daemonTimeoutFlag, "timeout", orchestrator.DefaultAgentTimeout, "Per-agent execution timeout")
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd)
}

// pidFilePath returns the path to the PID file.
func pidFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "nightshift", pidFileName)
}

// isDaemonRunning checks if the daemon is currently running.
func isDaemonRunning() (bool, int) {
	pid, _, err := readPidLock(pidFilePath())
	if err != nil {
		return false, 0
	}
	return isProcessRunning(pid), pid
}

func runDaemonStart(cmd *cobra.Command, args []string) error {
	// Check if already running
	if running, pid := isDaemonRunning(); running {
		return fmt.Errorf("daemon already running (pid %d)", pid)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Validate config for daemon mode (requires a schedule)
	if err := config.ValidateForDaemon(cfg); err != nil {
		if errors.Is(err, config.ErrNoSchedule) {
			return fmt.Errorf("no schedule configured: set schedule.cron or schedule.interval in config")
		}
		return fmt.Errorf("invalid config: %w", err)
	}

	if daemonForegroundFlag {
		// Run in foreground
		return runDaemonLoop(cfg)
	}

	// Daemonize: start a new process with --foreground flag
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable: %w", err)
	}

	daemonCmd := exec.Command(executable, "daemon", "start", "--foreground", "--timeout", daemonTimeoutFlag.String())
	daemonCmd.Stdout = nil
	daemonCmd.Stderr = nil
	daemonCmd.Stdin = nil
	// Detach from parent process group
	daemonCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}

	fmt.Printf("daemon started (pid %d)\n", daemonCmd.Process.Pid)
	return nil
}

func runDaemonLoop(cfg *config.Config) error {
	// Augment PATH so provider CLIs are discoverable when launched
	// from launchd/systemd/cron which have a minimal PATH.
	ensurePATH()

	// Initialize logging
	if err := initLogging(cfg); err != nil {
		return fmt.Errorf("init logging: %w", err)
	}
	log := logging.Component("daemon")

	// Write PID file
	pidLock, err := acquirePidLock(pidFilePath())
	if err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer func() { _ = pidLock.Release() }()

	log.Info("daemon starting")

	database, err := db.Open(cfg.ExpandedDBPath())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = database.Close() }()

	// Set up context with signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		sig := <-sigCh
		log.Infof("received signal %v, shutting down", sig)
		cancel()
	}()

	// Initialize scheduler from config
	sched, err := scheduler.NewFromConfig(&cfg.Schedule)
	if err != nil {
		return fmt.Errorf("init scheduler: %w", err)
	}

	// Add the main run job
	sched.AddJob(func(jobCtx context.Context) error {
		return runScheduledTasks(jobCtx, cfg, database, log)
	})

	// Start scheduler
	if err := sched.Start(ctx); err != nil {
		return fmt.Errorf("start scheduler: %w", err)
	}

	// Clean stale workspaces on daemon start if workspace mode is configured.
	if cfg.Workspace.Root != "" {
		go func() {
			n, err := workspace.CleanupStaleWorkspaces(workspaceConfigFromApp(cfg))
			if err != nil {
				log.Warnf("workspace cleanup: %v", err)
				return
			}
			if n > 0 {
				log.Infof("removed %d stale workspace(s)", n)
			}
		}()
	}

	log.InfoCtx("daemon running", map[string]any{
		"next_run": sched.NextRun().Format(time.RFC3339),
	})

	// Wait for context cancellation
	<-ctx.Done()

	// Stop scheduler gracefully
	if err := sched.Stop(); err != nil && err != scheduler.ErrNotRunning {
		log.Errorf("stopping scheduler: %v", err)
	}

	log.Info("daemon stopped")
	return nil
}

// runScheduledTasks executes the scheduled nightshift tasks.
func runScheduledTasks(ctx context.Context, cfg *config.Config, database *db.DB, log *logging.Logger) error {
	log.Info("scheduled run starting")
	start := time.Now()

	// Initialize state manager
	st, err := state.New(database)
	if err != nil {
		log.Errorf("init state: %v", err)
		return err
	}

	// Clear stale assignments older than 2 hours
	cleared := st.ClearStaleAssignments(2 * time.Hour)
	if cleared > 0 {
		log.Infof("cleared %d stale assignments", cleared)
	}

	budgetMgr := budget.NewManagerWithTracking(cfg)

	report := newRunReport(time.Now())

	// Workspace mode: clone repos and run tasks in isolation.
	if cfg.Workspace.Root != "" && len(cfg.Workspace.Repos) > 0 {
		return runScheduledWorkspacedTasks(ctx, cfg, database, log, st, budgetMgr, report)
	}

	// Resolve projects
	projects, err := resolveProjects(cfg, "")
	if err != nil {
		log.Errorf("resolve projects: %v", err)
		return err
	}

	if len(projects) == 0 {
		log.Info("no projects configured")
		return nil
	}

	// Create task selector
	selector := tasks.NewSelector(cfg, st)

	var tasksRun, tasksCompleted, tasksFailed int

	// Process each project
	for _, projectPath := range projects {
		select {
		case <-ctx.Done():
			log.Info("run cancelled")
			return ctx.Err()
		default:
		}

		// Skip if already processed today
		if st.WasProcessedToday(projectPath) {
			log.Debugf("skip %s (processed today)", projectPath)
			continue
		}

		// Select the best available provider with remaining budget
		choice, err := selectProvider(cfg, budgetMgr, log, false)
		if err != nil {
			log.Infof("no provider available: %v", err)
			break
		}

		// Detect the current branch before any tasks run so it can be
		// injected into RunMetadata for prompt branch instructions.
		baseBranch, _ := orchestrator.CurrentBranch(ctx, projectPath)

		orch := orchestrator.New(
			orchestrator.WithAgent(choice.agent),
			orchestrator.WithConfig(orchestrator.Config{
				MaxIterations: 3,
				AgentTimeout:  daemonTimeoutFlag,
				Compression:   compressionConfigFromApp(cfg),
			}),
			orchestrator.WithLogger(logging.Component("orchestrator")),
		)

		// Select tasks — respect schedule.max_tasks from config (default 5).
		maxTasks := cfg.Schedule.MaxTasks
		if maxTasks <= 0 {
			maxTasks = 5
		}
		selectedTasks := selector.SelectTopN(projectPath, maxTasks)
		if len(selectedTasks) == 0 {
			if report != nil {
				report.addTask(reporting.TaskResult{
					Project:    projectPath,
					TaskType:   "",
					Title:      "No tasks selected",
					Status:     "skipped",
					SkipReason: "no tasks available",
				})
			}
			continue
		}

		log.InfoCtx("processing project", map[string]any{
			"project":  projectPath,
			"tasks":    len(selectedTasks),
			"provider": choice.name,
		})

		// Execute each selected task
		projectStart := time.Now()
		projectTaskTypes := make([]string, 0, len(selectedTasks))
		projectTokensUsed := 0
		projectCompleted := 0
		projectFailed := 0
		for _, scoredTask := range selectedTasks {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			tasksRun++
			projectTaskTypes = append(projectTaskTypes, string(scoredTask.Definition.Type))

			// Create task instance
			taskInstance := &tasks.Task{
				ID:          fmt.Sprintf("%s:%s", scoredTask.Definition.Type, projectPath),
				Title:       scoredTask.Definition.Name,
				Description: scoredTask.Definition.Description,
				Priority:    int(scoredTask.Score),
				Type:        scoredTask.Definition.Type,
			}

			// Mark as assigned
			st.MarkAssigned(taskInstance.ID, projectPath, string(scoredTask.Definition.Type))

			orch.SetRunMetadata(&orchestrator.RunMetadata{
				Provider:  choice.name,
				TaskType:  string(scoredTask.Definition.Type),
				TaskScore: scoredTask.Score,
				CostTier:  scoredTask.Definition.CostTier.String(),
				RunStart:  projectStart,
				Branch:    baseBranch,
			})

			// Execute via orchestrator
			result, err := orch.RunTask(ctx, taskInstance, projectPath)

			// Clear assignment
			st.ClearAssigned(taskInstance.ID)

			if err != nil {
				tasksFailed++
				projectFailed++
				log.Errorf("task %s failed: %v", taskInstance.ID, err)
				if report != nil {
					report.addTask(reporting.TaskResult{
						Project:    projectPath,
						TaskType:   string(scoredTask.Definition.Type),
						Title:      scoredTask.Definition.Name,
						Status:     "failed",
						TokensUsed: 0,
						Duration:   result.Duration,
					})
				}
				continue
			}

			// Record result
			switch result.Status {
			case orchestrator.StatusCompleted:
				tasksCompleted++
				projectCompleted++
				st.RecordTaskRun(projectPath, string(scoredTask.Definition.Type))
				log.InfoCtx("task completed", map[string]any{
					"task":       taskInstance.ID,
					"iterations": result.Iterations,
					"duration":   result.Duration.String(),
				})
				_, maxTok := scoredTask.Definition.EstimatedTokens()
				projectTokensUsed += maxTok
				if report != nil {
					report.addTask(reporting.TaskResult{
						Project:    projectPath,
						TaskType:   string(scoredTask.Definition.Type),
						Title:      scoredTask.Definition.Name,
						Status:     "completed",
						OutputType: result.OutputType,
						OutputRef:  result.OutputRef,
						TokensUsed: maxTok,
						Duration:   result.Duration,
					})
				}
			case orchestrator.StatusAbandoned:
				tasksFailed++
				projectFailed++
				log.Warnf("task %s abandoned: %s", taskInstance.ID, result.Error)
				if report != nil {
					report.addTask(reporting.TaskResult{
						Project:    projectPath,
						TaskType:   string(scoredTask.Definition.Type),
						Title:      scoredTask.Definition.Name,
						Status:     "failed",
						SkipReason: result.Error,
						Duration:   result.Duration,
					})
				}
			default:
				tasksFailed++
				projectFailed++
				log.Errorf("task %s failed: %s", taskInstance.ID, result.Error)
				if report != nil {
					report.addTask(reporting.TaskResult{
						Project:    projectPath,
						TaskType:   string(scoredTask.Definition.Type),
						Title:      scoredTask.Definition.Name,
						Status:     "failed",
						SkipReason: result.Error,
						Duration:   result.Duration,
					})
				}
			}
		}

		// Record project run
		st.RecordProjectRun(projectPath)
		projectStatus := "partial"
		if projectFailed == 0 && projectCompleted > 0 {
			projectStatus = "success"
		}
		if projectCompleted == 0 && projectFailed > 0 {
			projectStatus = "failed"
		}
		st.AddRunRecord(state.RunRecord{
			StartTime:  projectStart,
			EndTime:    time.Now(),
			Provider:   choice.name,
			Project:    projectPath,
			Tasks:      projectTaskTypes,
			TokensUsed: projectTokensUsed,
			Status:     projectStatus,
		})
	}

	// Summary
	duration := time.Since(start)
	log.InfoCtx("scheduled run complete", map[string]any{
		"duration":  duration.String(),
		"tasks_run": tasksRun,
		"completed": tasksCompleted,
		"failed":    tasksFailed,
		"projects":  len(projects),
	})

	if report != nil {
		report.finalize(cfg, log)
	}

	return nil
}

// runScheduledWorkspacedTasks is the daemon counterpart of workspacedRun.
// It clones each configured repo into a fresh workspace and runs tasks in isolation,
// using the repo Name as the state key for cooldown tracking.
func runScheduledWorkspacedTasks(
	ctx context.Context,
	cfg *config.Config,
	_ *db.DB,
	log *logging.Logger,
	st *state.State,
	budgetMgr *budget.Manager,
	report *runReport,
) error {
	start := time.Now()
	log.Info("workspace scheduled run starting")

	runID := newRunID()

	// Clone-timeout: give 5 min for workspace setup, same as workspacedRun.
	cloneCtx, cloneCancel := context.WithTimeout(ctx, 5*time.Minute)
	ws, err := workspace.SetupWorkspace(cloneCtx, workspaceConfigFromApp(cfg), runID)
	cloneCancel()
	if err != nil {
		log.Errorf("setup workspace: %v", err)
		return fmt.Errorf("setup workspace: %w", err)
	}

	choice, err := selectProvider(cfg, budgetMgr, log, false)
	if err != nil {
		log.Infof("no provider available: %v", err)
		return nil
	}

	orch := orchestrator.New(
		orchestrator.WithAgent(choice.agent),
		orchestrator.WithConfig(orchestrator.Config{
			MaxIterations: 3,
			AgentTimeout:  daemonTimeoutFlag,
			Compression:   compressionConfigFromApp(cfg),
		}),
		orchestrator.WithLogger(logging.Component("orchestrator")),
	)

	maxTasks := cfg.Schedule.MaxTasks
	if maxTasks <= 0 {
		maxTasks = 5
	}

	selector := tasks.NewSelector(cfg, st)

	var tasksRun, tasksCompleted, tasksFailed int
	projectStart := time.Now()

	for _, rw := range ws.Repos {
		select {
		case <-ctx.Done():
			log.Info("workspace run cancelled")
			return ctx.Err()
		default:
		}

		// Use repo name as state key so cooldowns are stable across run IDs.
		if st.WasProcessedToday(rw.Name) {
			log.Debugf("skip workspace repo %s (processed today)", rw.Name)
			continue
		}

		baseBranch, _ := orchestrator.CurrentBranch(ctx, rw.Path)

		log.InfoCtx("processing workspace repo", map[string]any{
			"repo":     rw.Name,
			"path":     rw.Path,
			"provider": choice.name,
		})

		run, ok, fail := runRepoTasks(ctx, repoTasksParams{
			cfg:          cfg,
			st:           st,
			selector:     selector,
			orch:         orch,
			choice:       choice,
			repoPath:     rw.Path,
			stateKey:     rw.Name,
			allowedTasks: rw.Tasks,
			maxTasks:     maxTasks,
			branch:       baseBranch,
			report:       report,
			log:          log,
			projectStart: projectStart,
			verbose:      false,
		})
		tasksRun += run
		tasksCompleted += ok
		tasksFailed += fail
	}

	duration := time.Since(start)
	log.InfoCtx("workspace scheduled run complete", map[string]any{
		"duration":  duration.String(),
		"tasks_run": tasksRun,
		"completed": tasksCompleted,
		"failed":    tasksFailed,
		"run_id":    runID,
	})

	if report != nil {
		report.finalize(cfg, log)
	}

	return nil
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	running, pid := isDaemonRunning()
	if !running {
		// Check if PID file exists but process is dead.
		if _, _, err := readPidLock(pidFilePath()); err == nil {
			_ = os.Remove(pidFilePath())
			fmt.Println("daemon not running (stale pid file removed)")
			return nil
		}
		fmt.Println("daemon not running")
		return nil
	}

	// Send SIGTERM to the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending SIGTERM: %w", err)
	}

	fmt.Printf("stopping daemon (pid %d)...\n", pid)

	// Wait for process to exit (with timeout)
	timeout := time.After(10 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-timeout:
			// Force kill if still running
			fmt.Println("daemon did not stop, sending SIGKILL")
			_ = process.Signal(syscall.SIGKILL)
			_ = os.Remove(pidFilePath())
			return nil
		case <-tick.C:
			if !isProcessRunning(pid) {
				fmt.Println("daemon stopped")
				_ = os.Remove(pidFilePath())
				return nil
			}
		}
	}
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	running, pid := isDaemonRunning()

	if !running {
		fmt.Println("Status: not running")
		return nil
	}

	fmt.Printf("Status: running\n")
	fmt.Printf("PID: %d\n", pid)

	// Try to load config and show next run time
	cfg, err := config.Load()
	if err == nil && (cfg.Schedule.Cron != "" || cfg.Schedule.Interval != "") {
		sched, err := scheduler.NewFromConfig(&cfg.Schedule)
		if err == nil {
			// We need to start the scheduler briefly to calculate next run
			// Instead, just show the schedule config
			if cfg.Schedule.Cron != "" {
				fmt.Printf("Schedule: cron %s\n", cfg.Schedule.Cron)
			} else if cfg.Schedule.Interval != "" {
				fmt.Printf("Schedule: every %s\n", cfg.Schedule.Interval)
			}
			if cfg.Schedule.Window != nil {
				fmt.Printf("Window: %s - %s", cfg.Schedule.Window.Start, cfg.Schedule.Window.End)
				if cfg.Schedule.Window.Timezone != "" {
					fmt.Printf(" (%s)", cfg.Schedule.Window.Timezone)
				}
				fmt.Println()
			}
			_ = sched // satisfy compiler
		}
	}

	// Show PID file path for reference
	fmt.Printf("PID file: %s\n", pidFilePath())

	return nil
}
