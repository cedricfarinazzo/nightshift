package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cedricfarinazzo/nightshift/internal/config"
	"github.com/spf13/cobra"
)

// Service type constants
const (
	ServiceLaunchd = "launchd"
	ServiceSystemd = "systemd"
	ServiceCron    = "cron"
)

// File paths for installed services
const (
	launchdPlistName       = "com.nightshift.agent.plist"
	systemdServiceName     = "nightshift.service"
	systemdTimerName       = "nightshift.timer"
	systemdJiraServiceName = "nightshift-jira.service"
	systemdJiraTimerName   = "nightshift-jira.timer"
	cronMarker             = "# nightshift managed cron entry"
)

var installCmd = &cobra.Command{
	Use:   "install [launchd|systemd|cron]",
	Short: "Install system service",
	Long: `Generate and install a system service for nightshift.

Supported init systems:
  launchd  - macOS (creates ~/Library/LaunchAgents plist)
  systemd  - Linux (creates user systemd unit)
  cron     - Universal (creates crontab entry)

If no init system is specified, auto-detects based on OS.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInstall,
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove system service",
	Long:  `Remove the nightshift system service.`,
	RunE:  runUninstall,
}

var installSystemdJiraFlag bool

func init() {
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	installCmd.Flags().BoolVar(&installSystemdJiraFlag, "systemd-jira", false, "Install systemd user service for nightshift jira run")
}

// runInstall implements the install command
func runInstall(cmd *cobra.Command, args []string) error {
	// Handle --systemd-jira flag: install Jira-specific systemd unit
	if installSystemdJiraFlag {
		cfg, err := config.Load()
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("loading config: %w", err)
		}
		if cfg == nil {
			cfg = &config.Config{}
		}
		return installSystemdJira(cfg)
	}

	// Determine service type
	serviceType := ""
	if len(args) > 0 {
		serviceType = args[0]
	} else {
		serviceType = detectServiceType()
	}

	// Validate service type
	switch serviceType {
	case ServiceLaunchd, ServiceSystemd, ServiceCron:
		// Valid
	default:
		return fmt.Errorf("unsupported service type: %s (use launchd, systemd, or cron)", serviceType)
	}

	// Load config to get schedule
	cfg, err := config.Load()
	if err != nil {
		// Use defaults if no config
		cfg = &config.Config{
			Schedule: config.ScheduleConfig{
				Cron: "0 2 * * *", // Default: 2 AM daily
			},
		}
	}

	// Get nightshift binary path
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating nightshift binary: %w", err)
	}
	binaryPath, err = filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	// Install based on service type
	switch serviceType {
	case ServiceLaunchd:
		return installLaunchd(binaryPath, cfg)
	case ServiceSystemd:
		return installSystemd(binaryPath, cfg)
	case ServiceCron:
		return installCron(binaryPath, cfg)
	}

	return nil
}

// detectServiceType auto-detects the appropriate service type for the current OS
func detectServiceType() string {
	switch runtime.GOOS {
	case "darwin":
		return ServiceLaunchd
	case "linux":
		// Check if systemd is available
		if _, err := exec.LookPath("systemctl"); err == nil {
			return ServiceSystemd
		}
		return ServiceCron
	default:
		return ServiceCron
	}
}

// installLaunchd creates and loads a launchd plist for macOS
func installLaunchd(binaryPath string, cfg *config.Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	// Ensure LaunchAgents directory exists
	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents directory: %w", err)
	}

	plistPath := filepath.Join(launchAgentsDir, launchdPlistName)

	// Generate plist content
	plist := generateLaunchdPlist(binaryPath, cfg)

	// Unload existing service if present
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	// Write plist file
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	// Load the service
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("loading launchd service: %w", err)
	}

	fmt.Printf("Installed launchd service: %s\n", plistPath)
	fmt.Println("Service loaded and will run according to schedule")
	return nil
}

// generateLaunchdPlist creates the plist content for launchd
func generateLaunchdPlist(binaryPath string, cfg *config.Config) string {
	// Parse schedule for launchd calendar interval
	hour, minute := parseScheduleTime(cfg)

	// Capture the current PATH so launchd jobs can find provider CLIs.
	pathValue := os.Getenv("PATH")

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.nightshift.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>%s</string>
    </dict>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>%d</integer>
        <key>Minute</key>
        <integer>%d</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>RunAtLoad</key>
    <false/>
</dict>
</plist>
`, binaryPath, pathValue, hour, minute, getLogPath("stdout"), getLogPath("stderr"))
}

// installSystemd creates and enables systemd user service and timer
func installSystemd(binaryPath string, cfg *config.Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	// Ensure systemd user directory exists
	systemdDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		return fmt.Errorf("creating systemd directory: %w", err)
	}

	servicePath := filepath.Join(systemdDir, systemdServiceName)
	timerPath := filepath.Join(systemdDir, systemdTimerName)

	// Generate service and timer content
	service := generateSystemdService(binaryPath)
	timer := generateSystemdTimer(cfg)

	// Stop and disable existing service if present
	_ = exec.Command("systemctl", "--user", "stop", "nightshift.timer").Run()
	_ = exec.Command("systemctl", "--user", "disable", "nightshift.timer").Run()

	// Write service file
	if err := os.WriteFile(servicePath, []byte(service), 0644); err != nil {
		return fmt.Errorf("writing service file: %w", err)
	}

	// Write timer file
	if err := os.WriteFile(timerPath, []byte(timer), 0644); err != nil {
		return fmt.Errorf("writing timer file: %w", err)
	}

	// Best-effort: reload, enable, start. Failures don't block — files are already written.
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	_ = exec.Command("systemctl", "--user", "enable", "nightshift.timer").Run()
	_ = exec.Command("systemctl", "--user", "start", "nightshift.timer").Run()

	fmt.Printf("Installed systemd service: %s\n", servicePath)
	fmt.Printf("Installed systemd timer: %s\n", timerPath)
	fmt.Println("Timer enabled and started (or run: systemctl --user enable --now nightshift.timer)")
	return nil
}

// generateSystemdService creates the systemd service unit content.
// Uses daemon start --foreground so the internal interval+window loop handles cycles.
func generateSystemdService(binaryPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Nightshift AI-powered code maintenance
Documentation=https://github.com/cedricfarinazzo/nightshift

[Service]
Type=simple
ExecStart=%s daemon start --foreground
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`, binaryPath)
}

// generateSystemdTimer creates the systemd timer unit content
func generateSystemdTimer(cfg *config.Config) string {
	// Convert cron to systemd OnCalendar format or use interval
	onCalendar := convertCronToSystemd(cfg)

	return fmt.Sprintf(`[Unit]
Description=Nightshift scheduled runs

[Timer]
OnCalendar=%s
Persistent=true

[Install]
WantedBy=timers.target
`, onCalendar)
}

// convertCronToSystemd converts a cron expression to systemd OnCalendar format.
// When interval mode is used, the timer fires once at the window start time (if set)
// or at 02:00 — nightshift's internal cycle loop handles the interval repetition.
func convertCronToSystemd(cfg *config.Config) string {
	if cfg.Schedule.Interval != "" {
		// Use window start if configured, else default to 02:00.
		if cfg.Schedule.Window != nil && cfg.Schedule.Window.Start != "" {
			start := cfg.Schedule.Window.Start // e.g. "09:00"
			parts := strings.SplitN(start, ":", 2)
			if len(parts) == 2 {
				return fmt.Sprintf("*-*-* %s:%s:00", parts[0], parts[1])
			}
		}
		return "*-*-* 02:00:00"
	}

	if cfg.Schedule.Cron == "" {
		// Default: 2 AM daily
		return "*-*-* 02:00:00"
	}

	// Parse simple cron format: minute hour day month dow
	parts := strings.Fields(cfg.Schedule.Cron)
	if len(parts) != 5 {
		return "*-*-* 02:00:00" // Default fallback
	}

	minute := parts[0]
	hour := parts[1]
	dayOfMonth := parts[2]
	month := parts[3]
	// dayOfWeek := parts[4] // systemd handles this differently

	// Build OnCalendar string
	// Format: DayOfWeek Year-Month-Day Hour:Minute:Second
	if dayOfMonth == "*" {
		dayOfMonth = "*"
	}
	if month == "*" {
		month = "*"
	}

	return fmt.Sprintf("*-%s-%s %s:%s:00", month, dayOfMonth, hour, minute)
}

// installCron adds a crontab entry for nightshift
func installCron(binaryPath string, cfg *config.Config) error {
	// Get current crontab
	out, _ := exec.Command("crontab", "-l").Output()
	currentCrontab := string(out)

	// Remove existing nightshift entry if present
	lines := strings.Split(currentCrontab, "\n")
	var newLines []string
	skipNext := false
	for _, line := range lines {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.Contains(line, cronMarker) {
			skipNext = true // Skip the marker and the following command line
			continue
		}
		if strings.Contains(line, "nightshift run") {
			continue // Remove old-style entries without marker
		}
		newLines = append(newLines, line)
	}

	// Get cron expression
	cronExpr := cfg.Schedule.Cron
	if cronExpr == "" {
		cronExpr = "0 2 * * *" // Default: 2 AM daily
	}

	// Add new nightshift entry
	newLines = append(newLines, cronMarker)
	newLines = append(newLines, fmt.Sprintf("%s %s run >> %s 2>&1", cronExpr, binaryPath, getLogPath("cron")))

	// Remove trailing empty lines and join
	for len(newLines) > 0 && newLines[len(newLines)-1] == "" {
		newLines = newLines[:len(newLines)-1]
	}
	newCrontab := strings.Join(newLines, "\n") + "\n"

	// Write new crontab
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("updating crontab: %w", err)
	}

	fmt.Printf("Installed cron entry with schedule: %s\n", cronExpr)
	fmt.Println("Crontab updated successfully")
	return nil
}

// runUninstall implements the uninstall command
func runUninstall(cmd *cobra.Command, args []string) error {
	var errors []string
	removed := false

	// Try to uninstall launchd
	if uninstallLaunchd() {
		removed = true
		fmt.Println("Removed launchd service")
	}

	// Try to uninstall systemd
	if uninstallSystemd() {
		removed = true
		fmt.Println("Removed systemd service and timer")
	}

	// Try to uninstall cron
	if uninstallCron() {
		removed = true
		fmt.Println("Removed cron entry")
	}

	if !removed {
		return fmt.Errorf("no nightshift service installation found")
	}

	if len(errors) > 0 {
		return fmt.Errorf("some errors during uninstall: %s", strings.Join(errors, "; "))
	}

	fmt.Println("Nightshift service uninstalled successfully")
	return nil
}

// uninstallLaunchd removes the launchd plist
func uninstallLaunchd() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdPlistName)

	// Check if plist exists
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return false
	}

	// Unload the service
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	// Remove the plist file
	if err := os.Remove(plistPath); err != nil {
		return false
	}

	return true
}

// uninstallSystemd removes the systemd service and timer
func uninstallSystemd() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	systemdDir := filepath.Join(home, ".config", "systemd", "user")
	servicePath := filepath.Join(systemdDir, systemdServiceName)
	timerPath := filepath.Join(systemdDir, systemdTimerName)

	// Check if service or timer exists
	serviceExists := fileExists(servicePath)
	timerExists := fileExists(timerPath)

	if !serviceExists && !timerExists {
		return false
	}

	// Stop and disable timer
	_ = exec.Command("systemctl", "--user", "stop", "nightshift.timer").Run()
	_ = exec.Command("systemctl", "--user", "disable", "nightshift.timer").Run()

	// Remove files
	if serviceExists {
		_ = os.Remove(servicePath)
	}
	if timerExists {
		_ = os.Remove(timerPath)
	}

	// Reload systemd
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	return true
}

// uninstallCron removes the crontab entry
func uninstallCron() bool {
	// Get current crontab
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return false
	}
	currentCrontab := string(out)

	// Check if nightshift entry exists
	if !strings.Contains(currentCrontab, "nightshift") {
		return false
	}

	// Remove nightshift entries
	lines := strings.Split(currentCrontab, "\n")
	var newLines []string
	skipNext := false
	for _, line := range lines {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.Contains(line, cronMarker) {
			skipNext = true
			continue
		}
		if strings.Contains(line, "nightshift run") {
			continue
		}
		newLines = append(newLines, line)
	}

	// Remove trailing empty lines
	for len(newLines) > 0 && newLines[len(newLines)-1] == "" {
		newLines = newLines[:len(newLines)-1]
	}

	newCrontab := strings.Join(newLines, "\n")
	if newCrontab != "" {
		newCrontab += "\n"
	}

	// Write new crontab
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

// parseScheduleTime extracts hour and minute from config schedule
func parseScheduleTime(cfg *config.Config) (hour, minute int) {
	// Default: 2 AM
	hour = 2
	minute = 0

	if cfg.Schedule.Cron == "" {
		return
	}

	// Parse cron: minute hour day month dow
	parts := strings.Fields(cfg.Schedule.Cron)
	if len(parts) >= 2 {
		_, _ = fmt.Sscanf(parts[0], "%d", &minute)
		_, _ = fmt.Sscanf(parts[1], "%d", &hour)
	}

	return
}

// systemdAvailable reports whether systemd is present on this system.
func systemdAvailable() bool {
	if _, err := os.Stat("/run/systemd/private"); err == nil {
		return true
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// execRunner is a function type for running external commands (testable).
type execRunner func(name string, args ...string) error

// defaultExecRunner runs the named command using os/exec.
func defaultExecRunner(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// installSystemdJira writes nightshift-jira.service (and optionally .timer) to
// ~/.config/systemd/user/ and runs systemctl --user daemon-reload.
func installSystemdJira(cfg *config.Config) error {
	return installSystemdJiraWithExec(cfg, defaultExecRunner, os.Stdout)
}

// installSystemdJiraWithExec is the testable core of installSystemdJira.
// Accepts an io.Writer to allow tests and callers to capture output.
func installSystemdJiraWithExec(cfg *config.Config, run execRunner, w io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	systemdDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		return fmt.Errorf("creating systemd directory: %w", err)
	}

	servicePath := filepath.Join(systemdDir, systemdJiraServiceName)
	service := generateSystemdJiraService()
	if err := os.WriteFile(servicePath, []byte(service), 0644); err != nil {
		return fmt.Errorf("writing jira service file: %w", err)
	}
	fmt.Fprintf(w, "Installed %s\n", servicePath)

	onCalendar := cfg.Jira.SystemdOnCalendar
	if onCalendar == "" {
		onCalendar = "*-*-* 22:00:00"
	}
	// Reject OnCalendar values containing newlines to prevent injection
	if strings.ContainsAny(onCalendar, "\r\n") {
		return fmt.Errorf("OnCalendar value contains invalid characters")
	}
	timerPath := filepath.Join(systemdDir, systemdJiraTimerName)
	timer := generateSystemdJiraTimer(onCalendar)
	if err := os.WriteFile(timerPath, []byte(timer), 0644); err != nil {
		return fmt.Errorf("writing jira timer file: %w", err)
	}
	fmt.Fprintf(w, "Installed %s\n", timerPath)

	// Reload systemd so it picks up the new units.
	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("reloading systemd: %w", err)
	}

	fmt.Fprintf(w, "systemd daemon reloaded.\n")
	fmt.Fprintf(w, "To enable and start:\n")
	fmt.Fprintf(w, "  systemctl --user enable --now nightshift-jira.timer\n")
	return nil
}

// generateSystemdJiraService returns the content of the nightshift-jira.service unit.
// Uses the resolved binary path if it exists; prefers %h/.local/bin if binary is in home/.local/bin.
func generateSystemdJiraService() string {
	execStart := "%h/.local/bin/nightshift jira run"

	// Try to determine actual binary path for better portability
	if binaryPath, err := os.Executable(); err == nil {
		home, err := os.UserHomeDir()
		if err == nil {
			absPath, err := filepath.Abs(binaryPath)
			if err == nil {
				// If binary is in ~/.local/bin, use %h expansion for portability
				localBinPath := filepath.Join(home, ".local", "bin")
				if strings.HasPrefix(absPath, localBinPath) {
					// Binary is in ~/.local/bin, use %h expansion
					execStart = "%h/.local/bin/nightshift jira run"
				} else {
					// Binary is elsewhere, use absolute path
					execStart = absPath + " jira run"
				}
			}
		}
	}

	return fmt.Sprintf(`[Unit]
Description=Nightshift Jira autonomous pipeline
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=%s
Environment=HOME=%%h
EnvironmentFile=-%%h/.config/nightshift/env
StandardOutput=journal
StandardError=journal
SyslogIdentifier=nightshift-jira

[Install]
WantedBy=default.target
`, execStart)
}

// generateSystemdJiraTimer returns the content of the nightshift-jira.timer unit.
func generateSystemdJiraTimer(onCalendar string) string {
	return fmt.Sprintf(`[Unit]
Description=Run Nightshift Jira pipeline on schedule

[Timer]
OnCalendar=%s
Persistent=true

[Install]
WantedBy=timers.target
`, onCalendar)
}

// validateOnCalendar checks an OnCalendar expression via systemd-analyze calendar.
// Returns nil if systemd-analyze is not found and systemd is unavailable.
// Returns a warning if systemd-analyze is missing but systemd is available.
func validateOnCalendar(expr string) error {
	// Reject values containing newlines to prevent injection
	if strings.ContainsAny(expr, "\r\n") {
		return fmt.Errorf("OnCalendar value contains invalid characters")
	}
	path, err := exec.LookPath("systemd-analyze")
	if err != nil {
		// systemd-analyze not available: check if systemd is available
		if _, err := exec.LookPath("systemctl"); err == nil {
			// systemd is available but systemd-analyze is not — warn but don't fail
			// (systemd will validate the expression at enable time)
			return fmt.Errorf("warning: systemd-analyze not found; OnCalendar will be validated by systemd at enable time")
		}
		// Neither systemd nor systemd-analyze available — skip validation
		return nil
	}
	out, err := exec.Command(path, "calendar", expr).CombinedOutput()
	if err != nil {
		return fmt.Errorf("invalid OnCalendar expression %q: %s", expr, strings.TrimSpace(string(out)))
	}
	return nil
}

// getLogPath returns the log file path for the specified type
func getLogPath(logType string) string {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".local", "share", "nightshift", "logs")
	_ = os.MkdirAll(logDir, 0755)

	switch logType {
	case "stdout":
		return filepath.Join(logDir, "launchd-stdout.log")
	case "stderr":
		return filepath.Join(logDir, "launchd-stderr.log")
	case "cron":
		return filepath.Join(logDir, "cron.log")
	default:
		return filepath.Join(logDir, "nightshift.log")
	}
}
