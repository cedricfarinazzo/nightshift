package commands

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type jiraPreviewTextOptions struct {
	Explain bool
}

func renderJiraPreviewText(result *jiraPreviewResult, opts jiraPreviewTextOptions) string {
	styles := newPreviewStyles()
	b := &strings.Builder{}

	b.WriteString(styles.Title.Render("Nightshift Jira Preview"))
	b.WriteString("\n")
	b.WriteString(styles.Muted.Render("Dry-run of nightshift jira run. No state is modified."))
	b.WriteString("\n\n")

	// Connection status.
	b.WriteString(styles.Section.Render("Connection"))
	b.WriteString("\n")
	if result.ConnectionOK {
		fmt.Fprintf(b, "  Status:  %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render("OK"))
		fmt.Fprintf(b, "  Project: %s\n", result.JiraProject)
		fmt.Fprintf(b, "  User:    %s\n", result.JiraUser)
	} else if result.ConnectionErr != "" {
		fmt.Fprintf(b, "  Status:  %s\n", styles.Error.Render("FAILED"))
		fmt.Fprintf(b, "  Error:   %s\n", styles.Warn.Render(result.ConnectionErr))
	} else {
		fmt.Fprintf(b, "  Project: %s\n", result.JiraProject)
	}
	b.WriteString("\n")

	// Phase → provider table.
	b.WriteString(styles.Section.Render("Phase Assignments"))
	b.WriteString("\n")
	for _, phase := range []string{"validation", "plan", "implement", "review_fix"} {
		provider := result.Phases[phase]
		if provider == "" {
			provider = "claude"
		}
		fmt.Fprintf(b, "  %-12s → %s\n", phase, provider)
	}
	b.WriteString("\n")

	// Budget.
	if result.Budget != nil {
		b.WriteString(styles.Section.Render("Budget"))
		b.WriteString("\n")
		renderBudgetText(b, result.Budget, "  ")
		b.WriteString("\n")
	} else if result.BudgetErr != "" {
		b.WriteString(styles.Section.Render("Budget"))
		b.WriteString("\n")
		b.WriteString("  ")
		b.WriteString(styles.Warn.Render(fmt.Sprintf("budget unavailable: %s", result.BudgetErr)))
		b.WriteString("\n\n")
	}

	// TODO tickets in execution order.
	b.WriteString(styles.Section.Render(fmt.Sprintf("TODO Tickets (%d ready)", len(result.TodoTickets))))
	b.WriteString("\n")
	if len(result.TodoTickets) == 0 {
		b.WriteString("  ")
		b.WriteString(styles.Muted.Render("none"))
		b.WriteString("\n")
	} else {
		for i, t := range result.TodoTickets {
			b.WriteString(styles.Accent.Render(fmt.Sprintf("  %d. %s", i+1, t.Key)))
			fmt.Fprintf(b, "  %s\n", t.Summary)
			fmt.Fprintf(b, "     Status: %s  Branch: %s\n", styles.Label.Render(t.Status), styles.Value.Render(t.BranchName))
			if len(t.Dependencies) > 0 {
				fmt.Fprintf(b, "     Blocked by: %s\n", strings.Join(t.Dependencies, ", "))
			}
			if len(t.Blocks) > 0 {
				fmt.Fprintf(b, "     Blocks: %s\n", strings.Join(t.Blocks, ", "))
			}
		}
	}
	b.WriteString("\n")

	// Execution order.
	if len(result.ExecutionOrder) > 0 {
		b.WriteString(styles.Section.Render("Execution Order"))
		b.WriteString("\n")
		for i, key := range result.ExecutionOrder {
			fmt.Fprintf(b, "  %d. %s\n", i+1, key)
		}
		b.WriteString("\n")
	}

	// Blocked tickets.
	if len(result.BlockedTickets) > 0 {
		b.WriteString(styles.Section.Render(fmt.Sprintf("Blocked Tickets (%d)", len(result.BlockedTickets))))
		b.WriteString("\n")
		for _, bt := range result.BlockedTickets {
			b.WriteString("  ")
			b.WriteString(styles.Warn.Render(bt.Key))
			fmt.Fprintf(b, "  reason=%s", bt.Reason)
			if len(bt.Blockers) > 0 {
				fmt.Fprintf(b, "  blockers=[%s]", strings.Join(bt.Blockers, ", "))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Review tickets.
	b.WriteString(styles.Section.Render(fmt.Sprintf("Review Tickets (%d awaiting rework)", len(result.ReviewTickets))))
	b.WriteString("\n")
	if len(result.ReviewTickets) == 0 {
		b.WriteString("  ")
		b.WriteString(styles.Muted.Render("none"))
		b.WriteString("\n")
	} else {
		for _, t := range result.ReviewTickets {
			fmt.Fprintf(b, "  %s  %s  (%s)\n", styles.Accent.Render(t.Key), t.Summary, styles.Label.Render(t.Status))
		}
	}
	b.WriteString("\n")

	// Skipped / errors.
	if len(result.SkippedTickets) > 0 {
		b.WriteString(styles.Section.Render("Skipped"))
		b.WriteString("\n")
		for _, s := range result.SkippedTickets {
			fmt.Fprintf(b, "  %s  %s\n", styles.Warn.Render(s.Key), s.Reason)
		}
		b.WriteString("\n")
	}

	if opts.Explain && result.Budget != nil {
		b.WriteString(styles.Muted.Render("Budget details shown above. Token allowance is for the implement phase provider."))
		b.WriteString("\n")
	}

	b.WriteString(styles.Muted.Render(fmt.Sprintf("Generated at %s", result.GeneratedAt.Format("2006-01-02 15:04:05"))))
	b.WriteString("\n")

	return b.String()
}
