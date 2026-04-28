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

	// Phase → provider + model table.
	b.WriteString(styles.Section.Render("Phase Assignments"))
	b.WriteString("\n")
	for _, p := range result.Phases {
		provider := p.Provider
		if provider == "" {
			provider = "claude"
		}
		model := p.Model
		if model == "" {
			model = "default"
		}
		timeout := p.Timeout
		if timeout == "" {
			timeout = "30m"
		}
		fmt.Fprintf(b, "  %-12s → %-10s  %s  timeout=%s\n", p.Name, provider, styles.Muted.Render(model), timeout)
	}
	b.WriteString("\n")

	// Budget (summary always shown; details behind --explain).
	if result.Budget != nil {
		b.WriteString(styles.Section.Render("Budget"))
		b.WriteString("\n")
		if opts.Explain {
			renderBudgetText(b, result.Budget, "  ")
		} else {
			fmt.Fprintf(b, "  %s available (%.1f%% used, source=%s)\n",
				formatTokens64(result.Budget.Allowance),
				result.Budget.UsedPercent,
				result.Budget.BudgetSource)
		}
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
			typeStr := ""
			if t.IssueType != "" {
				typeStr = fmt.Sprintf("  Type: %s", styles.Label.Render(t.IssueType))
			}
			fmt.Fprintf(b, "     Status: %s  Branch: %s%s\n", styles.Label.Render(t.Status), styles.Value.Render(t.BranchName), typeStr)
			if t.ValidationScore != nil {
				scoreStyle := styles.Value
				if t.ValidationMsg != "" {
					scoreStyle = styles.Warn
				}
				fmt.Fprintf(b, "     Validation: %s\n", scoreStyle.Render(fmt.Sprintf("score %d/10", *t.ValidationScore)))
			}
			if len(t.Dependencies) > 0 {
				fmt.Fprintf(b, "     Blocked by: %s\n", strings.Join(t.Dependencies, ", "))
			}
			if len(t.Blocks) > 0 {
				fmt.Fprintf(b, "     Blocks: %s\n", strings.Join(t.Blocks, ", "))
			}
		}
	}
	b.WriteString("\n")

	// Full execution order: ready tickets + blocked tickets together.
	if len(result.FullOrder) > 0 {
		readyCount := len(result.ExecutionOrder)
		blockedCount := len(result.FullOrder) - readyCount
		title := fmt.Sprintf("Execution Order (%d ready", readyCount)
		if blockedCount > 0 {
			title += fmt.Sprintf(", %d blocked", blockedCount)
		}
		title += ")"
		b.WriteString(styles.Section.Render(title))
		b.WriteString("\n")
		for i, entry := range result.FullOrder {
			if entry.Ready {
				fmt.Fprintf(b, "  %2d. %s\n", i+1, styles.Accent.Render(entry.Key))
			} else {
				blocker := entry.Blocker
				if blocker == "" {
					blocker = entry.Reason
				}
				fmt.Fprintf(b, "  %2d. %s  %s\n", i+1,
					styles.Warn.Render(entry.Key),
					styles.Muted.Render("⊘ blocked by "+blocker))
			}
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

	b.WriteString(styles.Muted.Render(fmt.Sprintf("Generated at %s", result.GeneratedAt.Format("2006-01-02 15:04:05"))))
	b.WriteString("\n")

	return b.String()
}
