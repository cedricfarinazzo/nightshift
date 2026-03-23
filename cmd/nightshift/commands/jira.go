package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/marcus/nightshift/internal/config"
	"github.com/marcus/nightshift/internal/jira"
	"github.com/marcus/nightshift/internal/logging"
	"github.com/spf13/cobra"
)

var jiraCmd = &cobra.Command{
	Use:   "jira",
	Short: "Jira integration (daemon)",
	Long: `Connect to Jira, validate configuration, and check API connectivity.

This command starts the Jira integration daemon. In this initial phase it
loads and validates config, authenticates to Jira, fetches configured
projects, logs startup information, and exits.`,
	RunE: runJira,
}

func init() {
	rootCmd.AddCommand(jiraCmd)
}

func runJira(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := initLogging(cfg); err != nil {
		return fmt.Errorf("init logging: %w", err)
	}
	log := logging.Component("jira")

	log.Info("jira starting")

	if cfg.Jira.URL == "" {
		return fmt.Errorf("jira not configured: set jira.url in config")
	}

	log.InfoCtx("jira config loaded", map[string]any{
		"url":          cfg.Jira.URL,
		"email":        cfg.Jira.Email,
		"projects":     cfg.Jira.ProjectKeys,
		"label":        cfg.Jira.Label,
		"max_tickets":  cfg.Jira.MaxTickets,
		"concurrency":  cfg.Jira.Concurrency,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Infof("received signal %v, shutting down", sig)
		cancel()
	}()

	client, err := jira.New(&cfg.Jira)
	if err != nil {
		return fmt.Errorf("jira auth: %w", err)
	}

	log.Info("jira checking connectivity")
	if err := client.CheckConnectivity(ctx); err != nil {
		return err
	}
	log.Info("jira connectivity ok")

	projects, err := client.FetchProjects(ctx)
	if err != nil {
		return err
	}

	for _, p := range projects {
		log.InfoCtx("jira project found", map[string]any{
			"key":  p.Key,
			"name": p.Name,
		})
	}

	log.InfoCtx("jira startup complete", map[string]any{
		"projects_found": len(projects),
	})

	return nil
}
