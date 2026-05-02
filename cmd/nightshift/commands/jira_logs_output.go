package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/nightshift/internal/db"
)

const maxOutputRunes = 10_000

func showJiraRunSummary(ctx context.Context, database *db.DB, runID, ticketFilter, statusFilter string, raw bool) error {
	run, err := database.GetJiraRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get run %s: %w", runID, err)
	}
	if run == nil {
		return fmt.Errorf("run %s not found", runID)
	}

	results, err := database.GetJiraTicketResults(ctx, runID)
	if err != nil {
		return fmt.Errorf("get ticket results: %w", err)
	}

	// Apply filters.
	var filtered []db.JiraTicketResult
	for _, r := range results {
		if ticketFilter != "" && !strings.EqualFold(r.TicketKey, ticketFilter) {
			continue
		}
		if statusFilter != "" && !strings.EqualFold(r.Status, statusFilter) {
			continue
		}
		filtered = append(filtered, r)
	}

	if raw {
		return renderJiraRunSummaryJSON(run, filtered)
	}

	return renderJiraRunSummaryTable(run, filtered)
}

func renderJiraRunSummaryJSON(run *db.JiraRun, results []db.JiraTicketResult) error {
	enc := json.NewEncoder(newWriter())
	_ = enc.Encode(map[string]any{
		"run":     run,
		"tickets": results,
	})
	return nil
}

type stdoutWriter struct{}

func newWriter() *stdoutWriter { return &stdoutWriter{} }
func (w *stdoutWriter) Write(p []byte) (int, error) {
	fmt.Print(string(p))
	return len(p), nil
}

func renderJiraRunSummaryTable(run *db.JiraRun, results []db.JiraTicketResult) error {
	styles := newLogStyles()

	// Header.
	endStr := "in progress"
	if run.EndedAt != nil {
		endStr = run.EndedAt.Format("2006-01-02T15:04:05Z")
	}
	header := fmt.Sprintf("Jira Run  %s  → %s  (%d processed, %d completed, %d failed)",
		run.StartedAt.Format("2006-01-02T15:04:05Z"),
		endStr,
		run.TicketsProcessed, run.TicketsCompleted, run.TicketsFailed,
	)
	fmt.Println(styles.Title.Render(header))
	fmt.Println()

	if len(results) == 0 {
		fmt.Println(styles.Muted.Render("  No ticket results."))
		return nil
	}

	// Column widths.
	const (
		wTicket = 10
		wStatus = 12
		wDur    = 10
		wPhase  = 12
		wPR     = 40
	)

	headerLine := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %s",
		wTicket, "TICKET",
		wStatus, "STATUS",
		wDur, "DURATION",
		wPhase, "PHASE",
		"PR",
	)
	fmt.Println(styles.Label.Render(headerLine))
	fmt.Println(styles.Muted.Render("  " + strings.Repeat("─", 70)))

	for _, r := range results {
		dur := formatDurationMs(r.DurationMs)
		prCell := "—"
		if r.PRURL != "" {
			prCell = r.PRURL
			if len(prCell) > wPR {
				prCell = "…" + prCell[len(prCell)-wPR+1:]
			}
		}
		statusStyle := statusStyle(styles, r.Status)
		line := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %s",
			wTicket, r.TicketKey,
			wStatus, r.Status,
			wDur, dur,
			wPhase, r.PhaseReached,
			prCell,
		)
		_ = statusStyle
		fmt.Println(line)
	}

	return nil
}

func showJiraAgentOutput(ctx context.Context, database *db.DB, runID, ticketKey, phaseFilter string, raw bool) error {
	logs, err := database.GetJiraPhaseLogs(ctx, runID, ticketKey, phaseFilter)
	if err != nil {
		return fmt.Errorf("get phase logs: %w", err)
	}

	if len(logs) == 0 {
		fmt.Println("No phase logs found.")
		return nil
	}

	if raw {
		enc := json.NewEncoder(newWriter())
		_ = enc.Encode(logs)
		return nil
	}

	styles := newLogStyles()

	// Group by ticket key.
	type group struct {
		key  string
		logs []db.JiraPhaseLog
	}
	var groups []group
	seen := make(map[string]int)
	for _, l := range logs {
		if idx, ok := seen[l.TicketKey]; ok {
			groups[idx].logs = append(groups[idx].logs, l)
		} else {
			seen[l.TicketKey] = len(groups)
			groups = append(groups, group{key: l.TicketKey, logs: []db.JiraPhaseLog{l}})
		}
	}

	for gi, g := range groups {
		if gi > 0 {
			fmt.Println()
		}
		fmt.Println(styles.Title.Render(fmt.Sprintf("%s — phase logs", g.key)))
		for _, l := range g.logs {
			renderPhaseEntry(l, styles)
		}
	}

	return nil
}

func renderPhaseEntry(l db.JiraPhaseLog, styles logStyles) {
	status := "✓"
	if !l.ExitOk {
		status = "✗"
	}
	providerModel := "—"
	if l.Provider != "" || l.Model != "" {
		providerModel = fmt.Sprintf("%s/%s", l.Provider, l.Model)
	}
	dur := formatDurationMs(l.DurationMs)

	headerLine := fmt.Sprintf("  %-12s  %-30s  %-8s  %s",
		l.Phase, providerModel, dur, status)

	var lineStyle lipgloss.Style
	if l.ExitOk {
		lineStyle = styles.LevelInfo
	} else {
		lineStyle = styles.LevelError
	}
	fmt.Println(lineStyle.Render(headerLine))
	fmt.Println(styles.Muted.Render("  " + strings.Repeat("─", 60)))

	if l.Phase == "commit" || l.Phase == "pr" || l.Phase == "status" {
		fmt.Println(styles.Muted.Render("  (no agent output)"))
	} else if l.Output == "" && l.Error == "" {
		fmt.Println(styles.Muted.Render("  (no output recorded)"))
	} else {
		if l.Output != "" {
			output := truncateOutput(l.Output)
			for _, line := range strings.Split(output, "\n") {
				fmt.Printf("  %s\n", line)
			}
		}
		if l.Error != "" {
			fmt.Println(styles.LevelError.Render("  Error: " + l.Error))
		}
	}
	fmt.Println()
}

func truncateOutput(s string) string {
	if utf8.RuneCountInString(s) <= maxOutputRunes {
		return s
	}
	runes := []rune(s)
	truncated := runes[len(runes)-maxOutputRunes:]
	return "[... truncated ...]\n" + string(truncated)
}

func formatDurationMs(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

func statusStyle(styles logStyles, status string) lipgloss.Style {
	switch strings.ToLower(status) {
	case "completed":
		return styles.LevelInfo
	case "failed":
		return styles.LevelError
	case "rejected":
		return styles.LevelWarn
	default:
		return styles.Muted
	}
}
