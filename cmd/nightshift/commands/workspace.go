package commands

import (
	"fmt"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/workspace"
	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage clone-based task workspaces",
	Long:  `Commands for managing isolated workspace directories created for task runs.`,
}

var workspaceCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove stale workspaces older than TTL",
	Long: `Remove workspace directories that are older than the configured TTL (default 7 days).

Workspace mode is activated by setting workspace.root in the config file.
This command can also be invoked with --root and --days to override config values.`,
	RunE: runWorkspaceClean,
}

func init() {
	workspaceCleanCmd.Flags().String("root", "", "Override workspace root directory")
	workspaceCleanCmd.Flags().Int("days", 0, "Override TTL in days (default 7)")
	workspaceCmd.AddCommand(workspaceCleanCmd)
	rootCmd.AddCommand(workspaceCmd)
}

func runWorkspaceClean(cmd *cobra.Command, _ []string) error {
	rootOverride, _ := cmd.Flags().GetString("root")
	daysOverride, _ := cmd.Flags().GetInt("days")

	cfg, err := loadConfig("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	wsCfg := workspaceConfigFromApp(cfg)
	if rootOverride != "" {
		wsCfg.Root = rootOverride
	}
	if daysOverride > 0 {
		wsCfg.TTLDays = daysOverride
	}
	if wsCfg.Root == "" {
		return fmt.Errorf("workspace.root not configured; use --root to specify")
	}

	ttl := wsCfg.TTLDays
	if ttl <= 0 {
		ttl = 7
	}

	start := time.Now()
	n, err := workspace.CleanupStaleWorkspaces(wsCfg)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	fmt.Printf("removed %d workspace(s) older than %d day(s) (%s)\n", n, ttl, time.Since(start).Round(time.Millisecond))
	return nil
}
