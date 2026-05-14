package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/marcus/nightshift/internal/budget"
	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/db"
	"github.com/marcus/nightshift/internal/scheduler"
	"github.com/marcus/nightshift/internal/state"
)

type checkStatus string

const (
	statusOK   checkStatus = "OK"
	statusWarn checkStatus = "WARN"
	statusFail checkStatus = "FAIL"
)

type checkResult struct {
	name   string
	status checkStatus
	detail string
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check Nightshift configuration and environment",
	Long: `Run diagnostics to detect configuration and environment issues.

Checks config, scheduling, providers, database health, and budget readiness.`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	// Augment PATH the same way 'run' does so CLI checks are accurate.
	ensurePATH()

	results := make([]checkResult, 0)
	hasFail := false

	add := func(name string, status checkStatus, detail string) {
		if status == statusFail {
			hasFail = true
		}
		results = append(results, checkResult{name: name, status: status, detail: detail})
	}

	cfg, err := config.Load()
	if err != nil {
		add("config", statusFail, err.Error())
		printDoctorResults(results)
		return fmt.Errorf("config load failed")
	}
	add("config", statusOK, "loaded")

	database, err := db.Open(cfg.ExpandedDBPath())
	if err != nil {
		add("db", statusFail, err.Error())
		printDoctorResults(results)
		return fmt.Errorf("db open failed")
	}
	defer func() { _ = database.Close() }()
	add("db", statusOK, cfg.ExpandedDBPath())

	if _, err := state.New(database); err != nil {
		add("state", statusFail, err.Error())
	} else {
		add("state", statusOK, "ready")
	}

	checkSchedule(cfg, add)
	checkService(add)
	checkDaemon(add)

	checkCLIs(cfg, add)
	checkProviders(cfg, add)
	checkBudget(cfg, database, add)

	printDoctorResults(results)

	if hasFail {
		return fmt.Errorf("doctor found failures")
	}
	return nil
}

func checkSchedule(cfg *config.Config, add func(string, checkStatus, string)) {
	sched, err := scheduler.NewFromConfig(&cfg.Schedule)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoSchedule) {
			add("schedule", statusWarn, "no schedule configured (cron or interval)")
			return
		}
		add("schedule", statusFail, err.Error())
		return
	}
	nextRuns, err := sched.NextRuns(1)
	if err != nil || len(nextRuns) == 0 {
		add("schedule", statusWarn, "unable to compute next run")
		return
	}
	add("schedule", statusOK, fmt.Sprintf("next run %s", nextRuns[0].Format("2006-01-02 15:04")))
}

func checkService(add func(string, checkStatus, string)) {
	service := detectServiceType()
	switch service {
	case ServiceLaunchd:
		home, _ := os.UserHomeDir()
		plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdPlistName)
		if _, err := os.Stat(plistPath); err == nil {
			add("service", statusOK, fmt.Sprintf("launchd installed (%s)", plistPath))
			return
		}
		add("service", statusWarn, "launchd service not installed")
	case ServiceSystemd:
		home, _ := os.UserHomeDir()
		servicePath := filepath.Join(home, ".config", "systemd", "user", systemdServiceName)
		timerPath := filepath.Join(home, ".config", "systemd", "user", systemdTimerName)
		if _, err := os.Stat(servicePath); err == nil {
			add("service", statusOK, fmt.Sprintf("systemd service present (%s)", servicePath))
		} else {
			add("service", statusWarn, "systemd service not installed")
			return
		}
		if _, err := os.Stat(timerPath); err == nil {
			add("service.timer", statusOK, fmt.Sprintf("systemd timer present (%s)", timerPath))
		} else {
			add("service.timer", statusWarn, "systemd timer missing")
		}

		// Check Jira-specific service unit.
		jiraServicePath := filepath.Join(home, ".config", "systemd", "user", systemdJiraServiceName)
		if _, err := os.Stat(jiraServicePath); err == nil {
			out, err := exec.Command("systemctl", "--user", "is-active", systemdJiraServiceName).CombinedOutput()
			status := strings.TrimSpace(string(out))
			if err != nil {
				// systemctl failed (e.g., no user bus) — include error in output
				add("service.jira", statusWarn, fmt.Sprintf("nightshift-jira.service: %s (systemctl error: %v)", status, err))
			} else {
				switch status {
				case "active":
					add("service.jira", statusOK, "nightshift-jira.service active")
				case "inactive":
					add("service.jira", statusWarn, "nightshift-jira.service installed but inactive")
				default:
					add("service.jira", statusWarn, fmt.Sprintf("nightshift-jira.service: %s", status))
				}
			}
			jiraTimerPath := filepath.Join(home, ".config", "systemd", "user", systemdJiraTimerName)
			if _, err := os.Stat(jiraTimerPath); err == nil {
				out, err := exec.Command("systemctl", "--user", "is-active", systemdJiraTimerName).CombinedOutput()
				timerStatus := strings.TrimSpace(string(out))
				if err != nil {
					add("service.jira.timer", statusWarn, fmt.Sprintf("nightshift-jira.timer: %s (systemctl error: %v)", timerStatus, err))
				} else if timerStatus == "active" {
					add("service.jira.timer", statusOK, "nightshift-jira.timer active")
				} else {
					add("service.jira.timer", statusWarn, fmt.Sprintf("nightshift-jira.timer: %s", timerStatus))
				}
			}
		} else {
			add("service.jira", statusWarn, "nightshift-jira.service not installed")
		}
	case ServiceCron:
		out, err := exec.Command("crontab", "-l").CombinedOutput()
		if err != nil {
			add("service", statusWarn, "cron not accessible")
			return
		}
		if strings.Contains(string(out), cronMarker) {
			add("service", statusOK, "cron entry installed")
		} else {
			add("service", statusWarn, "cron entry not installed")
		}
	default:
		add("service", statusWarn, fmt.Sprintf("unknown service type (%s)", runtime.GOOS))
	}
}

func checkDaemon(add func(string, checkStatus, string)) {
	pid, err := readPidFile()
	if err != nil {
		add("daemon", statusWarn, "not running (pid file missing)")
		return
	}
	if isProcessRunning(pid) {
		add("daemon", statusOK, fmt.Sprintf("running (pid %d)", pid))
	} else {
		add("daemon", statusWarn, "pid file present but process not running")
	}
}

func checkCLIs(cfg *config.Config, add func(string, checkStatus, string)) {
	if cfg.Providers.Claude.Enabled {
		if path, err := exec.LookPath("claude"); err != nil {
			add("claude.cli", statusFail, "claude not found in PATH")
		} else {
			add("claude.cli", statusOK, path)
		}
	}
	if cfg.Providers.Codex.Enabled {
		if path, err := exec.LookPath("codex"); err != nil {
			add("codex.cli", statusFail, "codex not found in PATH")
		} else {
			add("codex.cli", statusOK, path)
		}
	}
}

func checkProviders(cfg *config.Config, add func(string, checkStatus, string)) {
	if cfg.Providers.Claude.Enabled {
		add("claude.provider", statusOK, "enabled (active tracking via API)")
	}
	if cfg.Providers.Codex.Enabled {
		add("codex.provider", statusOK, "enabled (active tracking via API)")
	}
	if cfg.Providers.Copilot.Enabled {
		add("copilot.provider", statusOK, "enabled (active tracking via API)")
	}
}

func checkBudget(cfg *config.Config, _ *db.DB, add func(string, checkStatus, string)) {
	budgetMgr := budget.NewManagerWithTracking(cfg)

	if cfg.Providers.Claude.Enabled {
		if usedPct, err := budgetMgr.GetUsedPercent("claude"); err != nil {
			add("budget.claude", statusFail, err.Error())
		} else {
			add("budget.claude", statusOK, fmt.Sprintf("%.1f%% used", usedPct))
		}
	}

	if cfg.Providers.Codex.Enabled {
		if usedPct, err := budgetMgr.GetUsedPercent("codex"); err != nil {
			add("budget.codex", statusFail, err.Error())
		} else {
			add("budget.codex", statusOK, fmt.Sprintf("%.1f%% used", usedPct))
		}
	}
}


func printDoctorResults(results []checkResult) {
	fmt.Println("Nightshift doctor")
	fmt.Println("=================")
	for _, result := range results {
		fmt.Printf("[%s] %-20s %s\n", result.status, result.name, result.detail)
	}
	fmt.Println()
}
