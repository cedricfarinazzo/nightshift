package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/nightshift/internal/db"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

// validPhases lists all phase values accepted by --phase.
var validPhases = []string{"validate", "plan", "implement", "commit", "pr", "status", "review_fix"}

var jiraLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View Jira autonomous run logs",
	Long: `View logs from nightshift jira run invocations.

By default shows the last 50 log entries scoped to the jira component.
Use --summary to see a table of tickets processed in a run.
Use --agent-output to see raw agent stdout stored during a run.`,
	RunE: runJiraLogs,
}

func init() {
	jiraCmd.AddCommand(jiraLogsCmd)

	jiraLogsCmd.Flags().IntP("tail", "n", 50, "Number of log entries to show (default 50)")
	jiraLogsCmd.Flags().BoolP("follow", "f", false, "Follow log output in real-time")
	jiraLogsCmd.Flags().StringP("ticket", "t", "", "Filter by ticket key (e.g. VC-42)")
	jiraLogsCmd.Flags().StringP("run", "r", "", "Filter by run ID or \"latest\"")
	jiraLogsCmd.Flags().String("phase", "", "Filter by phase (validate|plan|implement|commit|pr|status|review_fix)")
	jiraLogsCmd.Flags().String("status", "", "Filter by outcome (completed|failed|rejected|skipped)")
	jiraLogsCmd.Flags().String("since", "", "Start time filter (YYYY-MM-DD or RFC3339)")
	jiraLogsCmd.Flags().String("until", "", "End time filter (YYYY-MM-DD or RFC3339)")
	jiraLogsCmd.Flags().String("level", "", "Min log level (debug|info|warn|error)")
	jiraLogsCmd.Flags().Bool("summary", false, "Show run summary only (ticket counts, durations, outcomes)")
	jiraLogsCmd.Flags().Bool("agent-output", false, "Show full agent stdout from jira_phase_logs")
	jiraLogsCmd.Flags().Bool("raw", false, "Raw JSON output (for piping)")
	jiraLogsCmd.Flags().Bool("no-color", false, "Disable ANSI colors")
	jiraLogsCmd.Flags().StringP("export", "e", "", "Export to file")
}

func runJiraLogs(cmd *cobra.Command, _ []string) error {
	tail, _ := cmd.Flags().GetInt("tail")
	follow, _ := cmd.Flags().GetBool("follow")
	ticketKey, _ := cmd.Flags().GetString("ticket")
	runFlag, _ := cmd.Flags().GetString("run")
	phaseFlag, _ := cmd.Flags().GetString("phase")
	statusFlag, _ := cmd.Flags().GetString("status")
	sinceStr, _ := cmd.Flags().GetString("since")
	untilStr, _ := cmd.Flags().GetString("until")
	levelFlag, _ := cmd.Flags().GetString("level")
	summary, _ := cmd.Flags().GetBool("summary")
	agentOutput, _ := cmd.Flags().GetBool("agent-output")
	raw, _ := cmd.Flags().GetBool("raw")
	noColor, _ := cmd.Flags().GetBool("no-color")
	exportFile, _ := cmd.Flags().GetString("export")

	if noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	// Validate --phase value.
	if phaseFlag != "" {
		valid := false
		for _, p := range validPhases {
			if p == phaseFlag {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid --phase %q: must be one of %s", phaseFlag, strings.Join(validPhases, "|"))
		}
	}

	// Flag compatibility checks.
	if follow && (summary || agentOutput || raw || exportFile != "" || runFlag != "") {
		return fmt.Errorf("--follow cannot be combined with --summary, --agent-output, --raw, --export, or --run")
	}

	// --status and --phase only work with DB-backed modes.
	if statusFlag != "" && !summary && !agentOutput && runFlag == "" {
		return fmt.Errorf("--status requires --summary, --agent-output, or --run")
	}
	if phaseFlag != "" && !summary && !agentOutput && runFlag == "" {
		return fmt.Errorf("--phase requires --summary, --agent-output, or --run")
	}

	// DB-backed modes need the database.
	needsDB := summary || agentOutput || runFlag != ""

	var database *db.DB
	if needsDB {
		var err error
		database, err = db.Open(db.DefaultPath())
		if err != nil {
			return fmt.Errorf("cannot open database (required for --summary/--run/--agent-output): %w", err)
		}
		defer func() { _ = database.Close() }()
	}

	// --run requires --summary or --agent-output.
	if runFlag != "" && !summary && !agentOutput {
		return fmt.Errorf("--run requires --summary or --agent-output")
	}

	ctx := context.Background()

	// Resolve --run latest.
	resolvedRunID := runFlag
	if runFlag == "latest" {
		if database == nil {
			return fmt.Errorf("--run latest requires database access")
		}
		id, err := database.GetLatestJiraRunID(ctx)
		if err != nil {
			return fmt.Errorf("resolve latest run: %w", err)
		}
		if id == "" {
			return fmt.Errorf("no jira runs found in database")
		}
		resolvedRunID = id
	}

	// --summary mode: query DB and render table.
	if summary {
		if resolvedRunID == "" {
			// Find latest run.
			id, err := database.GetLatestJiraRunID(ctx)
			if err != nil {
				return fmt.Errorf("get latest run: %w", err)
			}
			if id == "" {
				return fmt.Errorf("no jira runs found in database")
			}
			resolvedRunID = id
		}
		return showJiraRunSummary(ctx, database, resolvedRunID, ticketKey, statusFlag, raw)
	}

	// --agent-output mode: show phase logs from DB.
	if agentOutput {
		if resolvedRunID == "" {
			id, err := database.GetLatestJiraRunID(ctx)
			if err != nil {
				return fmt.Errorf("get latest run: %w", err)
			}
			if id == "" {
				return fmt.Errorf("no jira runs found in database")
			}
			resolvedRunID = id
		}
		return showJiraAgentOutput(ctx, database, resolvedRunID, ticketKey, phaseFlag, raw)
	}

	// Log-file mode: build filter.
	logDir := resolveLogDir("")
	filter := logFilter{
		level:     strings.ToLower(strings.TrimSpace(levelFlag)),
		component: "jira",
		ticketKey: strings.ToUpper(strings.TrimSpace(ticketKey)),
	}
	if filter.level != "" && levelRank(filter.level) == 0 {
		return fmt.Errorf("invalid log level %q (use debug|info|warn|error)", filter.level)
	}
	if sinceStr != "" {
		parsed, err := parseTimeInput(sinceStr, time.Local)
		if err != nil {
			return err
		}
		filter.since = &parsed
	}
	if untilStr != "" {
		parsed, err := parseTimeInput(untilStr, time.Local)
		if err != nil {
			return err
		}
		filter.until = &parsed
	}

	if exportFile != "" {
		return exportLogs(logDir, exportFile, filter, tail)
	}
	if follow {
		if filter.until != nil {
			return fmt.Errorf("--until cannot be used with --follow")
		}
		return followLogs(logDir, tail, filter, raw)
	}
	return showLogs(logDir, tail, filter, false, raw)
}
