package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/cedricfarinazzo/nightshift/internal/budget"
)

type previewTextOptions struct {
	LongPrompt bool
}

type previewPagerOptions struct {
	Plain bool
}

type previewStyles struct {
	Title   lipgloss.Style
	Section lipgloss.Style
	Label   lipgloss.Style
	Value   lipgloss.Style
	Muted   lipgloss.Style
	Warn    lipgloss.Style
	Error   lipgloss.Style
	Accent  lipgloss.Style
}

func newPreviewStyles() previewStyles {
	return previewStyles{
		Title:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69")),
		Section: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
		Label:   lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		Value:   lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		Muted:   lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		Warn:    lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		Error:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		Accent:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
	}
}

func renderPreviewText(result *previewResult, opts previewTextOptions) string {
	styles := newPreviewStyles()
	b := &strings.Builder{}

	b.WriteString(styles.Title.Render("Nightshift Preview"))
	b.WriteString("\n")
	b.WriteString(styles.Muted.Render(fmt.Sprintf("Previewing next %d run(s). Assumes current state and usage; no tasks are executed.", len(result.Runs))))
	b.WriteString("\n\n")

	b.WriteString(styles.Section.Render("Summary"))
	b.WriteString("\n")
	fmt.Fprintf(b, "  Provider: %s (preview picks first enabled: claude -> codex -> copilot)\n", result.Provider)
	fmt.Fprintf(b, "  Budget limit: %d%%\n", result.MaxPercent)
	if result.ConfigSources != nil {
		fmt.Fprintf(b, "  Config global: %s (%s)\n", result.ConfigSources.GlobalPath, configLoadedLabel(result.ConfigSources.GlobalExists))
		fmt.Fprintf(b, "  Config project: %s (%s)\n", result.ConfigSources.ProjectPath, configLoadedLabel(result.ConfigSources.ProjectExists))
		b.WriteString("  Config order: global -> project -> env overrides\n")
	}
	if len(result.Providers) > 0 {
		b.WriteString("  Provider budgets:\n")
		for _, summary := range result.Providers {
			if summary.err != nil {
				fmt.Fprintf(b, "    - %s: %v\n", summary.name, summary.err)
				continue
			}
			model := summary.model
			if model == "" {
				model = "default"
			}
			line := fmt.Sprintf("    - %s: %.0f%% capacity  %.1f%% used (limit: %d%%, source=%s, model=%s)",
				summary.name,
				summary.allowance.HourlyCapacity*100,
				summary.allowance.BottleneckUsedPct,
				summary.allowance.MaxPercent,
				summary.allowance.Source,
				model)
			fmt.Fprintln(b, line)
		}
	}
	if result.TaskFilter != "" {
		fmt.Fprintf(b, "  Task filter: %s\n", result.TaskFilter)
	} else if len(result.EnabledTasks) == 0 {
		b.WriteString("  Task filter: all enabled tasks (none explicitly enabled)\n")
	} else {
		fmt.Fprintf(b, "  Task filter: enabled list (%d) [%s]\n", len(result.EnabledTasks), strings.Join(result.EnabledTasks, ", "))
	}
	if result.ProjectCount > 1 {
		b.WriteString("  Note: budget is not split per project during preview/run\n")
	}
	if result.Timeout != "" {
		fmt.Fprintf(b, "  Agent timeout: %s\n", result.Timeout)
	}
	if result.Compression != "" {
		fmt.Fprintf(b, "  Compression: %s\n", result.Compression)
	}
	if result.WorkspaceMode != "" {
		fmt.Fprintf(b, "  Workspace mode: %s\n", result.WorkspaceMode)
	}

	for _, run := range result.Runs {
		b.WriteString("\n")
		b.WriteString(styles.Section.Render(fmt.Sprintf("Run %d · %s", run.Index, run.RunAt.Format("2006-01-02 15:04"))))
		b.WriteString("\n")

		for _, project := range run.Projects {
			b.WriteString(styles.Label.Render("  " + project.Path))
			b.WriteString("\n")

			switch project.Status {
			case previewProjectSkipped:
				b.WriteString("    ")
				b.WriteString(styles.Muted.Render(fmt.Sprintf("skipped: %s", project.Detail)))
				b.WriteString("\n")
				continue
			case previewProjectBudgetExhausted:
				b.WriteString("    ")
				b.WriteString(styles.Warn.Render(project.Detail))
				b.WriteString("\n")
			case previewProjectNoTasks:
				b.WriteString("    ")
				b.WriteString(styles.Warn.Render(project.Detail))
				b.WriteString("\n")
			case previewProjectError:
				b.WriteString("    ")
				b.WriteString(styles.Error.Render(project.Detail))
				b.WriteString("\n")
			}

			if project.Diagnostics != nil {
				renderDiagnosticsText(b, styles, project.Diagnostics, "    ")
			}

			if project.Status != previewProjectReady {
				continue
			}

			renderBudgetText(b, project.Budget, "    ")

			for _, task := range project.Tasks {
				b.WriteString("    ")
				b.WriteString(styles.Accent.Render(fmt.Sprintf("%d. %s", task.Index, task.Name)))
				fmt.Fprintf(b, " (%s)\n", task.Type)
				b.WriteString("       ")
				b.WriteString(styles.Muted.Render(fmt.Sprintf("score=%.1f, cost=%s (%d-%d)\n", task.Score, task.CostTier, task.MinTokens, task.MaxTokens)))
				b.WriteString("       Prompt:\n")
				preview := renderPromptPreview(task.Prompt, opts.LongPrompt)
				b.WriteString(indentLines(preview, "       "))
				b.WriteString("\n")
				if task.PromptFileError != "" {
					b.WriteString("       ")
					b.WriteString(styles.Warn.Render(fmt.Sprintf("Prompt file: error writing (%s)", task.PromptFileError)))
					b.WriteString("\n")
				} else if task.PromptFile != "" {
					fmt.Fprintf(b, "       Prompt file: %s\n", task.PromptFile)
				}
				b.WriteString("\n")
			}
		}
	}

	if result.Note != "" {
		b.WriteString(styles.Muted.Render(result.Note))
		b.WriteString("\n")
	}

	return b.String()
}

func renderSetupPreviewText(result *previewResult) string {
	styles := newPreviewStyles()
	b := &strings.Builder{}

	if len(result.Runs) == 0 {
		b.WriteString(styles.Warn.Render("No upcoming runs found for the current schedule."))
		b.WriteString("\n")
		return b.String()
	}

	run := result.Runs[0]
	b.WriteString(styles.Section.Render("Next run"))
	b.WriteString("\n")
	fmt.Fprintf(b, "  Scheduled: %s\n", run.RunAt.Format("Mon 2006-01-02 15:04"))
	fmt.Fprintf(b, "  Provider: %s\n", result.Provider)

	totalTasks := 0
	for _, project := range run.Projects {
		if project.Status == previewProjectReady {
			totalTasks += len(project.Tasks)
		}
	}
	fmt.Fprintf(b, "  Tasks queued: %d\n", totalTasks)

	b.WriteString("\n")
	b.WriteString(styles.Section.Render("Tasks"))
	b.WriteString("\n")
	for _, project := range run.Projects {
		b.WriteString(styles.Label.Render("  " + project.Path))
		b.WriteString("\n")
		switch project.Status {
		case previewProjectSkipped:
			b.WriteString("    ")
			b.WriteString(styles.Muted.Render(fmt.Sprintf("skipped: %s", project.Detail)))
			b.WriteString("\n")
			continue
		case previewProjectBudgetExhausted:
			b.WriteString("    ")
			b.WriteString(styles.Warn.Render(project.Detail))
			b.WriteString("\n")
		case previewProjectNoTasks:
			b.WriteString("    ")
			b.WriteString(styles.Warn.Render(project.Detail))
			b.WriteString("\n")
		case previewProjectError:
			b.WriteString("    ")
			b.WriteString(styles.Error.Render(project.Detail))
			b.WriteString("\n")
		}

		if project.Status != previewProjectReady {
			continue
		}
		if len(project.Tasks) == 0 {
			b.WriteString("    ")
			b.WriteString(styles.Muted.Render("no tasks selected"))
			b.WriteString("\n")
			continue
		}
		for _, task := range project.Tasks {
			b.WriteString("    ")
			b.WriteString(styles.Accent.Render(fmt.Sprintf("%d. %s", task.Index, task.Name)))
			if task.Type != "" || task.CostTier != "" {
				meta := []string{}
				if task.Type != "" {
					meta = append(meta, task.Type)
				}
				if task.CostTier != "" {
					meta = append(meta, task.CostTier)
				}
				b.WriteString(styles.Muted.Render(fmt.Sprintf(" (%s)", strings.Join(meta, ", "))))
			}
			b.WriteString("\n")
		}
	}

	if result.Note != "" {
		b.WriteString("\n")
		b.WriteString(styles.Muted.Render(result.Note))
		b.WriteString("\n")
	}

	return b.String()
}

func renderBudgetText(b *strings.Builder, allowance *budget.AllowanceResult, indent string) {
	if allowance == nil {
		return
	}
	b.WriteString(indent)
	fmt.Fprintf(b, "Budget: %.0f%% capacity  (bottleneck: %s, limit: %d%%, source: %s)\n",
		allowance.HourlyCapacity*100, allowance.BottleneckWindow, allowance.MaxPercent, allowance.Source)
	for _, w := range allowance.Windows {
		marker := " "
		if w.Name == allowance.BottleneckWindow {
			marker = "▶"
		}
		resetStr := ""
		if w.ResetIn > 0 {
			resetStr = fmt.Sprintf("  resets in %s", formatDuration(w.ResetIn))
		}
		fmt.Fprintf(b, "%s  %s %-18s  used=%3.0f%%  cap=%3.0f%%%s\n",
			indent, marker, windowDisplayName(w.Name), w.UsedPct, w.Capacity*100, resetStr)
	}
}

func renderDiagnosticsText(b *strings.Builder, styles previewStyles, diagnostics *previewDiagnostics, indent string) {
	b.WriteString(indent)
	b.WriteString(styles.Muted.Render("Diagnostics:"))
	b.WriteString("\n")
	if diagnostics.FilteredTask != nil {
		if diagnostics.FilteredTask.Error != "" {
			b.WriteString(indent)
			b.WriteString("  - ")
			b.WriteString(styles.Warn.Render(fmt.Sprintf("Task filter unknown: %s", diagnostics.FilteredTask.Type)))
			b.WriteString("\n")
			renderCooldownsText(b, styles, diagnostics.Cooldowns, indent)
			return
		}
		b.WriteString(indent)
		fmt.Fprintf(b, "  - Filtered to %s (%s), cost %s (%d-%d)\n",
			diagnostics.FilteredTask.Type,
			diagnostics.FilteredTask.Name,
			diagnostics.FilteredTask.CostTier,
			diagnostics.FilteredTask.MinTokens,
			diagnostics.FilteredTask.MaxTokens)
		if diagnostics.FilteredTask.Disabled {
			b.WriteString(indent)
			b.WriteString("  - Task disabled by config\n")
		}
		renderCooldownsText(b, styles, diagnostics.Cooldowns, indent)
		return
	}
	if diagnostics.Aggregate == nil {
		renderCooldownsText(b, styles, diagnostics.Cooldowns, indent)
		return
	}

	agg := diagnostics.Aggregate
	b.WriteString(indent)
	fmt.Fprintf(b, "  - Enabled tasks: %d (disabled: %d)\n", agg.Enabled, agg.Disabled)
	if agg.Assigned > 0 {
		b.WriteString(indent)
		fmt.Fprintf(b, "  - Already assigned: %d\n", agg.Assigned)
	}
	if agg.OnCooldown > 0 {
		b.WriteString(indent)
		fmt.Fprintf(b, "  - On cooldown: %d\n", agg.OnCooldown)
	}
	b.WriteString(indent)
	fmt.Fprintf(b, "  - Candidates after filters: %d\n", agg.Candidates)
	if len(agg.UnknownEnabled) > 0 {
		b.WriteString(indent)
		fmt.Fprintf(b, "  - Unknown enabled task types: %s\n", strings.Join(agg.UnknownEnabled, ", "))
	}
	if agg.NoEnabledTasks {
		b.WriteString(indent)
		b.WriteString("  - No enabled tasks in config\n")
	}
	if agg.Candidates == 0 && agg.OnCooldown > 0 {
		b.WriteString(indent)
		b.WriteString("  - ")
		b.WriteString(styles.Warn.Render("All available tasks are on cooldown"))
		b.WriteString("\n")
	}
	renderCooldownsText(b, styles, diagnostics.Cooldowns, indent)
}

func renderCooldownsText(b *strings.Builder, styles previewStyles, cooldowns []previewCooldownEntry, indent string) {
	if len(cooldowns) == 0 {
		return
	}
	for _, cd := range cooldowns {
		b.WriteString(indent)
		label := "cooldown"
		if cd.Simulated {
			label = "simulated cooldown"
		}
		fmt.Fprintf(b, "  - %s (%s): %s, remaining %s (interval %s)\n",
			cd.TaskName, cd.TaskType, label, cd.Remaining, cd.TotalInterval)
	}
}

func configLoadedLabel(loaded bool) string {
	if loaded {
		return "loaded"
	}
	return "missing"
}

func indentLines(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func writePreviewText(w io.Writer, text string, options previewPagerOptions) error {
	if canUseGumPager(w, options) {
		if gumPath, ok := ensureGum(); ok {
			if err := runGumPager(w, gumPath, text); err == nil {
				return nil
			}
		}
	}

	_, err := io.WriteString(w, text)
	return err
}

func canUseGumPager(w io.Writer, options previewPagerOptions) bool {
	if options.Plain {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	return file.Fd() == os.Stdout.Fd()
}

func ensureGum() (string, bool) {
	if path, err := exec.LookPath("gum"); err == nil {
		return path, true
	}
	if _, err := exec.LookPath("brew"); err == nil {
		cmd := exec.Command("brew", "install", "gum")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		_ = cmd.Run()
		if path, err := exec.LookPath("gum"); err == nil {
			return path, true
		}
	}
	return "", false
}

func runGumPager(w io.Writer, gumPath, text string) error {
	cmd := exec.Command(gumPath, "pager")
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type previewJSON struct {
	GeneratedAt     string                      `json:"generated_at"`
	Provider        string                      `json:"provider"`
	TaskFilter      string                      `json:"task_filter,omitempty"`
	EnabledTasks    []string                    `json:"enabled_tasks,omitempty"`
	Budget          previewJSONBudgetConfig     `json:"budget"`
	Config          previewJSONConfigSources    `json:"config"`
	ProviderBudgets []previewJSONProviderBudget `json:"provider_budgets,omitempty"`
	Runs            []previewJSONRun            `json:"runs"`
	Notes           []string                    `json:"notes,omitempty"`
}

type previewJSONBudgetConfig struct {
	MaxPercent int `json:"max_percent"`
}

type previewJSONConfigSources struct {
	Global  previewJSONConfigSource `json:"global"`
	Project previewJSONConfigSource `json:"project"`
	Order   string                  `json:"order"`
}

type previewJSONConfigSource struct {
	Path   string `json:"path"`
	Loaded bool   `json:"loaded"`
}

type previewJSONProviderBudget struct {
	Provider       string  `json:"provider"`
	HourlyCapacity float64 `json:"hourly_capacity"`
	BottleneckUsed float64 `json:"bottleneck_used_pct"`
	MaxPercent     int     `json:"max_percent"`
	Source         string  `json:"source"`
	Error          string  `json:"error,omitempty"`
}

type previewJSONRun struct {
	Index    int                  `json:"index"`
	RunAt    string               `json:"run_at"`
	Projects []previewJSONProject `json:"projects"`
}

type previewJSONProject struct {
	Path        string              `json:"path"`
	Status      string              `json:"status"`
	Detail      string              `json:"detail,omitempty"`
	Budget      *previewJSONBudget  `json:"budget,omitempty"`
	Tasks       []previewJSONTask   `json:"tasks,omitempty"`
	Diagnostics *previewDiagnostics `json:"diagnostics,omitempty"`
}

type previewJSONBudget struct {
	HourlyCapacity float64 `json:"hourly_capacity"`
	BottleneckUsed float64 `json:"bottleneck_used_pct"`
	MaxPercent     int     `json:"max_percent"`
	Source         string  `json:"source"`
}

type previewJSONTask struct {
	Index           int     `json:"index"`
	Type            string  `json:"type"`
	Name            string  `json:"name"`
	Description     string  `json:"description,omitempty"`
	Score           float64 `json:"score"`
	CostTier        string  `json:"cost_tier"`
	MinTokens       int     `json:"min_tokens"`
	MaxTokens       int     `json:"max_tokens"`
	Prompt          string  `json:"prompt"`
	PromptFile      string  `json:"prompt_file,omitempty"`
	PromptFileError string  `json:"prompt_file_error,omitempty"`
}

func writePreviewJSON(w io.Writer, result *previewResult) error {
	payload := buildPreviewJSON(result)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func buildPreviewJSON(result *previewResult) previewJSON {
	configSources := previewJSONConfigSources{
		Order: "global -> project -> env overrides",
	}
	if result.ConfigSources != nil {
		configSources.Global = previewJSONConfigSource{Path: result.ConfigSources.GlobalPath, Loaded: result.ConfigSources.GlobalExists}
		configSources.Project = previewJSONConfigSource{Path: result.ConfigSources.ProjectPath, Loaded: result.ConfigSources.ProjectExists}
	}

	budgets := make([]previewJSONProviderBudget, 0, len(result.Providers))
	for _, summary := range result.Providers {
		entry := previewJSONProviderBudget{Provider: summary.name}
		if summary.err != nil {
			entry.Error = summary.err.Error()
			budgets = append(budgets, entry)
			continue
		}
		if summary.allowance != nil {
			entry.HourlyCapacity = summary.allowance.HourlyCapacity
			entry.BottleneckUsed = summary.allowance.BottleneckUsedPct
			entry.MaxPercent = summary.allowance.MaxPercent
			entry.Source = summary.allowance.Source
		}
		budgets = append(budgets, entry)
	}

	runs := make([]previewJSONRun, 0, len(result.Runs))
	for _, run := range result.Runs {
		projects := make([]previewJSONProject, 0, len(run.Projects))
		for _, project := range run.Projects {
			var budgetPayload *previewJSONBudget
			if project.Budget != nil {
				budgetPayload = &previewJSONBudget{
					HourlyCapacity: project.Budget.HourlyCapacity,
					BottleneckUsed: project.Budget.BottleneckUsedPct,
					MaxPercent:     project.Budget.MaxPercent,
					Source:         project.Budget.Source,
				}
			}

			tasksPayload := make([]previewJSONTask, 0, len(project.Tasks))
			for _, task := range project.Tasks {
				tasksPayload = append(tasksPayload, previewJSONTask{
					Index:           task.Index,
					Type:            task.Type,
					Name:            task.Name,
					Description:     task.Description,
					Score:           task.Score,
					CostTier:        task.CostTier,
					MinTokens:       task.MinTokens,
					MaxTokens:       task.MaxTokens,
					Prompt:          task.Prompt,
					PromptFile:      task.PromptFile,
					PromptFileError: task.PromptFileError,
				})
			}

			projects = append(projects, previewJSONProject{
				Path:        project.Path,
				Status:      string(project.Status),
				Detail:      project.Detail,
				Budget:      budgetPayload,
				Tasks:       tasksPayload,
				Diagnostics: project.Diagnostics,
			})
		}

		runs = append(runs, previewJSONRun{
			Index:    run.Index,
			RunAt:    run.RunAt.Format(time.RFC3339),
			Projects: projects,
		})
	}

	payload := previewJSON{
		GeneratedAt:  result.GeneratedAt.Format(time.RFC3339),
		Provider:     result.Provider,
		TaskFilter:   result.TaskFilter,
		EnabledTasks: result.EnabledTasks,
		Budget: previewJSONBudgetConfig{
			MaxPercent: result.MaxPercent,
		},
		Config:          configSources,
		ProviderBudgets: budgets,
		Runs:            runs,
	}
	if result.Note != "" {
		payload.Notes = []string{result.Note}
	}

	return payload
}
