package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/cedricfarinazzo/nightshift/internal/config"
	"github.com/cedricfarinazzo/nightshift/internal/db"
	jiraconfig "github.com/cedricfarinazzo/nightshift/internal/jira"
	"github.com/cedricfarinazzo/nightshift/internal/providers"
	"github.com/cedricfarinazzo/nightshift/internal/reporting"
	"github.com/cedricfarinazzo/nightshift/internal/scheduler"
	"github.com/cedricfarinazzo/nightshift/internal/security"
	"github.com/cedricfarinazzo/nightshift/internal/setup"
	"github.com/cedricfarinazzo/nightshift/internal/tasks"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive onboarding wizard",
	Long: `Interactive onboarding wizard that configures Nightshift end-to-end.

Creates/updates the global config, validates providers, previews the next run,
and optionally installs/enables the daemon.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		model, err := newSetupModel()
		if err != nil {
			return err
		}
		_, err = tea.NewProgram(model).Run()
		return err
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

type setupStep int

const (
	stepWelcome setupStep = iota
	stepConfig
	stepProjects
	stepBudget
	stepSafety
	stepModel
	stepTaskPreset
	stepTaskSelect
	stepSchedule
	stepJira
	stepPromptCompression
	stepSystemd
	stepPreview
	stepPath
	stepDaemon
	stepFinish
)

const (
	nightshiftPlanIgnore        = ".nightshift-plan"
	nightshiftPlanIgnoreComment = "# Nightshift plan artifacts (keep out of version control)"
)

type modelOption struct {
	label string
	value string // empty = use CLI default
}

// modelProviderLists holds the model option slice for each provider in cursor order
// (claude=0, codex=1, copilot=2). Used to bound modelCursor in handleModelInput.
var modelProviderLists = []*[]modelOption{&claudeModels, &codexModels, &copilotModels}

// Effort level slices per provider. "default" maps to "" (use CLI default).
var claudeEfforts = []string{"default", "low", "medium", "high", "xhigh", "max"}
var copilotEfforts = []string{"default", "low", "medium", "high", "xhigh"}
var codexEfforts = []string{"default", "none", "minimal", "low", "medium", "high", "xhigh"}

// effortIndex returns the index of the given effort value in an effort slice, defaulting to 0.
func effortIndex(efforts []string, value string) int {
	if value == "" {
		return 0
	}
	for i, e := range efforts {
		if e == value {
			return i
		}
	}
	return 0
}

// effortValue converts a display effort string to a config value ("default" → "").
func effortValue(s string) string {
	if s == "default" {
		return ""
	}
	return s
}

// orDefaultStr returns s if non-empty, otherwise def.
func orDefaultStr(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

// jiraProviders lists providers selectable for Jira phase configuration.
var jiraProviders = []string{"claude", "codex", "copilot"}

// compressionProviders lists providers available for prompt compression.
var compressionProviders = []string{"claude", "codex", "copilot"}

// modelOptionValues extracts the value field from a slice of modelOption.
func modelOptionValues(opts []modelOption) []string {
	vals := make([]string, len(opts))
	for i, o := range opts {
		vals[i] = o.value
	}
	return vals
}

// claudeModels lists available Claude models (static fallback).
// Updated at runtime via FetchAnthropicModels when ANTHROPIC_API_KEY is set.
// Source: https://platform.claude.com/docs/en/about-claude/models/overview
var claudeModels = []modelOption{
	{label: "default", value: ""},
	{label: "claude-opus-4-7", value: "claude-opus-4-7"},
	{label: "claude-sonnet-4-6", value: "claude-sonnet-4-6"},
	{label: "claude-haiku-4-5-20251001", value: "claude-haiku-4-5-20251001"},
	{label: "claude-opus-4-6", value: "claude-opus-4-6"},
	{label: "claude-sonnet-4-5-20250929", value: "claude-sonnet-4-5-20250929"},
	{label: "claude-opus-4-5-20251101", value: "claude-opus-4-5-20251101"},
	{label: "claude-opus-4-1-20250805", value: "claude-opus-4-1-20250805"},
}

// codexModels lists available Codex/GPT models (static fallback).
// Updated at runtime via FetchOpenAIModels when OPENAI_API_KEY is set.
// Source: https://developers.openai.com/api/docs/models
var codexModels = []modelOption{
	{label: "default", value: ""},
	{label: "gpt-5.5", value: "gpt-5.5"},
	{label: "gpt-5.4", value: "gpt-5.4"},
	{label: "gpt-5.4-mini", value: "gpt-5.4-mini"},
	{label: "gpt-5.4-nano", value: "gpt-5.4-nano"},
	{label: "gpt-5.3-codex", value: "gpt-5.3-codex"},
	{label: "gpt-5.2-codex", value: "gpt-5.2-codex"},
	{label: "gpt-5.2", value: "gpt-5.2"},
	{label: "gpt-5-mini", value: "gpt-5-mini"},
	{label: "gpt-4.1", value: "gpt-4.1"},
}

// initCopilotModels generates the copilot model list from providers.CopilotModels(),
// reordering to place sonnet models first (followed by others) for better default selection.
func initCopilotModels() []modelOption {
	opts := []modelOption{{label: "default", value: ""}}
	models := providers.CopilotModels()

	// Partition: sonnet models first, then rest
	var sonnetModels []string
	var otherModels []string

	for _, m := range models {
		if strings.Contains(m, "sonnet") {
			sonnetModels = append(sonnetModels, m)
		} else {
			otherModels = append(otherModels, m)
		}
	}

	// Combine: sonnet first, then others
	for _, m := range sonnetModels {
		opts = append(opts, modelOption{label: m, value: m})
	}
	for _, m := range otherModels {
		opts = append(opts, modelOption{label: m, value: m})
	}

	return opts
}

// copilotModels lists available Copilot models, generated from providers.CopilotModels()
// with sonnet models prioritized as better defaults.
var copilotModels = initCopilotModels()

type setupModel struct {
	step setupStep

	cfg             *config.Config
	configPath      string
	configExist     bool
	includePathStep bool

	projects       []string
	projectCursor  int
	projectInput   textinput.Model
	projectEditing bool
	projectErr     string
	gitignoreAdded int
	gitignoreKept  int
	gitignoreErrs  []string

	budgetCursor  int
	budgetInput   textinput.Model
	budgetEditing bool
	budgetErr     string

	safetyCursor int

	modelCursor     int
	claudeModelIdx  int
	codexModelIdx   int
	copilotModelIdx int
	modelsLoading   int // counts pending dynamic-fetch goroutines (0 = done)

	claudeEffortIdx  int
	codexEffortIdx   int
	copilotEffortIdx int

	taskPresetCursor   int
	taskCursor         int
	taskViewportOffset int
	taskItems          []taskItem
	taskErr            string
	preset             setup.Preset
	windowHeight int

	scheduleMode      string
	scheduleCursor    int
	scheduleInput     textinput.Model
	scheduleEditing   bool
	scheduleStart     string
	scheduleCycles    int
	scheduleInterval  string
	scheduleCron      string
	scheduleErr       string
	scheduleWindowEnd string

	previewRunning bool
	previewOutput  string
	previewErr     error

	pathCursor       int
	pathOptions      []pathOption
	pathErr          string
	pathApplied      bool
	pathStatus       string
	pathShell        string
	pathConfig       string
	pathSourceHint   string
	nightshiftInPath bool

	daemonCursor int
	serviceType  string
	serviceState serviceState
	daemonAction string

	// Jira step state
	jiraSubStep       int
	jiraInput         textinput.Model
	jiraEnableCursor  int
	jiraEnabled       bool
	jiraSite          string
	jiraEmail         string
	jiraTokenEnv      string
	jiraMaxTickets    int
	jiraRepos         []jiraRepoEntry
	jiraRepoCursor    int
	jiraRepoEditing   bool
	jiraRepoField     int
	jiraRepoEditURL   string
	jiraPhaseCursor       int
	jiraPhaseModelIdx     [4]int
	jiraPhaseProvider     [4]string // provider per phase: claude, codex, or copilot
	jiraPhaseEffortIdx    [4]int    // effort index per phase, into provider-specific effort slice
	jiraPhaseTimeout      [4]string // timeout per phase (duration string, e.g. "30m")
	jiraPhaseTimeoutInput textinput.Model
	jiraPhaseTimeoutEdit  bool // true when editing the timeout for the focused phase
	jiraPinging       bool
	jiraPingOK        bool
	jiraPingErr       string
	jiraErr           string

	// Multi-project Jira state
	jiraProjects                    []jiraProjectEntry
	jiraProjectCursor               int
	jiraProjectEditMode             bool
	jiraProjectEditSubStep int // 0=key, 1=label, 2=repos
	jiraEditProjectKey     string
	jiraEditProjectLabel   string

	// Agent timeout (shown in providers/model step, applied to all providers)
	agentTimeout      string          // duration string, e.g. "30m"
	agentTimeoutInput textinput.Model // used when editing timeout (modelCursor==3)
	agentTimeoutEdit  bool            // true while editing timeout

	// Prompt compression step state
	compressionCursor    int    // 0=enable toggle, 1=provider, 2=model, 3=effort
	compressionEnabled   bool
	compressionProvider  string // "claude" or "codex"
	compressionModelIdx  int
	compressionEffortIdx int

	// Systemd step state
	systemdCursor     int // 0=yes, 1=no
	systemdOnCalendar string
	systemdSubStep    int // 0=enable prompt, 1=oncalendar prompt
	systemdInput      textinput.Model
	systemdErr        string

	spinner spinner.Model
}

type taskItem struct {
	def      tasks.TaskDefinition
	selected bool
}

type serviceState struct {
	installed bool
	running   bool
	detail    string
}

type previewMsg struct {
	output string
	err    error
}

type jiraPingMsg struct {
	ok  bool
	err string
}

type anthropicModelsFetchedMsg struct {
	models []string
	err    error
}

type openaiModelsFetchedMsg struct {
	models []string
	err    error
}

type jiraRepoEntry struct {
	URL        string
	BaseBranch string
}

type jiraProjectEntry struct {
	Key   string
	Label string
	Repos []jiraRepoEntry
}

type pathOption struct {
	label   string
	action  pathAction
	dir     string
	install bool
}

type pathAction int

const (
	pathActionSkip pathAction = iota
	pathActionAdd
)

// calculateTaskViewportHeight computes the number of task items to display
// based on terminal height, reserving space for header/footer/indicators.
// Returns at least 5 items even on small terminals.
func (m *setupModel) calculateTaskViewportHeight() int {
	// Reserve space: ~8 lines for header/footer/indicators
	// (title, instruction, above/below indicators, error, footer)
	const headerFooterReserved = 8
	if m.windowHeight > 0 {
		h := m.windowHeight - headerFooterReserved
		if h < 5 {
			h = 5 // minimum visible items
		}
		return h
	}
	// Fallback before first WindowSizeMsg — conservative to avoid initial overflow
	return 10
}

var (
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69"))
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleOk     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleNote   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleAccent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
)

func newSetupModel() (*setupModel, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	// Keep task registry aligned with current config so setup can display custom tasks.
	tasks.ClearCustom()
	if err := tasks.RegisterCustomTasksFromConfig(cfg.Tasks.Custom); err != nil {
		return nil, fmt.Errorf("register custom tasks: %w", err)
	}
	configPath := config.GlobalConfigPath()
	_, err = os.Stat(configPath)
	configExist := err == nil

	projectInput := textinput.New()
	projectInput.Placeholder = "~/code/project"
	projectInput.Prompt = "> "

	budgetInput := textinput.New()
	budgetInput.Prompt = "> "

	scheduleInput := textinput.New()
	scheduleInput.Prompt = "> "

	jiraInput := textinput.New()
	jiraInput.Prompt = "> "

	jiraPhaseTimeoutInput := textinput.New()
	jiraPhaseTimeoutInput.Prompt = "> "
	jiraPhaseTimeoutInput.Placeholder = "30m"

	agentTimeoutInput := textinput.New()
	agentTimeoutInput.Prompt = "> "
	agentTimeoutInput.Placeholder = "30m"

	systemdInput := textinput.New()
	systemdInput.Prompt = "> "

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot

	projects := make([]string, 0, len(cfg.Projects))
	for _, p := range cfg.Projects {
		if p.Path != "" {
			projects = append(projects, p.Path)
		}
	}
	if len(projects) == 0 {
		projects = []string{""}
	}

	preset := setup.PresetBalanced
	taskItems := makeTaskItems(cfg, projects, preset)
	_, err = execLookPath("nightshift")
	nightshiftInPath := err == nil
	includePathStep := !nightshiftInPath

	pendingFetches := 0
	if security.GetAnthropicKey() != "" {
		pendingFetches++
	}
	if security.GetOpenAIKey() != "" {
		pendingFetches++
	}

	model := &setupModel{
		step:              stepWelcome,
		cfg:               cfg,
		configPath:        configPath,
		configExist:       configExist,
		includePathStep:   includePathStep,
		projects:          projects,
		projectInput:      projectInput,
		budgetInput:       budgetInput,
		taskItems:         taskItems,
		preset:            preset,
		scheduleMode:      "interval",
		scheduleStart:     "22:00",
		scheduleCycles:    3,
		scheduleInterval:  "30m",
		scheduleCron:      "0 2 * * *",
		scheduleInput:     scheduleInput,
		spinner:           spin,
		nightshiftInPath:  nightshiftInPath,
		modelsLoading:     pendingFetches,
		claudeModelIdx:    modelIndex(claudeModels, cfg.Providers.Claude.Model),
		codexModelIdx:     modelIndex(codexModels, cfg.Providers.Codex.Model),
		copilotModelIdx:   modelIndex(copilotModels, cfg.Providers.Copilot.Model),
		claudeEffortIdx:   effortIndex(claudeEfforts, cfg.Providers.Claude.ReasoningEffort),
		codexEffortIdx:    effortIndex(codexEfforts, cfg.Providers.Codex.ReasoningEffort),
		copilotEffortIdx:  effortIndex(copilotEfforts, cfg.Providers.Copilot.ReasoningEffort),
		compressionEnabled:   cfg.PromptCompression.Enabled,
		compressionProvider:  func() string {
			if cfg.PromptCompression.Provider != "" {
				return cfg.PromptCompression.Provider
			}
			return "claude"
		}(),
		compressionModelIdx:  0,
		compressionEffortIdx: 0,
		jiraInput:             jiraInput,
		jiraPhaseTimeoutInput: jiraPhaseTimeoutInput,
		jiraTokenEnv:          "JIRA_API_TOKEN",
		agentTimeout:          orDefaultStr(cfg.Providers.Claude.Timeout, "30m"),
		agentTimeoutInput:     agentTimeoutInput,
		systemdInput:          systemdInput,
		systemdOnCalendar: func() string {
			if cfg.Jira.SystemdOnCalendar != "" {
				return cfg.Jira.SystemdOnCalendar
			}
			return "*-*-* 22:00:00"
		}(),
		jiraMaxTickets:    10,
		jiraPhaseProvider: defaultJiraPhaseProviders(cfg.Providers.Preference),
		jiraPhaseModelIdx: defaultJiraPhaseModelIdxs(cfg.Providers.Preference),
		jiraPhaseTimeout:  [4]string{"2m", "5m", "30m", "20m"},
	}

	// Pre-populate compression fields from existing config.
	if cfg.PromptCompression.Enabled {
		models := compressionModelsForProvider(model.compressionProvider)
		model.compressionModelIdx = modelIndex(models, cfg.PromptCompression.Model)
		efforts := compressionEffortsForProvider(model.compressionProvider)
		model.compressionEffortIdx = effortIndex(efforts, cfg.PromptCompression.ReasoningEffort)
	}

	// Pre-populate schedule fields from existing config.
	if cfg.Schedule.Cron != "" {
		model.scheduleMode = "cron"
		model.scheduleCron = cfg.Schedule.Cron
	} else if cfg.Schedule.Interval != "" {
		model.scheduleMode = "interval"
		model.scheduleInterval = cfg.Schedule.Interval
	}
	if cfg.Schedule.Window != nil {
		model.scheduleStart = cfg.Schedule.Window.Start
	}

	// Pre-populate from existing Jira config when re-running wizard.
	if cfg.Jira.Site != "" {
		model.jiraEnabled = true
		model.jiraSite = cfg.Jira.Site
		model.jiraEmail = cfg.Jira.Email
		if cfg.Jira.TokenEnv != "" {
			model.jiraTokenEnv = cfg.Jira.TokenEnv
		}
		if cfg.Jira.MaxTickets > 0 {
			model.jiraMaxTickets = cfg.Jira.MaxTickets
		}
		for _, p := range cfg.Jira.Projects {
			entry := jiraProjectEntry{
				Key:   p.Key,
				Label: p.Label,
			}
			for _, r := range p.Repos {
				entry.Repos = append(entry.Repos, jiraRepoEntry{
					URL:        r.URL,
					BaseBranch: r.BaseBranch,
				})
			}
			model.jiraProjects = append(model.jiraProjects, entry)
		}
		model.jiraPhaseModelIdx = [4]int{
			jiraModelIndexForProvider(model.jiraPhaseProvider[0], cfg.Jira.Validation.Model),
			jiraModelIndexForProvider(model.jiraPhaseProvider[1], cfg.Jira.Plan.Model),
			jiraModelIndexForProvider(model.jiraPhaseProvider[2], cfg.Jira.Implement.Model),
			jiraModelIndexForProvider(model.jiraPhaseProvider[3], cfg.Jira.ReviewFix.Model),
		}
		// Populate per-phase providers from existing config (fall back to claude for old configs).
		for i, phase := range []jiraconfig.PhaseConfig{
			cfg.Jira.Validation, cfg.Jira.Plan, cfg.Jira.Implement, cfg.Jira.ReviewFix,
		} {
			p := phase.Provider
			if p == "" {
				p = "claude"
			}
			model.jiraPhaseProvider[i] = p
			model.jiraPhaseModelIdx[i] = jiraModelIndexForProvider(p, phase.Model)
			model.jiraPhaseEffortIdx[i] = effortIndex(jiraPhaseEffortsForProvider(p), phase.ReasoningEffort)
		}
		defaults := [4]string{"2m", "5m", "30m", "20m"}
		phases := []string{
			cfg.Jira.Validation.Timeout,
			cfg.Jira.Plan.Timeout,
			cfg.Jira.Implement.Timeout,
			cfg.Jira.ReviewFix.Timeout,
		}
		for i, t := range phases {
			model.jiraPhaseTimeout[i] = orDefaultStr(t, defaults[i])
		}
	}

	return model, nil
}

// Init implements tea.Model.
func (m *setupModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}

	if key := security.GetAnthropicKey(); key != "" {
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ids, err := providers.FetchAnthropicModels(ctx, key)
			return anthropicModelsFetchedMsg{models: ids, err: err}
		})
	}

	if key := security.GetOpenAIKey(); key != "" {
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ids, err := providers.FetchOpenAIModels(ctx, key)
			return openaiModelsFetchedMsg{models: ids, err: err}
		})
	}

	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m *setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

		switch m.step {
		case stepWelcome:
			if msg.String() == "enter" {
				return m, m.setStep(stepConfig)
			}
		case stepConfig:
			if msg.String() == "enter" {
				return m, m.setStep(stepProjects)
			}
		case stepProjects:
			return m.handleProjectsInput(msg)
		case stepBudget:
			return m.handleBudgetInput(msg)
		case stepSafety:
			return m.handleSafetyInput(msg)
		case stepModel:
			return m.handleModelInput(msg)
		case stepTaskPreset:
			return m.handlePresetInput(msg)
		case stepTaskSelect:
			return m.handleTaskInput(msg)
		case stepSchedule:
			return m.handleScheduleInput(msg)
		case stepJira:
			return m.handleJiraInput(msg)
		case stepPromptCompression:
			return m.handleCompressionInput(msg)
		case stepSystemd:
			return m.handleSystemdInput(msg)
		case stepPreview:
			if !m.previewRunning && msg.String() == "enter" {
				if m.nightshiftInPath {
					return m, m.setStep(stepDaemon)
				}
				return m, m.setStep(stepPath)
			}
		case stepPath:
			return m.handlePathInput(msg)
		case stepDaemon:
			return m.handleDaemonInput(msg)
		case stepFinish:
			if msg.String() == "enter" {
				return m, tea.Quit
			}
		}
	case previewMsg:
		m.previewRunning = false
		m.previewOutput = msg.output
		m.previewErr = msg.err
	case jiraPingMsg:
		m.jiraPinging = false
		m.jiraPingOK = msg.ok
		m.jiraPingErr = msg.err
	case anthropicModelsFetchedMsg:
		if m.modelsLoading > 0 {
			m.modelsLoading--
		}
		if msg.err == nil && len(msg.models) > 0 {
			opts := []modelOption{{label: "default", value: ""}}
			for _, id := range msg.models {
				opts = append(opts, modelOption{label: id, value: id})
			}
			claudeModels = opts
			m.claudeModelIdx = modelIndex(claudeModels, m.cfg.Providers.Claude.Model)
		}
	case openaiModelsFetchedMsg:
		if m.modelsLoading > 0 {
			m.modelsLoading--
		}
		if msg.err == nil && len(msg.models) > 0 {
			opts := []modelOption{{label: "default", value: ""}}
			for _, id := range msg.models {
				opts = append(opts, modelOption{label: id, value: id})
			}
			codexModels = opts
			m.codexModelIdx = modelIndex(codexModels, m.cfg.Providers.Codex.Model)
		}
	case tea.WindowSizeMsg:
		m.windowHeight = msg.Height
	}

	return m, cmd
}

// View implements tea.Model.
func (m *setupModel) View() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("Nightshift Setup"))
	b.WriteString("\n")
	b.WriteString(styleDim.Render("================"))
	b.WriteString("\n")
	b.WriteString(renderSetupStepper(m))
	b.WriteString("\n\n")

	switch m.step {
	case stepWelcome:
		b.WriteString("This wizard will configure Nightshift end-to-end.\n\n")
		b.WriteString("Checks:\n")
		b.WriteString(renderEnvChecks(m.cfg))
		b.WriteString("\nPress Enter to continue.\n")
	case stepConfig:
		b.WriteString(styleAccent.Render("Global config"))
		b.WriteString("\n")
		fmt.Fprintf(&b, "  %s\n", m.configPath)
		if m.configExist {
			b.WriteString("  Status: found (will update in place)\n")
		} else {
			b.WriteString("  Status: will create\n")
		}
		b.WriteString("\nThis wizard only writes the global config. Per-project configs are optional.\n")
		b.WriteString("\nPress Enter to continue.\n")
	case stepProjects:
		b.WriteString(styleAccent.Render("Projects (global config)"))
		b.WriteString("\n")
		b.WriteString("Use ↑/↓ to navigate, 'a' to add, 'd' to delete.\n")
		if m.projectEditing {
			b.WriteString("\nAdd project path:\n")
			b.WriteString(m.projectInput.View() + "\n")
			if m.projectErr != "" {
				b.WriteString("Error: " + m.projectErr + "\n")
			}
			b.WriteString("\nPress Enter to add or Esc to cancel.\n")
			return b.String()
		}

		for i, project := range m.projects {
			cursor := " "
			if i == m.projectCursor {
				cursor = ">"
			}
			label := project
			if label == "" {
				label = "(unset)"
			}
			fmt.Fprintf(&b, " %s %s\n", cursor, label)
		}
		if m.projectErr != "" {
			b.WriteString("\nError: " + m.projectErr + "\n")
		}
		b.WriteString("\nPress Enter to continue.\n")
	case stepBudget:
		b.WriteString(styleAccent.Render("Budget defaults"))
		b.WriteString("\n")
		b.WriteString("Edit with e.\n")
		b.WriteString("Use ↑/↓ to select a field.\n\n")
		renderBudgetFields(&b, m)
		if m.budgetEditing {
			b.WriteString("\nEdit value:\n")
			b.WriteString(m.budgetInput.View() + "\n")
			if m.budgetErr != "" {
				b.WriteString("Error: " + m.budgetErr + "\n")
			}
			b.WriteString("\nPress Enter to save, Esc to cancel.\n")
			return b.String()
		}
		if m.budgetErr != "" {
			b.WriteString("\nError: " + m.budgetErr + "\n")
		}
		b.WriteString("\nPress Enter to continue.\n")
	case stepSafety:
		b.WriteString(styleAccent.Render("Approvals & sandbox"))
		b.WriteString("\n")
		b.WriteString("These flags reduce interactive prompts. They’re convenient but carry more risk.\n")
		b.WriteString("We default them ON; you can turn them off here.\n\n")
		b.WriteString("Use ↑/↓ to select, space to toggle.\n\n")
		renderSafetyFields(&b, m)
		b.WriteString("\nPress Enter to continue.\n")
	case stepModel:
		b.WriteString(styleAccent.Render("Model selection"))
		b.WriteString("\n")
		b.WriteString("Choose the model for each provider. Use ↑/↓ to select a row, ←/→ to cycle models.\n\n")
		renderModelFields(&b, m)
		b.WriteString("\nPress Enter to continue.\n")
	case stepPromptCompression:
		b.WriteString(styleAccent.Render("Prompt compression"))
		b.WriteString("\n")
		b.WriteString("Compress prompts via LLM before sending to agents (reduces ARG_MAX risk + token cost).\n\n")
		renderCompressionFields(&b, m)
		b.WriteString("\nPress Enter to continue.\n")
	case stepTaskPreset:
		b.WriteString(styleAccent.Render("Task presets (derived from registry)"))
		b.WriteString("\n")
		b.WriteString("Use ↑/↓ to select, Enter to continue.\n\n")
		presets := []setup.Preset{setup.PresetBalanced, setup.PresetSafe, setup.PresetAggressive}
		for i, preset := range presets {
			cursor := " "
			if i == m.taskPresetCursor {
				cursor = ">"
			}
			label := string(preset)
			if preset == setup.PresetBalanced {
				label += " (recommended)"
			}
			fmt.Fprintf(&b, " %s %s\n", cursor, label)
		}
	case stepTaskSelect:
		b.WriteString(styleAccent.Render("Tasks"))
		b.WriteString("\n")
		b.WriteString("Space to toggle, ↑/↓ to move.\n\n")
		if len(m.taskItems) == 0 {
			b.WriteString(styleWarn.Render("No task definitions found."))
			b.WriteString("\n")
		} else {
			vh := m.calculateTaskViewportHeight()
			start := m.taskViewportOffset
			end := start + vh
			if end > len(m.taskItems) {
				end = len(m.taskItems)
			}
			if start > 0 {
				fmt.Fprintf(&b, "  ... (%d more above)\n", start)
			}
			for i := start; i < end; i++ {
				item := m.taskItems[i]
				cursor := " "
				if i == m.taskCursor {
					cursor = ">"
				}
				check := " "
				if item.selected {
					check = "x"
				}
				fmt.Fprintf(&b, " %s [%s] %-22s %s\n", cursor, check, item.def.Type, item.def.Name)
			}
			if end < len(m.taskItems) {
				fmt.Fprintf(&b, "  ... (%d more below)\n", len(m.taskItems)-end)
			}
		}
		if m.taskErr != "" {
			b.WriteString("\nError: " + m.taskErr + "\n")
		}
		b.WriteString("\nPress Enter to continue.\n")
	case stepSchedule:
		b.WriteString(styleAccent.Render("Schedule"))
		b.WriteString("\n")
		b.WriteString("Use ↑/↓ to select, e to edit. We’ll explain each field.\n\n")
		renderScheduleFields(&b, m)
		if help := scheduleFieldHelp(m.scheduleCursor, m.scheduleMode); help != "" {
			b.WriteString("\n")
			b.WriteString(styleNote.Render(help))
			b.WriteString("\n")
		}
		if m.scheduleEditing {
			b.WriteString("\nEdit value:\n")
			b.WriteString(m.scheduleInput.View() + "\n")
			if m.scheduleErr != "" {
				b.WriteString("Error: " + m.scheduleErr + "\n")
			}
			b.WriteString("\nPress Enter to save, Esc to cancel.\n")
			return b.String()
		}
		if m.scheduleErr != "" {
			b.WriteString("\nError: " + m.scheduleErr + "\n")
		}
		b.WriteString("\nPress Enter to continue.\n")
	case stepJira:
		b.WriteString(styleAccent.Render("Jira integration"))
		b.WriteString("\n")
		renderJiraStep(&b, m)
	case stepSystemd:
		b.WriteString(styleAccent.Render("Systemd service (Jira pipeline)"))
		b.WriteString("\n")
		renderSystemdStep(&b, m)
	case stepPreview:
		b.WriteString(styleAccent.Render("Preview step"))
		b.WriteString("\n")
		b.WriteString("Next up: we’ll preview the first scheduled run with a compact task list.\n")
		b.WriteString("Use `nightshift preview --long` later if you want full prompt text.\n\n")
		if m.previewRunning {
			b.WriteString(m.spinner.View() + "\n")
		} else {
			if m.previewErr != nil {
				b.WriteString("Preview error: " + m.previewErr.Error() + "\n")
			} else {
				b.WriteString(m.previewOutput + "\n")
			}
			b.WriteString("\nPress Enter to continue.\n")
		}
	case stepPath:
		b.WriteString(styleAccent.Render("Add Nightshift to PATH"))
		b.WriteString("\n")
		if m.nightshiftInPath {
			b.WriteString("Nightshift is already available in PATH.\n\n")
			b.WriteString("Press Enter to continue.\n")
			break
		}
		b.WriteString("Nightshift isn’t in PATH yet. The daemon and CLI shortcuts need it there.\n")
		if m.pathShell != "" && m.pathConfig != "" {
			fmt.Fprintf(&b, "Shell: %s\n", m.pathShell)
			fmt.Fprintf(&b, "Config: %s\n", m.pathConfig)
		}
		b.WriteString("\nSelect action:\n")
		for i, option := range m.pathOptions {
			cursor := " "
			if i == m.pathCursor {
				cursor = ">"
			}
			fmt.Fprintf(&b, " %s %s\n", cursor, option.label)
		}
		if m.pathErr != "" {
			b.WriteString("\nError: " + m.pathErr + "\n")
		}
		if m.pathStatus != "" {
			b.WriteString("\n" + m.pathStatus + "\n")
			if m.pathSourceHint != "" {
				b.WriteString("Run: " + m.pathSourceHint + "\n")
			}
		}
		if m.pathApplied {
			b.WriteString("\nPress Enter to continue.\n")
		} else {
			b.WriteString("\nPress Enter to apply.\n")
		}
	case stepDaemon:
		b.WriteString(styleAccent.Render("Daemon setup"))
		b.WriteString("\n\n")
		fmt.Fprintf(&b, "Service: %s\n", m.serviceType)
		if m.serviceState.installed {
			b.WriteString("Status: installed\n")
		} else {
			b.WriteString("Status: not installed\n")
		}
		if m.serviceState.running {
			b.WriteString("Daemon: running\n")
		} else {
			b.WriteString("Daemon: stopped\n")
		}
		b.WriteString("\nSelect action:\n")
		for i, label := range m.daemonOptions() {
			cursor := " "
			if i == m.daemonCursor {
				cursor = ">"
			}
			fmt.Fprintf(&b, " %s %s\n", cursor, label)
		}
		b.WriteString("\nPress Enter to apply.\n")
	case stepFinish:
		b.WriteString(styleAccent.Render("Setup complete"))
		b.WriteString("\n")
		b.WriteString(m.finishSummaryLine())
		b.WriteString("\n\n")
		if status := m.finishDaemonStatus(); status != "" {
			b.WriteString(status + "\n\n")
		}
		b.WriteString("What to expect:\n")
		for _, line := range m.finishExpectations() {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("\nPress Enter to exit.\n")
	}

	return b.String()
}

func (m *setupModel) setStep(step setupStep) tea.Cmd {
	m.step = step
	switch step {
	case stepTaskSelect:
		m.taskViewportOffset = 0
	case stepJira:
		m.jiraSubStep = 0
		m.jiraErr = ""
		m.jiraInput.SetValue("")
		m.jiraInput.Blur()
		m.jiraProjectCursor = 0
		m.jiraProjectEditMode = false
		m.jiraProjectEditSubStep = 0
	case stepSystemd:
		m.systemdSubStep = 0
		m.systemdErr = ""
		m.systemdCursor = 0
		m.systemdInput.SetValue("")
		m.systemdInput.Blur()
	case stepPreview:
		m.previewRunning = true
		m.previewOutput = ""
		m.previewErr = nil
		return runPreviewCmd(m.cfg, m.projects)
	case stepPath:
		m.preparePathStep()
	case stepDaemon:
		m.serviceType, m.serviceState = detectServiceState()
	}
	return nil
}

func (m *setupModel) preparePathStep() {
	m.pathErr = ""
	m.pathStatus = ""
	m.pathApplied = false
	m.pathCursor = 0

	if m.nightshiftInPath {
		m.pathOptions = nil
		return
	}

	shellName, configPath := detectShellConfig()
	m.pathShell = shellName
	m.pathConfig = configPath
	m.pathSourceHint = sourceHint(shellName, configPath)

	exeDir := filepath.Dir(mustExecutablePath())
	exeDir = expandPath(exeDir)
	if absDir, err := filepath.Abs(exeDir); err == nil {
		exeDir = absDir
	}

	goBinDir, goOK := detectGoBinDir()
	if goOK {
		goBinDir = expandPath(goBinDir)
		if absDir, err := filepath.Abs(goBinDir); err == nil {
			goBinDir = absDir
		}
	}

	home, _ := os.UserHomeDir()
	localBinDir := filepath.Join(home, ".local", "bin")
	if absDir, err := filepath.Abs(localBinDir); err == nil {
		localBinDir = absDir
	}

	var options []pathOption
	if goOK && goBinDir != "" {
		options = append(options, pathOption{
			label:   fmt.Sprintf("Install to %s and add to PATH (recommended)", goBinDir),
			action:  pathActionAdd,
			dir:     goBinDir,
			install: true,
		})
	} else {
		options = append(options, pathOption{
			label:   fmt.Sprintf("Install to %s and add to PATH", localBinDir),
			action:  pathActionAdd,
			dir:     localBinDir,
			install: true,
		})
	}

	if exeDir != "" && exeDir != goBinDir && exeDir != localBinDir {
		options = append(options, pathOption{
			label:   fmt.Sprintf("Add current binary dir to PATH (%s)", exeDir),
			action:  pathActionAdd,
			dir:     exeDir,
			install: false,
		})
	}

	options = append(options, pathOption{
		label:  "Skip (I'll handle PATH myself)",
		action: pathActionSkip,
	})

	m.pathOptions = options
}

func (m *setupModel) handleProjectsInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.projectEditing {
		switch msg.String() {
		case "enter":
			value := strings.TrimSpace(m.projectInput.Value())
			if value == "" {
				m.projectErr = "path cannot be empty"
				return m, nil
			}
			path := expandPath(value)
			info, err := os.Stat(path)
			if err != nil {
				m.projectErr = "path not found"
				return m, nil
			}
			if !info.IsDir() {
				m.projectErr = "path must be a directory"
				return m, nil
			}
			m.projects = append(m.projects, value)
			m.projectInput.SetValue("")
			m.projectErr = ""
			m.projectEditing = false
			return m, nil
		case "esc":
			m.projectEditing = false
			m.projectErr = ""
			return m, nil
		}
		var cmd tea.Cmd
		m.projectInput, cmd = m.projectInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.projectCursor > 0 {
			m.projectCursor--
		}
	case "down", "j":
		if m.projectCursor < len(m.projects)-1 {
			m.projectCursor++
		}
	case "a":
		m.projectEditing = true
		m.projectInput.Focus()
	case "d":
		if len(m.projects) > 0 {
			m.projects = append(m.projects[:m.projectCursor], m.projects[m.projectCursor+1:]...)
			if m.projectCursor >= len(m.projects) && m.projectCursor > 0 {
				m.projectCursor--
			}
		}
	case "enter":
		if len(m.projects) == 0 {
			m.projectErr = "add at least one project"
			return m, nil
		}
		m.projectErr = ""
		m.applyProjects()
		return m, m.setStep(stepBudget)
	}

	return m, nil
}

func (m *setupModel) handleBudgetInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.budgetEditing {
		switch msg.String() {
		case "enter":
			if err := m.applyBudgetEdit(); err != nil {
				m.budgetErr = err.Error()
				return m, nil
			}
			m.budgetEditing = false
			m.budgetErr = ""
			return m, nil
		case "esc":
			m.budgetEditing = false
			m.budgetErr = ""
			return m, nil
		}
		var cmd tea.Cmd
		m.budgetInput, cmd = m.budgetInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.budgetCursor > 0 {
			m.budgetCursor--
		}
	case "down", "j":
		if m.budgetCursor < 1 {
			m.budgetCursor++
		}
	case "e":
		m.budgetEditing = true
		m.budgetInput.SetValue(m.budgetFieldValue())
		m.budgetInput.Focus()
	case "enter":
		m.applyBudgetDefaults()
		return m, m.setStep(stepSafety)
	}
	return m, nil
}

func (m *setupModel) handlePresetInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.taskPresetCursor > 0 {
			m.taskPresetCursor--
		}
	case "down", "j":
		if m.taskPresetCursor < 2 {
			m.taskPresetCursor++
		}
	case "enter":
		presets := []setup.Preset{setup.PresetBalanced, setup.PresetSafe, setup.PresetAggressive}
		m.preset = presets[m.taskPresetCursor]
		m.taskItems = makeTaskItems(m.cfg, m.projects, m.preset)
		m.taskViewportOffset = 0
		return m, m.setStep(stepTaskSelect)
	}
	return m, nil
}

func (m *setupModel) handleSafetyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.safetyCursor > 0 {
			m.safetyCursor--
		}
	case "down", "j":
		if m.safetyCursor < 2 {
			m.safetyCursor++
		}
	case " ":
		switch m.safetyCursor {
		case 0:
			m.cfg.Providers.Claude.DangerouslySkipPermissions = !m.cfg.Providers.Claude.DangerouslySkipPermissions
		case 1:
			m.cfg.Providers.Codex.DangerouslyBypassApprovalsAndSandbox = !m.cfg.Providers.Codex.DangerouslyBypassApprovalsAndSandbox
		case 2:
			m.cfg.Providers.Copilot.DangerouslySkipPermissions = !m.cfg.Providers.Copilot.DangerouslySkipPermissions
		}
	case "enter":
		return m, m.setStep(stepModel)
	}
	return m, nil
}

func (m *setupModel) handleTaskInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.taskItems) == 0 {
		if msg.String() == "enter" {
			m.taskErr = "no task definitions available"
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.taskCursor > 0 {
			m.taskCursor--
		}
	case "down", "j":
		if m.taskCursor < len(m.taskItems)-1 {
			m.taskCursor++
		}
	case " ":
		m.taskItems[m.taskCursor].selected = !m.taskItems[m.taskCursor].selected
		m.taskErr = ""
	case "enter":
		if !m.hasSelectedTasks() {
			m.taskErr = "select at least one task"
			return m, nil
		}
		m.applyTasks()
		m.taskErr = ""
		return m, m.setStep(stepSchedule)
	}
	// Clamp viewport so cursor stays visible.
	vh := m.calculateTaskViewportHeight()
	if m.taskCursor < m.taskViewportOffset {
		m.taskViewportOffset = m.taskCursor
	}
	if m.taskCursor >= m.taskViewportOffset+vh {
		m.taskViewportOffset = m.taskCursor - vh + 1
	}
	return m, nil
}

func (m *setupModel) handleScheduleInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.scheduleEditing {
		switch msg.String() {
		case "enter":
			if err := m.applyScheduleEdit(); err != nil {
				m.scheduleErr = err.Error()
				return m, nil
			}
			m.scheduleEditing = false
			m.scheduleErr = ""
			return m, nil
		case "esc":
			m.scheduleEditing = false
			m.scheduleErr = ""
			return m, nil
		}
		var cmd tea.Cmd
		m.scheduleInput, cmd = m.scheduleInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.scheduleCursor > 0 {
			m.scheduleCursor--
		}
	case "down", "j":
		if m.scheduleCursor < 4 {
			m.scheduleCursor++
		}
	case "e":
		m.scheduleEditing = true
		m.scheduleInput.SetValue(m.scheduleFieldValue())
		m.scheduleInput.Focus()
	case "enter":
		m.applyScheduleDefaults()
		if err := writeGlobalConfigToPath(m.cfg, m.configPath); err != nil {
			m.scheduleErr = err.Error()
			return m, nil
		}
		return m, m.setStep(stepJira)
	}
	return m, nil
}

func (m *setupModel) handlePathInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pathApplied || m.nightshiftInPath {
		if msg.String() == "enter" {
			return m, m.setStep(stepDaemon)
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.pathCursor > 0 {
			m.pathCursor--
		}
	case "down", "j":
		if m.pathCursor < len(m.pathOptions)-1 {
			m.pathCursor++
		}
	case "enter":
		if len(m.pathOptions) == 0 {
			m.pathErr = "no PATH options available"
			return m, nil
		}
		option := m.pathOptions[m.pathCursor]
		m.pathErr = ""
		m.pathStatus = ""
		if option.action == pathActionSkip {
			m.pathApplied = true
			m.pathStatus = "Skipped PATH update."
			return m, nil
		}
		if err := m.applyPathOption(option); err != nil {
			m.pathErr = err.Error()
			return m, nil
		}
		m.pathApplied = true
		m.nightshiftInPath = true
		return m, nil
	}
	return m, nil
}

func (m *setupModel) handleDaemonInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.daemonCursor > 0 {
			m.daemonCursor--
		}
	case "down", "j":
		if m.daemonCursor < len(m.daemonOptions())-1 {
			m.daemonCursor++
		}
	case "enter":
		action := m.daemonOptions()[m.daemonCursor]
		if err := m.applyDaemonAction(action); err != nil {
			m.serviceState.detail = err.Error()
			return m, nil
		}
		m.daemonAction = action
		return m, m.setStep(stepFinish)
	}
	return m, nil
}

func (m *setupModel) applyProjects() {
	m.cfg.Projects = nil
	for _, project := range m.projects {
		project = strings.TrimSpace(project)
		if project == "" {
			continue
		}
		m.cfg.Projects = append(m.cfg.Projects, config.ProjectConfig{Path: project})
	}
	m.updateProjectGitignores()
}

func (m *setupModel) applyBudgetDefaults() {
	if m.cfg.Budget.MaxPercent == 0 {
		m.cfg.Budget.MaxPercent = config.DefaultMaxPercent
	}
}

func (m *setupModel) budgetFieldValue() string {
	switch m.budgetCursor {
	case 0:
		return strconv.Itoa(m.cfg.Budget.MaxPercent)
	default:
		return ""
	}
}

func (m *setupModel) applyBudgetEdit() error {
	value := strings.TrimSpace(m.budgetInput.Value())
	switch m.budgetCursor {
	case 0:
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 || v > 100 {
			return fmt.Errorf("max_percent must be between 1 and 100")
		}
		m.cfg.Budget.MaxPercent = v
	}
	return nil
}

func (m *setupModel) applyTasks() {
	selected := make([]string, 0)
	for _, item := range m.taskItems {
		if item.selected {
			selected = append(selected, string(item.def.Type))
		}
	}
	m.cfg.Tasks.Enabled = selected
}

func (m *setupModel) hasSelectedTasks() bool {
	for _, item := range m.taskItems {
		if item.selected {
			return true
		}
	}
	return false
}

func (m *setupModel) scheduleFieldValue() string {
	switch m.scheduleCursor {
	case 0:
		return m.scheduleStart
	case 1:
		return strconv.Itoa(m.scheduleCycles)
	case 2:
		return m.scheduleInterval
	case 3:
		return m.scheduleMode
	case 4:
		return m.scheduleCron
	default:
		return ""
	}
}

func (m *setupModel) applyScheduleEdit() error {
	value := strings.TrimSpace(m.scheduleInput.Value())
	switch m.scheduleCursor {
	case 0:
		if _, err := scheduler.ParseTimeOfDay(value); err != nil {
			return err
		}
		m.scheduleStart = value
	case 1:
		v, err := strconv.Atoi(value)
		if err != nil || v <= 0 {
			return fmt.Errorf("cycles must be positive")
		}
		m.scheduleCycles = v
	case 2:
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("interval must be duration (e.g., 30m)")
		}
		m.scheduleInterval = value
	case 3:
		if value != "interval" && value != "cron" {
			return fmt.Errorf("mode must be interval or cron")
		}
		m.scheduleMode = value
	case 4:
		test := scheduler.New()
		if err := test.SetCron(value); err != nil {
			return err
		}
		m.scheduleCron = value
	}
	return nil
}

func (m *setupModel) applyScheduleDefaults() {
	m.cfg.Schedule = config.ScheduleConfig{}
	if m.scheduleMode == "cron" {
		m.cfg.Schedule.Cron = m.scheduleCron
		return
	}

	m.cfg.Schedule.Interval = m.scheduleInterval
	start, _ := scheduler.ParseTimeOfDay(m.scheduleStart)
	interval, _ := time.ParseDuration(m.scheduleInterval)
	end := computeWindowEnd(start, interval, m.scheduleCycles)
	m.scheduleWindowEnd = end.String()
	m.cfg.Schedule.Window = &config.WindowConfig{
		Start:    m.scheduleStart,
		End:      end.String(),
		Timezone: "",
	}
}

func (m *setupModel) daemonOptions() []string {
	if !m.serviceState.installed {
		return []string{"Install and enable daemon", "Skip"}
	}
	return []string{"Start daemon", "Stop daemon", "Remove service", "Leave as-is"}
}

func (m *setupModel) applyDaemonAction(action string) error {
	switch action {
	case "Install and enable daemon":
		if err := installService(m.serviceType, m.cfg); err != nil {
			return err
		}
		// Non-fatal: service is installed; start failure (e.g. systemd not reloaded yet) should not block wizard.
		_ = runDaemonStart(nil, nil)
		return nil
	case "Start daemon":
		return runDaemonStart(nil, nil)
	case "Stop daemon":
		return runDaemonStop(nil, nil)
	case "Remove service":
		_ = runDaemonStop(nil, nil) // ignore if not running
		return uninstallService(m.serviceType)
	default:
		return nil
	}
}

func (m *setupModel) finishSummaryLine() string {
	switch m.daemonAction {
	case "Stop daemon", "Remove service", "Skip":
		return "Nightshift is configured, but the daemon is not running."
	case "Leave as-is":
		if m.serviceState.running {
			return "Nightshift is configured and the daemon is running."
		}
		if m.serviceState.installed {
			return "Nightshift is configured, but the daemon is stopped."
		}
		return "Nightshift is configured, but no daemon service is installed."
	default:
		return "Nightshift is configured and ready to run."
	}
}

func (m *setupModel) finishDaemonStatus() string {
	switch m.daemonAction {
	case "Install and enable daemon":
		return "Daemon status: installed and started."
	case "Start daemon":
		return "Daemon status: started."
	case "Stop daemon":
		return "Daemon status: stopped."
	case "Remove service":
		return "Daemon status: service removed."
	case "Skip":
		return "Daemon status: not installed."
	case "Leave as-is":
		if m.serviceState.running {
			return "Daemon status: running (unchanged)."
		}
		if m.serviceState.installed {
			return "Daemon status: installed but stopped (unchanged)."
		}
		return "Daemon status: not installed."
	default:
		return ""
	}
}

func (m *setupModel) finishExpectations() []string {
	lines := []string{
		fmt.Sprintf("Summary report: %s", reporting.DefaultSummaryPath(time.Now())),
		fmt.Sprintf("Run report: %s", reporting.DefaultRunReportPath(time.Now())),
		"CLI status: `nightshift status --today` or `nightshift logs`",
		"Safety: Nightshift never writes to your primary branch. Expect PRs or branches.",
	}
	if m.gitignoreAdded > 0 || m.gitignoreKept > 0 {
		var parts []string
		if m.gitignoreAdded > 0 {
			parts = append(parts, fmt.Sprintf("added to %d project(s)", m.gitignoreAdded))
		}
		if m.gitignoreKept > 0 {
			parts = append(parts, fmt.Sprintf("already present in %d project(s)", m.gitignoreKept))
		}
		lines = append(lines, fmt.Sprintf("Gitignore: ensured `%s` is ignored (%s) so plan artifacts stay out of version control.", nightshiftPlanIgnore, strings.Join(parts, ", ")))
	}
	for _, errLine := range m.gitignoreErrs {
		lines = append(lines, fmt.Sprintf("Gitignore: %s", errLine))
	}

	switch m.daemonAction {
	case "Stop daemon", "Remove service", "Skip":
		lines = append([]string{
			"Nightshift will not run automatically until the daemon is started.",
			"Run manually: `nightshift run`.",
			"Start the daemon later: `nightshift daemon start` (or re-run setup to install a service).",
		}, lines...)
	case "Leave as-is":
		if !m.serviceState.running {
			lines = append([]string{
				"Nightshift will not run automatically until the daemon is started.",
				"Run manually: `nightshift run`.",
				"Start the daemon later: `nightshift daemon start` (or re-run setup to install a service).",
			}, lines...)
		}
	}

	return lines
}

func (m *setupModel) applyPathOption(option pathOption) error {
	if option.dir == "" {
		return fmt.Errorf("missing target path")
	}

	var statusParts []string
	if option.install {
		dest, err := installNightshiftBinary(option.dir)
		if err != nil {
			return err
		}
		statusParts = append(statusParts, fmt.Sprintf("Installed binary to %s.", dest))
	}

	changed, err := ensurePathInShell(m.pathConfig, m.pathShell, option.dir)
	if err != nil {
		return err
	}
	if changed {
		statusParts = append(statusParts, fmt.Sprintf("Added %s to PATH in %s.", option.dir, m.pathConfig))
	} else {
		statusParts = append(statusParts, fmt.Sprintf("%s already present in %s.", option.dir, m.pathConfig))
	}

	m.pathStatus = strings.Join(statusParts, " ")
	return nil
}

func detectShellConfig() (string, string) {
	shell := filepath.Base(os.Getenv("SHELL"))
	home, _ := os.UserHomeDir()
	switch shell {
	case "zsh":
		return "zsh", filepath.Join(home, ".zshrc")
	case "bash":
		bashProfile := filepath.Join(home, ".bash_profile")
		if _, err := os.Stat(bashProfile); err == nil {
			return "bash", bashProfile
		}
		return "bash", filepath.Join(home, ".bashrc")
	case "fish":
		return "fish", filepath.Join(home, ".config", "fish", "config.fish")
	default:
		if shell == "" {
			shell = "sh"
		}
		return shell, filepath.Join(home, ".profile")
	}
}

func detectGoBinDir() (string, bool) {
	if _, err := exec.LookPath("go"); err != nil {
		return "", false
	}
	out, err := exec.Command("go", "env", "GOBIN").Output()
	if err == nil {
		gobin := strings.TrimSpace(string(out))
		if gobin != "" {
			return gobin, true
		}
	}
	out, err = exec.Command("go", "env", "GOPATH").Output()
	if err != nil {
		return "", false
	}
	gopath := strings.TrimSpace(string(out))
	if gopath == "" {
		return "", false
	}
	if strings.Contains(gopath, string(os.PathListSeparator)) {
		gopath = strings.Split(gopath, string(os.PathListSeparator))[0]
	}
	return filepath.Join(gopath, "bin"), true
}

func sourceHint(shell, configPath string) string {
	if configPath == "" {
		return ""
	}
	if shell == "fish" {
		return fmt.Sprintf("source %s", configPath)
	}
	return fmt.Sprintf("source %s", configPath)
}

func ensurePathInShell(configPath, shell, dir string) (bool, error) {
	if configPath == "" {
		return false, fmt.Errorf("missing shell config path")
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return false, err
	}

	dir = expandPath(dir)
	line := pathExportLine(shell, dir)

	var existing string
	if data, err := os.ReadFile(configPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return false, err
	}

	if shellConfigHasPath(existing, dir) {
		return false, nil
	}

	if len(existing) > 0 && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	existing += line + "\n"
	return true, os.WriteFile(configPath, []byte(existing), 0644)
}

// escapeShellPath returns a shell-safe escaped version of a path string.
// Wraps the path in single quotes to prevent interpretation of special characters.
func escapeShellPath(path string) string {
	// Single quotes prevent all expansions in shell, safest approach
	// If the path contains a single quote, we need to escape it carefully
	if !strings.Contains(path, "'") {
		return fmt.Sprintf("'%s'", path)
	}
	// Path contains single quote: use double quotes and escape special chars
	// Replace $ and ` and " with escaped versions
	escaped := strings.ReplaceAll(path, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	escaped = strings.ReplaceAll(escaped, "$", "\\$")
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	return fmt.Sprintf("\"%s\"", escaped)
}

func pathExportLine(shell, dir string) string {
	// SECURITY: Escape the directory path to prevent shell injection
	escapedDir := escapeShellPath(dir)
	switch shell {
	case "fish":
		return fmt.Sprintf("set -gx PATH %s $PATH", escapedDir)
	default:
		return fmt.Sprintf("export PATH=\"$PATH:%s\"", escapedDir)
	}
}

func shellConfigHasPath(content, dir string) bool {
	target := filepath.Clean(dir)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.Contains(trimmed, "PATH") {
			continue
		}
		if containsPathToken(trimmed, target) {
			return true
		}
	}
	return false
}

func containsPathToken(line, target string) bool {
	tokens := strings.FieldsFunc(line, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}
		switch r {
		case ':', ';', '"', '\'', '=', '$', '{', '}', '(', ')':
			return true
		default:
			return false
		}
	})
	for _, token := range tokens {
		if filepath.Clean(token) == target {
			return true
		}
	}
	return false
}

func installNightshiftBinary(targetDir string) (string, error) {
	exePath := mustExecutablePath()
	if exePath == "" {
		return "", fmt.Errorf("unable to locate nightshift binary")
	}

	targetDir = expandPath(targetDir)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", err
	}

	dest := filepath.Join(targetDir, "nightshift")
	if samePath(exePath, dest) {
		return dest, nil
	}

	if err := copyFile(exePath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func samePath(a, b string) bool {
	aa, errA := filepath.EvalSymlinks(a)
	bb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return aa == bb
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return os.Chmod(dst, 0755)
}

func renderEnvChecks(cfg *config.Config) string {
	var b strings.Builder
	if _, err := execLookPath("nightshift"); err != nil {
		fmt.Fprintf(&b, "  %s %s\n", styleWarn.Render("Heads up:"), "nightshift not found in PATH yet. Setup can add it for you.")
	} else {
		fmt.Fprintf(&b, "  %s %s\n", styleOk.Render("OK:"), "nightshift is in PATH")
	}
	// Check for Copilot CLI (gh or copilot binary)
	_, ghErr := execLookPath("gh")
	_, copilotErr := execLookPath("copilot")
	if ghErr != nil && copilotErr != nil {
		fmt.Fprintf(&b, "  %s %s\n", styleWarn.Render("Note:"), "Copilot CLI not found (install via 'gh' or native 'copilot')")
	} else if ghErr == nil {
		fmt.Fprintf(&b, "  %s %s\n", styleOk.Render("OK:"), "gh CLI available (use 'gh copilot')")
	} else {
		fmt.Fprintf(&b, "  %s %s\n", styleOk.Render("OK:"), "copilot CLI available")
	}
	if cfg.Providers.Claude.Enabled {
		if _, err := os.Stat(cfg.ExpandedProviderPath("claude")); err != nil {
			fmt.Fprintf(&b, "  %s %s\n", styleWarn.Render("Note:"), "Claude data path not found")
		} else {
			fmt.Fprintf(&b, "  %s %s\n", styleOk.Render("OK:"), "Claude data path found")
		}
	}
	if cfg.Providers.Codex.Enabled {
		if _, err := os.Stat(cfg.ExpandedProviderPath("codex")); err != nil {
			fmt.Fprintf(&b, "  %s %s\n", styleWarn.Render("Note:"), "Codex data path not found")
		} else {
			fmt.Fprintf(&b, "  %s %s\n", styleOk.Render("OK:"), "Codex data path found")
		}
	}
	return b.String()
}

func renderBudgetFields(b *strings.Builder, m *setupModel) {
	fields := []string{
		fmt.Sprintf("Max percent: %d", m.cfg.Budget.MaxPercent),
	}
	for i, field := range fields {
		cursor := " "
		if i == m.budgetCursor {
			cursor = ">"
		}
		fmt.Fprintf(b, " %s %s\n", cursor, field)
	}
}

func renderSafetyFields(b *strings.Builder, m *setupModel) {
	items := []struct {
		label     string
		enabled   bool
		available bool
	}{
		{
			label:     "Claude: --dangerously-skip-permissions",
			enabled:   m.cfg.Providers.Claude.DangerouslySkipPermissions,
			available: m.cfg.Providers.Claude.Enabled,
		},
		{
			label:     "Codex:  --dangerously-bypass-approvals-and-sandbox",
			enabled:   m.cfg.Providers.Codex.DangerouslyBypassApprovalsAndSandbox,
			available: m.cfg.Providers.Codex.Enabled,
		},
		{
			label:     "Copilot: --allow-all-tools --allow-all-urls",
			enabled:   m.cfg.Providers.Copilot.DangerouslySkipPermissions,
			available: m.cfg.Providers.Copilot.Enabled,
		},
	}

	for i, item := range items {
		cursor := " "
		if i == m.safetyCursor {
			cursor = ">"
		}
		state := "OFF"
		if item.enabled {
			state = "ON"
		}
		status := state
		if !item.available {
			status = fmt.Sprintf("%s (provider disabled)", state)
		}
		fmt.Fprintf(b, " %s [%s] %s\n", cursor, status, item.label)
	}
	b.WriteString(styleNote.Render("Tip: leave these OFF if you want the CLI to ask for approvals."))
	b.WriteString("\n")
}

func renderModelFields(b *strings.Builder, m *setupModel) {
	rows := []struct {
		label     string
		models    []modelOption
		modelIdx  int
		efforts   []string
		effortIdx int
		available bool
	}{
		{"Claude ", claudeModels, m.claudeModelIdx, claudeEfforts, m.claudeEffortIdx, m.cfg.Providers.Claude.Enabled},
		{"Codex  ", codexModels, m.codexModelIdx, codexEfforts, m.codexEffortIdx, m.cfg.Providers.Codex.Enabled},
		{"Copilot", copilotModels, m.copilotModelIdx, copilotEfforts, m.copilotEffortIdx, m.cfg.Providers.Copilot.Enabled},
	}
	for i, row := range rows {
		cursor := " "
		if i == m.modelCursor {
			cursor = ">"
		}
		avail := ""
		if !row.available {
			avail = " (provider disabled)"
		}
		fmt.Fprintf(b, " %s %s  Model: ← %s →   Effort: ← %s →%s\n",
			cursor, row.label,
			row.models[row.modelIdx].label,
			row.efforts[row.effortIdx],
			avail,
		)
	}
	// Timeout row (cursor index 3)
	cursor := " "
	if m.modelCursor == 3 {
		cursor = ">"
	}
	timeout := m.agentTimeout
	if timeout == "" {
		timeout = "30m"
	}
	if m.agentTimeoutEdit {
		fmt.Fprintf(b, " %s Timeout  [%s]\n", cursor, m.agentTimeoutInput.View())
	} else {
		fmt.Fprintf(b, " %s Timeout  %s  (press [t] to edit)\n", cursor, timeout)
	}
	b.WriteString(styleNote.Render("Tip: ←/→ model  [e] cycle effort  [t] set timeout  'default' = CLI built-in."))
	b.WriteString("\n")
	if m.modelsLoading > 0 {
		b.WriteString(styleDim.Render(m.spinner.View() + " Fetching live model list…"))
		b.WriteString("\n")
	}
}

func (m *setupModel) handleModelInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle timeout editing sub-state when cursor is on row 3.
	if m.agentTimeoutEdit {
		switch msg.String() {
		case "enter":
			val := strings.TrimSpace(m.agentTimeoutInput.Value())
			if val != "" {
				if _, err := time.ParseDuration(val); err != nil {
					// Leave edit open; user must correct value or press esc.
					return m, nil
				}
				m.agentTimeout = val
			}
			m.agentTimeoutEdit = false
			m.agentTimeoutInput.Blur()
		case "esc":
			m.agentTimeoutEdit = false
			m.agentTimeoutInput.Blur()
		default:
			var cmd tea.Cmd
			m.agentTimeoutInput, cmd = m.agentTimeoutInput.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.modelCursor > 0 {
			m.modelCursor--
		}
	case "down", "j":
		// 3 provider rows (0-2) + 1 timeout row (3)
		if m.modelCursor < len(modelProviderLists) {
			m.modelCursor++
		}
	case "left", "h":
		switch m.modelCursor {
		case 0:
			if m.claudeModelIdx > 0 {
				m.claudeModelIdx--
			}
		case 1:
			if m.codexModelIdx > 0 {
				m.codexModelIdx--
			}
		case 2:
			if m.copilotModelIdx > 0 {
				m.copilotModelIdx--
			}
		}
	case "right", "l":
		switch m.modelCursor {
		case 0:
			if m.claudeModelIdx < len(claudeModels)-1 {
				m.claudeModelIdx++
			}
		case 1:
			if m.codexModelIdx < len(codexModels)-1 {
				m.codexModelIdx++
			}
		case 2:
			if m.copilotModelIdx < len(copilotModels)-1 {
				m.copilotModelIdx++
			}
		}
	case "e":
		// Cycle effort forward for the focused provider row
		switch m.modelCursor {
		case 0:
			m.claudeEffortIdx = (m.claudeEffortIdx + 1) % len(claudeEfforts)
		case 1:
			m.codexEffortIdx = (m.codexEffortIdx + 1) % len(codexEfforts)
		case 2:
			m.copilotEffortIdx = (m.copilotEffortIdx + 1) % len(copilotEfforts)
		}
	case "t":
		// Open timeout editor when on the timeout row (cursor 3).
		if m.modelCursor == 3 {
			m.agentTimeoutEdit = true
			m.agentTimeoutInput.SetValue(m.agentTimeout)
			m.agentTimeoutInput.Focus()
		}
	case "enter":
		m.cfg.Providers.Claude.Model = claudeModels[m.claudeModelIdx].value
		m.cfg.Providers.Codex.Model = codexModels[m.codexModelIdx].value
		m.cfg.Providers.Copilot.Model = copilotModels[m.copilotModelIdx].value
		m.cfg.Providers.Claude.ReasoningEffort = effortValue(claudeEfforts[m.claudeEffortIdx])
		m.cfg.Providers.Codex.ReasoningEffort = effortValue(codexEfforts[m.codexEffortIdx])
		m.cfg.Providers.Copilot.ReasoningEffort = effortValue(copilotEfforts[m.copilotEffortIdx])
		// Apply global agent timeout to all providers.
		t := m.agentTimeout
		if t == "30m" {
			t = "" // 30m is the default; no need to write it explicitly
		}
		m.cfg.Providers.Claude.Timeout = t
		m.cfg.Providers.Codex.Timeout = t
		m.cfg.Providers.Copilot.Timeout = t
		return m, m.setStep(stepTaskPreset)
	}
	return m, nil
}

// compressionModelsForProvider returns the model options for a compression provider.
func compressionModelsForProvider(provider string) []modelOption {
	switch provider {
	case "codex":
		return codexModels
	case "copilot":
		return copilotModels
	default:
		return claudeModels
	}
}

// compressionEffortsForProvider returns the effort options for a compression provider.
func compressionEffortsForProvider(provider string) []string {
	switch provider {
	case "codex":
		return codexEfforts
	case "copilot":
		return copilotEfforts
	default:
		return claudeEfforts
	}
}

func renderCompressionFields(b *strings.Builder, m *setupModel) {
	// Row 0: enabled toggle
	cursor := " "
	if m.compressionCursor == 0 {
		cursor = ">"
	}
	state := "off"
	if m.compressionEnabled {
		state = "on"
	}
	fmt.Fprintf(b, " %s Enable: [%s]  (space to toggle)\n", cursor, state)

	if !m.compressionEnabled {
		b.WriteString(styleDim.Render("   (enable to configure provider, model, effort)"))
		b.WriteString("\n")
		return
	}

	// Row 1: provider
	cursor = " "
	if m.compressionCursor == 1 {
		cursor = ">"
	}
	fmt.Fprintf(b, " %s Provider: ← %s →\n", cursor, m.compressionProvider)

	// Row 2: model
	cursor = " "
	if m.compressionCursor == 2 {
		cursor = ">"
	}
	models := compressionModelsForProvider(m.compressionProvider)
	modelLabel := models[m.compressionModelIdx].label
	fmt.Fprintf(b, " %s Model:    ← %s →\n", cursor, modelLabel)

	// Row 3: effort
	cursor = " "
	if m.compressionCursor == 3 {
		cursor = ">"
	}
	efforts := compressionEffortsForProvider(m.compressionProvider)
	effortLabel := efforts[m.compressionEffortIdx]
	fmt.Fprintf(b, " %s Effort:   ← %s →\n", cursor, effortLabel)

	b.WriteString(styleNote.Render("Tip: ←/→ cycle values  'default' = provider built-in"))
	b.WriteString("\n")
}

func (m *setupModel) handleCompressionInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxRows := 1
	if m.compressionEnabled {
		maxRows = 4
	}

	switch msg.String() {
	case "up", "k":
		if m.compressionCursor > 0 {
			m.compressionCursor--
		}
	case "down", "j":
		if m.compressionCursor < maxRows-1 {
			m.compressionCursor++
		}
	case " ":
		if m.compressionCursor == 0 {
			m.compressionEnabled = !m.compressionEnabled
			if !m.compressionEnabled {
				m.compressionCursor = 0
			}
		}
	case "left", "h":
		switch m.compressionCursor {
		case 1:
			idx := compressionProviderIdx(m.compressionProvider)
			if idx > 0 {
				m.compressionProvider = compressionProviders[idx-1]
				m.compressionModelIdx = 0
				m.compressionEffortIdx = 0
			}
		case 2:
			if m.compressionModelIdx > 0 {
				m.compressionModelIdx--
			}
		case 3:
			if m.compressionEffortIdx > 0 {
				m.compressionEffortIdx--
			}
		}
	case "right", "l":
		switch m.compressionCursor {
		case 1:
			idx := compressionProviderIdx(m.compressionProvider)
			if idx < len(compressionProviders)-1 {
				m.compressionProvider = compressionProviders[idx+1]
				m.compressionModelIdx = 0
				m.compressionEffortIdx = 0
			}
		case 2:
			models := compressionModelsForProvider(m.compressionProvider)
			if m.compressionModelIdx < len(models)-1 {
				m.compressionModelIdx++
			}
		case 3:
			efforts := compressionEffortsForProvider(m.compressionProvider)
			if m.compressionEffortIdx < len(efforts)-1 {
				m.compressionEffortIdx++
			}
		}
	case "e":
		switch m.compressionCursor {
		case 3:
			efforts := compressionEffortsForProvider(m.compressionProvider)
			m.compressionEffortIdx = (m.compressionEffortIdx + 1) % len(efforts)
		}
	case "enter":
		m.cfg.PromptCompression.Enabled = m.compressionEnabled
		if m.compressionEnabled {
			models := compressionModelsForProvider(m.compressionProvider)
			efforts := compressionEffortsForProvider(m.compressionProvider)
			m.cfg.PromptCompression.Provider = m.compressionProvider
			m.cfg.PromptCompression.Model = models[m.compressionModelIdx].value
			m.cfg.PromptCompression.ReasoningEffort = effortValue(efforts[m.compressionEffortIdx])
		} else {
			m.cfg.PromptCompression.Provider = ""
			m.cfg.PromptCompression.Model = ""
			m.cfg.PromptCompression.ReasoningEffort = ""
		}
		nextStep := stepPreview
		if m.jiraEnabled && systemdAvailable() {
			nextStep = stepSystemd
		}
		return m, m.setStep(nextStep)
	}
	return m, nil
}

func compressionProviderIdx(provider string) int {
	for i, p := range compressionProviders {
		if p == provider {
			return i
		}
	}
	return 0
}

// modelIndex returns the index of the given model value in a model list, defaulting to 0.
func modelIndex(models []modelOption, value string) int {
	for i, m := range models {
		if m.value == value {
			return i
		}
	}
	return 0
}

func renderScheduleFields(b *strings.Builder, m *setupModel) {
	fields := []string{
		fmt.Sprintf("Start time: %s", m.scheduleStart),
		fmt.Sprintf("Cycles: %d", m.scheduleCycles),
		fmt.Sprintf("Interval: %s", m.scheduleInterval),
		fmt.Sprintf("Mode: %s (interval|cron)", m.scheduleMode),
		fmt.Sprintf("Cron: %s", m.scheduleCron),
	}
	for i, field := range fields {
		cursor := " "
		if i == m.scheduleCursor {
			cursor = ">"
		}
		fmt.Fprintf(b, " %s %s\n", cursor, field)
	}
	if m.scheduleMode == "interval" {
		start, errStart := scheduler.ParseTimeOfDay(m.scheduleStart)
		interval, errInterval := time.ParseDuration(m.scheduleInterval)
		if errStart == nil && errInterval == nil {
			end := computeWindowEnd(start, interval, m.scheduleCycles)
			fmt.Fprintf(b, "   Window end (computed): %s\n", end)
		}
	}
}

func scheduleFieldHelp(cursor int, mode string) string {
	switch cursor {
	case 0:
		return "Start time: when Nightshift becomes eligible to run each night (local time)."
	case 1:
		return "Cycles: how many runs to attempt inside the nightly window."
	case 2:
		return "Interval: spacing between runs (e.g., 30m = every 30 minutes)."
	case 3:
		return "Mode: interval uses Start/Cycles/Interval; cron uses a single cron expression."
	case 4:
		if mode == "cron" {
			return "Cron: advanced schedule (e.g., \"0 2 * * *\" = 2:00 AM daily)."
		}
		return "Cron: only used when mode is set to cron."
	default:
		return ""
	}
}

func makeTaskItems(cfg *config.Config, projects []string, preset setup.Preset) []taskItem {
	defs := tasks.AllDefinitionsSorted()
	var selected map[tasks.TaskType]bool
	if len(cfg.Tasks.Enabled) > 0 {
		selected = make(map[tasks.TaskType]bool, len(cfg.Tasks.Enabled))
		for _, enabled := range cfg.Tasks.Enabled {
			selected[tasks.TaskType(enabled)] = true
		}
	} else {
		signals := setup.DetectRepoSignals(projects)
		selected = setup.PresetTasks(preset, defs, signals)
	}

	items := make([]taskItem, 0, len(defs))
	for _, def := range defs {
		items = append(items, taskItem{
			def:      def,
			selected: selected[def.Type],
		})
	}
	return items
}

func runPreviewCmd(cfg *config.Config, projects []string) tea.Cmd {
	return func() tea.Msg {
		output, err := buildSetupPreviewOutput(cfg, projects)
		return previewMsg{output: output, err: err}
	}
}

func buildSetupPreviewOutput(cfg *config.Config, projects []string) (string, error) {
	database, err := db.Open(cfg.ExpandedDBPath())
	if err != nil {
		return "", err
	}
	defer func() { _ = database.Close() }()

	result, err := buildPreviewResult(cfg, database, projects, "", 1, "", nil, false, false)
	if err != nil {
		return "", err
	}
	return renderSetupPreviewText(result), nil
}

type setupStepInfo struct {
	step  setupStep
	label string
}

func renderSetupStepper(m *setupModel) string {
	steps := setupSteps(m.includePathStep)
	stepIndex := 0
	stepLabel := ""
	for i, info := range steps {
		if info.step == m.step {
			stepIndex = i
			stepLabel = info.label
			break
		}
	}

	total := len(steps)
	current := stepIndex + 1
	line := fmt.Sprintf("%s  %s", styleNote.Render(fmt.Sprintf("Step %d of %d", current, total)), styleAccent.Render(stepLabel))
	bar := renderSetupProgressBar(current, total, 28)
	return line + "\n" + bar
}

func setupSteps(includePathStep bool) []setupStepInfo {
	steps := []setupStepInfo{
		{step: stepWelcome, label: "Welcome"},
		{step: stepConfig, label: "Global config"},
		{step: stepProjects, label: "Projects"},
		{step: stepBudget, label: "Budget"},
		{step: stepSafety, label: "Safety"},
		{step: stepModel, label: "Models"},
		{step: stepTaskPreset, label: "Task presets"},
		{step: stepTaskSelect, label: "Task selection"},
		{step: stepSchedule, label: "Schedule"},
		{step: stepJira, label: "Jira"},
		{step: stepPromptCompression, label: "Compression"},
		{step: stepSystemd, label: "Systemd"},
		{step: stepPreview, label: "Preview"},
	}
	if includePathStep {
		steps = append(steps, setupStepInfo{step: stepPath, label: "PATH"})
	}
	steps = append(steps,
		setupStepInfo{step: stepDaemon, label: "Daemon"},
		setupStepInfo{step: stepFinish, label: "Finish"},
	)
	return steps
}

func renderSetupProgressBar(current, total, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	if current < 1 {
		current = 1
	}
	if current > total {
		current = total
	}
	filled := (width*current + total - 1) / total
	if filled > width {
		filled = width
	}
	empty := width - filled
	filledPart := styleOk.Render(strings.Repeat("=", filled))
	emptyPart := styleDim.Render(strings.Repeat("-", empty))
	return "[" + filledPart + emptyPart + "]"
}

func computeWindowEnd(start scheduler.TimeOfDay, interval time.Duration, cycles int) scheduler.TimeOfDay {
	if cycles <= 0 {
		cycles = 3
	}
	total := interval * time.Duration(cycles)
	startTime := time.Date(2000, 1, 1, start.Hour, start.Minute, 0, 0, time.Local)
	endTime := startTime.Add(total)
	return scheduler.TimeOfDay{Hour: endTime.Hour(), Minute: endTime.Minute()}
}

func detectServiceState() (string, serviceState) {
	service := detectServiceType()
	state := serviceState{}

	switch service {
	case ServiceLaunchd:
		home, _ := os.UserHomeDir()
		plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdPlistName)
		if _, err := os.Stat(plistPath); err == nil {
			state.installed = true
			state.detail = plistPath
		}
	case ServiceSystemd:
		home, _ := os.UserHomeDir()
		servicePath := filepath.Join(home, ".config", "systemd", "user", systemdServiceName)
		timerPath := filepath.Join(home, ".config", "systemd", "user", systemdTimerName)
		if _, err := os.Stat(servicePath); err == nil {
			state.installed = true
			state.detail = servicePath
		}
		if _, err := os.Stat(timerPath); err == nil && state.detail != "" {
			state.detail = fmt.Sprintf("%s, %s", state.detail, timerPath)
		}
	case ServiceCron:
		out, err := exec.Command("crontab", "-l").CombinedOutput()
		if err == nil && strings.Contains(string(out), cronMarker) {
			state.installed = true
			state.detail = "cron entry present"
		}
	}

	running, _ := isDaemonRunning()
	state.running = running
	return service, state
}

func installService(service string, cfg *config.Config) error {
	if cfg == nil {
		loaded, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg = loaded
	}

	switch service {
	case ServiceLaunchd:
		return installLaunchd(mustExecutablePath(), cfg)
	case ServiceSystemd:
		return installSystemd(mustExecutablePath(), cfg)
	case ServiceCron:
		return installCron(mustExecutablePath(), cfg)
	default:
		return fmt.Errorf("unknown service type: %s", service)
	}
}

func uninstallService(service string) error {
	switch service {
	case ServiceLaunchd:
		if !uninstallLaunchd() {
			return fmt.Errorf("launchd service not found")
		}
		return nil
	case ServiceSystemd:
		if !uninstallSystemd() {
			return fmt.Errorf("systemd service not found")
		}
		return nil
	case ServiceCron:
		if !uninstallCron() {
			return fmt.Errorf("cron entry not found")
		}
		return nil
	default:
		return fmt.Errorf("unknown service type: %s", service)
	}
}

func mustExecutablePath() string {
	path, _ := os.Executable()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return real
}

func writeGlobalConfigToPath(cfg *config.Config, configPath string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	if _, err := os.Stat(configPath); err == nil {
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("read config: %w", err)
		}
	}

	v.Set("schedule", cfg.Schedule)
	v.Set("budget.max_percent", cfg.Budget.MaxPercent)

	// Providers: set fields individually to match mapstructure tag names (fixes #20)
	v.Set("providers.claude.enabled", cfg.Providers.Claude.Enabled)
	v.Set("providers.claude.data_path", cfg.Providers.Claude.DataPath)
	v.Set("providers.claude.model", cfg.Providers.Claude.Model)
	v.Set("providers.claude.reasoning_effort", cfg.Providers.Claude.ReasoningEffort)
	v.Set("providers.claude.timeout", cfg.Providers.Claude.Timeout)
	v.Set("providers.claude.dangerously_skip_permissions", cfg.Providers.Claude.DangerouslySkipPermissions)
	v.Set("providers.claude.dangerously_bypass_approvals_and_sandbox", cfg.Providers.Claude.DangerouslyBypassApprovalsAndSandbox)
	v.Set("providers.codex.enabled", cfg.Providers.Codex.Enabled)
	v.Set("providers.codex.data_path", cfg.Providers.Codex.DataPath)
	v.Set("providers.codex.model", cfg.Providers.Codex.Model)
	v.Set("providers.codex.reasoning_effort", cfg.Providers.Codex.ReasoningEffort)
	v.Set("providers.codex.timeout", cfg.Providers.Codex.Timeout)
	v.Set("providers.codex.dangerously_skip_permissions", cfg.Providers.Codex.DangerouslySkipPermissions)
	v.Set("providers.codex.dangerously_bypass_approvals_and_sandbox", cfg.Providers.Codex.DangerouslyBypassApprovalsAndSandbox)
	v.Set("providers.copilot.enabled", cfg.Providers.Copilot.Enabled)
	v.Set("providers.copilot.data_path", cfg.Providers.Copilot.DataPath)
	v.Set("providers.copilot.model", cfg.Providers.Copilot.Model)
	v.Set("providers.copilot.reasoning_effort", cfg.Providers.Copilot.ReasoningEffort)
	v.Set("providers.copilot.timeout", cfg.Providers.Copilot.Timeout)
	v.Set("providers.copilot.dangerously_skip_permissions", cfg.Providers.Copilot.DangerouslySkipPermissions)
	v.Set("providers.copilot.dangerously_bypass_approvals_and_sandbox", cfg.Providers.Copilot.DangerouslyBypassApprovalsAndSandbox)
	v.Set("providers.preference", cfg.Providers.Preference)
	v.Set("projects", cfg.Projects)
	v.Set("tasks.enabled", cfg.Tasks.Enabled)

	// Jira integration — written unconditionally to prevent stale keys.
	// When Jira is disabled (Site is empty), all jira.* keys are zeroed out.
	if cfg.Jira.Site != "" {
		v.Set("jira.site", cfg.Jira.Site)
		v.Set("jira.email", cfg.Jira.Email)
		v.Set("jira.token_env", cfg.Jira.TokenEnv)
		v.Set("jira.max_tickets", cfg.Jira.MaxTickets)
		v.Set("jira.budget_enabled", cfg.Jira.BudgetEnabled)
		if cfg.Jira.WorkspaceRoot != "" {
			v.Set("jira.workspace_root", cfg.Jira.WorkspaceRoot)
		}
		if cfg.Jira.CleanupAfterDays != 0 {
			v.Set("jira.cleanup_after_days", cfg.Jira.CleanupAfterDays)
		}
		// Build projects as explicit maps so mapstructure tags are honoured and empty
		// optional fields (lint_command, test_command, per-project phase overrides) are omitted.
		v.Set("jira.projects", jiraProjectsToMaps(cfg.Jira.Projects))
		// Write each phase field individually so viper uses the dot-key path instead of
		// reflecting over a struct (which would lose mapstructure tag key names).
		v.Set("jira.validation.provider", cfg.Jira.Validation.Provider)
		v.Set("jira.validation.model", cfg.Jira.Validation.Model)
		v.Set("jira.validation.timeout", cfg.Jira.Validation.Timeout)
		v.Set("jira.validation.reasoning_effort", cfg.Jira.Validation.ReasoningEffort)
		v.Set("jira.plan.provider", cfg.Jira.Plan.Provider)
		v.Set("jira.plan.model", cfg.Jira.Plan.Model)
		v.Set("jira.plan.timeout", cfg.Jira.Plan.Timeout)
		v.Set("jira.plan.reasoning_effort", cfg.Jira.Plan.ReasoningEffort)
		v.Set("jira.implement.provider", cfg.Jira.Implement.Provider)
		v.Set("jira.implement.model", cfg.Jira.Implement.Model)
		v.Set("jira.implement.timeout", cfg.Jira.Implement.Timeout)
		v.Set("jira.implement.reasoning_effort", cfg.Jira.Implement.ReasoningEffort)
		v.Set("jira.review_fix.provider", cfg.Jira.ReviewFix.Provider)
		v.Set("jira.review_fix.model", cfg.Jira.ReviewFix.Model)
		v.Set("jira.review_fix.timeout", cfg.Jira.ReviewFix.Timeout)
		v.Set("jira.review_fix.reasoning_effort", cfg.Jira.ReviewFix.ReasoningEffort)
		v.Set("jira.systemd_enabled", cfg.Jira.SystemdEnabled)
		if cfg.Jira.SystemdOnCalendar != "" {
			v.Set("jira.systemd_on_calendar", cfg.Jira.SystemdOnCalendar)
		}
	} else {
		// Explicitly zero out every jira leaf key so a previously enabled Jira config
		// does not survive a disable-and-save round-trip.
		v.Set("jira.site", "")
		v.Set("jira.email", "")
		v.Set("jira.token_env", "")
		v.Set("jira.max_tickets", 0)
		v.Set("jira.budget_enabled", false)
		v.Set("jira.projects", []interface{}{})
		v.Set("jira.validation.provider", "")
		v.Set("jira.validation.model", "")
		v.Set("jira.validation.timeout", "")
		v.Set("jira.validation.reasoning_effort", "")
		v.Set("jira.plan.provider", "")
		v.Set("jira.plan.model", "")
		v.Set("jira.plan.timeout", "")
		v.Set("jira.plan.reasoning_effort", "")
		v.Set("jira.implement.provider", "")
		v.Set("jira.implement.model", "")
		v.Set("jira.implement.timeout", "")
		v.Set("jira.implement.reasoning_effort", "")
		v.Set("jira.review_fix.provider", "")
		v.Set("jira.review_fix.model", "")
		v.Set("jira.review_fix.timeout", "")
		v.Set("jira.review_fix.reasoning_effort", "")
		v.Set("jira.systemd_enabled", false)
		v.Set("jira.systemd_on_calendar", "")
	}

	// Prompt compression — always write to prevent stale keys on disable.
	v.Set("prompt_compression.enabled", cfg.PromptCompression.Enabled)
	v.Set("prompt_compression.provider", cfg.PromptCompression.Provider)
	v.Set("prompt_compression.model", cfg.PromptCompression.Model)
	v.Set("prompt_compression.reasoning_effort", cfg.PromptCompression.ReasoningEffort)
	if cfg.PromptCompression.Threshold > 0 {
		v.Set("prompt_compression.threshold", cfg.PromptCompression.Threshold)
	}

	if err := v.WriteConfig(); err != nil {
		if os.IsNotExist(err) {
			return v.SafeWriteConfig()
		}
		return err
	}

	return nil
}

func execLookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (m *setupModel) updateProjectGitignores() {
	m.gitignoreAdded = 0
	m.gitignoreKept = 0
	m.gitignoreErrs = nil

	for _, project := range m.cfg.Projects {
		path := expandPath(project.Path)
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		gitignorePath := filepath.Join(path, ".gitignore")
		added, err := ensureGitignoreEntry(gitignorePath, nightshiftPlanIgnore, nightshiftPlanIgnoreComment)
		if err != nil {
			m.gitignoreErrs = append(m.gitignoreErrs, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		if added {
			m.gitignoreAdded++
		} else {
			m.gitignoreKept++
		}
	}
}

func ensureGitignoreEntry(gitignorePath, entry, comment string) (bool, error) {
	var existing string
	if data, err := os.ReadFile(gitignorePath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return false, err
	}

	if gitignoreHasEntry(existing, entry) {
		return false, nil
	}

	var b strings.Builder
	if existing != "" {
		b.WriteString(strings.TrimRight(existing, "\n"))
		b.WriteString("\n")
	}
	if comment != "" && !strings.Contains(existing, comment) {
		b.WriteString(comment)
		b.WriteString("\n")
	}
	b.WriteString(entry)
	b.WriteString("\n")

	// SECURITY: Use 0644 for .gitignore (world-readable is acceptable for git-tracked files)
	// but ensure proper atomic write to prevent corruption
	return true, os.WriteFile(gitignorePath, []byte(b.String()), 0644)
}

func gitignoreHasEntry(content, entry string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "/")
		trimmed = strings.TrimSuffix(trimmed, "/")
		if trimmed == entry {
			return true
		}
	}
	return false
}

// jiraSubStep constants for the Jira wizard step.
const (
	jiraSubStepEnable     = 0
	jiraSubStepSite       = 1
	jiraSubStepEmail      = 2
	jiraSubStepTokenEnv   = 3
	jiraSubStepProjects   = 4
	jiraSubStepPhases     = 5
	jiraSubStepMaxTickets = 6
	jiraSubStepPing       = 7
)

// jiraModelIndex returns the index of model in the claude Jira model list, defaulting to 0.
func jiraModelIndex(model string) int {
	return jiraModelIndexForProvider("claude", model)
}

// jiraPhaseModelsForProvider returns the model list for the given provider.
// Derived from the global provider model lists so there is a single source of truth.
func jiraPhaseModelsForProvider(provider string) []string {
	switch provider {
	case "codex":
		return modelOptionValues(codexModels[1:]) // skip "default" entry
	case "copilot":
		return modelOptionValues(copilotModels[1:])
	default:
		return modelOptionValues(claudeModels[1:])
	}
}

// jiraPhaseEffortsForProvider returns the effort slice for the given provider.
func jiraPhaseEffortsForProvider(provider string) []string {
	switch provider {
	case "codex":
		return codexEfforts
	case "copilot":
		return copilotEfforts
	default:
		return claudeEfforts
	}
}

// jiraProviderIndex returns the index of provider in jiraProviders, defaulting to 0.
func jiraProviderIndex(provider string) int {
	for i, p := range jiraProviders {
		if p == provider {
			return i
		}
	}
	return 0
}

// jiraModelIndexForProvider returns the index of model within the provider's model list.
func jiraModelIndexForProvider(provider, model string) int {
	models := jiraPhaseModelsForProvider(provider)
	for i, m := range models {
		if m == model {
			return i
		}
	}
	return 0
}

// defaultJiraPhaseProviders returns the initial provider array for Jira phases.
// Copilot is preferred when present in preference (it is non-interactive by design);
// otherwise falls back to the first preference entry, then "claude".
func defaultJiraPhaseProviders(preference []string) [4]string {
	p := "claude"
	for _, pref := range preference {
		if pref == "copilot" {
			p = "copilot"
			break
		}
	}
	if p == "claude" && len(preference) > 0 && preference[0] != "" {
		p = preference[0]
	}
	return [4]string{p, p, p, p}
}

// defaultJiraPhaseModelIdxs returns initial model indexes for Jira phases.
// All phases default to index 0 (first model in the provider list). For copilot
// this is claude-sonnet-4.6 which is a good default for plan/implement.
// For claude this is claude-opus-4-6; index 1 (sonnet) would be more balanced but
// the copilot path (which is preferred when copilot is in preference) maps index 0
// to the right model, so we keep it consistent.
func defaultJiraPhaseModelIdxs(preference []string) [4]int {
	p := "claude"
	for _, pref := range preference {
		if pref == "copilot" {
			p = "copilot"
			break
		}
	}
	if p == "claude" && len(preference) > 0 && preference[0] != "" {
		p = preference[0]
	}
	models := jiraPhaseModelsForProvider(p)
	implIdx := 0
	if p != "copilot" && len(models) > 1 {
		implIdx = 1
	}
	return [4]int{0, implIdx, implIdx, implIdx}
}

// handleJiraInput dispatches to the appropriate Jira sub-step handler.
func (m *setupModel) handleJiraInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.jiraSubStep {
	case jiraSubStepEnable:
		return m.handleJiraEnableInput(msg)
	case jiraSubStepSite, jiraSubStepEmail, jiraSubStepTokenEnv, jiraSubStepMaxTickets:
		return m.handleJiraTextInput(msg)
	case jiraSubStepProjects:
		return m.handleJiraProjectsInput(msg)
	case jiraSubStepPhases:
		return m.handleJiraPhaseInput(msg)
	case jiraSubStepPing:
		return m.handleJiraPingInput(msg)
	}
	return m, nil
}

func (m *setupModel) handleJiraEnableInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.jiraEnableCursor > 0 {
			m.jiraEnableCursor--
		}
	case "down", "j":
		if m.jiraEnableCursor < 1 {
			m.jiraEnableCursor++
		}
	case "y", "Y":
		m.jiraEnabled = true
		m.jiraSubStep = jiraSubStepSite
		m.jiraInput.SetValue(m.jiraSite)
		m.jiraInput.Focus()
	case "n", "N":
		m.jiraEnabled = false
		m.cfg.Jira = jiraconfig.JiraConfig{}
		if err := writeGlobalConfigToPath(m.cfg, m.configPath); err != nil {
			m.jiraErr = err.Error()
			return m, nil
		}
		return m, m.setStep(stepPreview)
	case "enter":
		if m.jiraEnableCursor == 0 {
			m.jiraEnabled = true
			m.jiraSubStep = jiraSubStepSite
			m.jiraInput.SetValue(m.jiraSite)
			m.jiraInput.Focus()
		} else {
			m.jiraEnabled = false
			m.cfg.Jira = jiraconfig.JiraConfig{}
			if err := writeGlobalConfigToPath(m.cfg, m.configPath); err != nil {
				m.jiraErr = err.Error()
				return m, nil
			}
			return m, m.setStep(stepPreview)
		}
	}
	return m, nil
}

func (m *setupModel) handleJiraTextInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.jiraErr = ""
		m.jiraInput.Blur()
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.jiraInput.Value())
		switch m.jiraSubStep {
		case jiraSubStepSite:
			if value == "" {
				m.jiraErr = "site is required"
				return m, nil
			}
			// Strip https:// prefix if user pastes full URL
			value = strings.TrimPrefix(value, "https://")
			value = strings.TrimSuffix(value, ".atlassian.net")
			value = strings.TrimSuffix(value, "/")
			m.jiraSite = value
			m.jiraErr = ""
			m.jiraSubStep = jiraSubStepEmail
			m.jiraInput.SetValue(m.jiraEmail)
			m.jiraInput.Focus()
		case jiraSubStepEmail:
			if value == "" {
				m.jiraErr = "email is required"
				return m, nil
			}
			m.jiraEmail = value
			m.jiraErr = ""
			m.jiraSubStep = jiraSubStepTokenEnv
			m.jiraInput.SetValue(m.jiraTokenEnv)
			m.jiraInput.Focus()
		case jiraSubStepTokenEnv:
			if value == "" {
				value = "JIRA_API_TOKEN"
			}
			m.jiraTokenEnv = value
			m.jiraErr = ""
			m.jiraSubStep = jiraSubStepProjects
			m.jiraInput.Blur()
		case jiraSubStepMaxTickets:
			if value == "" {
				value = "10"
			}
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				m.jiraErr = "max tickets must be a positive integer"
				return m, nil
			}
			m.jiraMaxTickets = n
			m.jiraErr = ""
			m.jiraSubStep = jiraSubStepPing
			m.jiraInput.Blur()
			m.jiraPinging = true
			m.jiraPingOK = false
			m.jiraPingErr = ""
			return m, runJiraPingCmd(m)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.jiraInput, cmd = m.jiraInput.Update(msg)
	return m, cmd
}

func (m *setupModel) handleJiraRepoInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.jiraRepoEditing {
		switch msg.String() {
		case "esc":
			m.jiraRepoEditing = false
			m.jiraRepoField = 0
			m.jiraRepoEditURL = ""
			m.jiraErr = ""
			m.jiraInput.Blur()
			return m, nil
		case "enter":
			value := strings.TrimSpace(m.jiraInput.Value())
			if m.jiraRepoField == 0 {
				// Collecting URL
				if value == "" {
					m.jiraErr = "URL is required"
					return m, nil
				}
				m.jiraRepoEditURL = value
				m.jiraRepoField = 1
				m.jiraInput.SetValue("main")
				m.jiraInput.Focus()
				m.jiraErr = ""
				return m, nil
			}
			// Collecting base branch
			branch := value
			if branch == "" {
				branch = "main"
			}
			m.jiraRepos = append(m.jiraRepos, jiraRepoEntry{
				URL:        m.jiraRepoEditURL,
				BaseBranch: branch,
			})
			m.jiraRepoCursor = len(m.jiraRepos) - 1
			m.jiraRepoEditing = false
			m.jiraRepoField = 0
			m.jiraRepoEditURL = ""
			m.jiraErr = ""
			m.jiraInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.jiraInput, cmd = m.jiraInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.jiraRepoCursor > 0 {
			m.jiraRepoCursor--
		}
	case "down", "j":
		if m.jiraRepoCursor < len(m.jiraRepos)-1 {
			m.jiraRepoCursor++
		}
	case "a":
		m.jiraRepoEditing = true
		m.jiraRepoField = 0
		m.jiraRepoEditURL = ""
		m.jiraErr = ""
		m.jiraInput.SetValue("")
		m.jiraInput.Placeholder = "git@github.com:org/repo.git"
		m.jiraInput.Focus()
	case "d":
		if len(m.jiraRepos) > 0 {
			m.jiraRepos = append(m.jiraRepos[:m.jiraRepoCursor], m.jiraRepos[m.jiraRepoCursor+1:]...)
			if m.jiraRepoCursor >= len(m.jiraRepos) && m.jiraRepoCursor > 0 {
				m.jiraRepoCursor--
			}
		}
	case "enter":
		if len(m.jiraRepos) == 0 {
			m.jiraErr = "add at least one repository"
			return m, nil
		}
		m.jiraErr = ""
		m.jiraSubStep = jiraSubStepPhases
	}
	return m, nil
}

func (m *setupModel) handleJiraProjectsInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.jiraProjectEditMode {
		return m.handleJiraProjectEditInput(msg)
	}
	switch msg.String() {
	case "up", "k":
		if m.jiraProjectCursor > 0 {
			m.jiraProjectCursor--
		}
	case "down", "j":
		if m.jiraProjectCursor < len(m.jiraProjects)-1 {
			m.jiraProjectCursor++
		}
	case "a":
		m.jiraProjectEditMode = true
		m.jiraProjectEditSubStep = 0
		m.jiraEditProjectKey = ""
		m.jiraEditProjectLabel = "nightshift"
		m.jiraRepos = nil
		m.jiraRepoCursor = 0
		m.jiraRepoEditing = false
		m.jiraErr = ""
		m.jiraInput.SetValue("")
		m.jiraInput.Placeholder = "PROJ"
		m.jiraInput.Focus()
	case "d":
		if len(m.jiraProjects) > 0 {
			m.jiraProjects = append(m.jiraProjects[:m.jiraProjectCursor], m.jiraProjects[m.jiraProjectCursor+1:]...)
			if m.jiraProjectCursor >= len(m.jiraProjects) && m.jiraProjectCursor > 0 {
				m.jiraProjectCursor--
			}
		}
	case "enter":
		if len(m.jiraProjects) == 0 {
			m.jiraErr = "add at least one project"
			return m, nil
		}
		m.jiraErr = ""
		m.jiraSubStep = jiraSubStepPhases
	}
	return m, nil
}

func (m *setupModel) handleJiraProjectEditInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.jiraProjectEditSubStep {
	case 0: // key
		switch msg.String() {
		case "esc":
			m.jiraProjectEditMode = false
			m.jiraErr = ""
			m.jiraInput.Blur()
		case "enter":
			value := strings.ToUpper(strings.TrimSpace(m.jiraInput.Value()))
			if value == "" {
				m.jiraErr = "project key is required"
				return m, nil
			}
			m.jiraEditProjectKey = value
			m.jiraErr = ""
			m.jiraProjectEditSubStep = 1
			m.jiraInput.SetValue(m.jiraEditProjectLabel)
			m.jiraInput.Placeholder = "nightshift"
			m.jiraInput.Focus()
		default:
			var cmd tea.Cmd
			m.jiraInput, cmd = m.jiraInput.Update(msg)
			return m, cmd
		}
	case 1: // label
		switch msg.String() {
		case "esc":
			m.jiraProjectEditSubStep = 0
			m.jiraErr = ""
			m.jiraInput.SetValue(m.jiraEditProjectKey)
			m.jiraInput.Placeholder = "PROJ"
			m.jiraInput.Focus()
		case "enter":
			value := strings.TrimSpace(m.jiraInput.Value())
			if value == "" {
				value = "nightshift"
			}
			m.jiraEditProjectLabel = value
			m.jiraErr = ""
			m.jiraProjectEditSubStep = 2
			m.jiraInput.Blur()
		default:
			var cmd tea.Cmd
			m.jiraInput, cmd = m.jiraInput.Update(msg)
			return m, cmd
		}
	case 2: // repos
		model, cmd := m.handleJiraRepoInput(msg)
		mm := model.(*setupModel)
		// When repos step is "done" (enter pressed with repos), it transitions to jiraSubStepPhases.
		// Intercept that and finalise the project instead.
		if mm.jiraSubStep == jiraSubStepPhases {
			mm.jiraSubStep = jiraSubStepProjects
			mm.jiraProjects = append(mm.jiraProjects, jiraProjectEntry{
				Key:   mm.jiraEditProjectKey,
				Label: mm.jiraEditProjectLabel,
				Repos: append([]jiraRepoEntry(nil), mm.jiraRepos...),
			})
			mm.jiraProjectCursor = len(mm.jiraProjects) - 1
			mm.jiraProjectEditMode = false
			mm.jiraProjectEditSubStep = 0
			mm.jiraRepos = nil
			mm.jiraRepoCursor = 0
			mm.jiraInput.Blur()
			mm.jiraErr = ""
		}
		return mm, cmd
	}
	return m, nil
}

func (m *setupModel) handleJiraPhaseInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.jiraPhaseTimeoutEdit {
		switch msg.String() {
		case "enter":
			val := strings.TrimSpace(m.jiraPhaseTimeoutInput.Value())
			if val != "" {
				if _, err := time.ParseDuration(val); err != nil {
					m.jiraErr = fmt.Sprintf("invalid timeout %q: must be a duration like 30m or 1h", val)
					return m, nil
				}
				m.jiraPhaseTimeout[m.jiraPhaseCursor] = val
			}
			m.jiraPhaseTimeoutEdit = false
			m.jiraErr = ""
			m.jiraPhaseTimeoutInput.Blur()
		case "esc":
			m.jiraPhaseTimeoutEdit = false
			m.jiraErr = ""
			m.jiraPhaseTimeoutInput.Blur()
		default:
			var cmd tea.Cmd
			m.jiraPhaseTimeoutInput, cmd = m.jiraPhaseTimeoutInput.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.jiraPhaseCursor > 0 {
			m.jiraPhaseCursor--
		}
	case "down", "j":
		if m.jiraPhaseCursor < 3 {
			m.jiraPhaseCursor++
		}
	case "left", "h":
		if m.jiraPhaseModelIdx[m.jiraPhaseCursor] > 0 {
			m.jiraPhaseModelIdx[m.jiraPhaseCursor]--
		}
	case "right", "l":
		provider := m.jiraPhaseProvider[m.jiraPhaseCursor]
		models := jiraPhaseModelsForProvider(provider)
		if m.jiraPhaseModelIdx[m.jiraPhaseCursor] < len(models)-1 {
			m.jiraPhaseModelIdx[m.jiraPhaseCursor]++
		}
	case "e":
		// Cycle effort forward for the focused phase (wraps around).
		efforts := jiraPhaseEffortsForProvider(m.jiraPhaseProvider[m.jiraPhaseCursor])
		m.jiraPhaseEffortIdx[m.jiraPhaseCursor] = (m.jiraPhaseEffortIdx[m.jiraPhaseCursor] + 1) % len(efforts)
	case "t":
		// Open inline timeout editor for the focused phase.
		m.jiraPhaseTimeoutEdit = true
		m.jiraPhaseTimeoutInput.SetValue(m.jiraPhaseTimeout[m.jiraPhaseCursor])
		m.jiraPhaseTimeoutInput.Focus()
	case "tab":
		// Cycle provider for the selected phase; reset model index to avoid out-of-bounds.
		// Clamp effort index to new provider's effort slice length.
		idx := jiraProviderIndex(m.jiraPhaseProvider[m.jiraPhaseCursor])
		idx = (idx + 1) % len(jiraProviders)
		m.jiraPhaseProvider[m.jiraPhaseCursor] = jiraProviders[idx]
		m.jiraPhaseModelIdx[m.jiraPhaseCursor] = 0
		newEfforts := jiraPhaseEffortsForProvider(jiraProviders[idx])
		if m.jiraPhaseEffortIdx[m.jiraPhaseCursor] >= len(newEfforts) {
			m.jiraPhaseEffortIdx[m.jiraPhaseCursor] = 0
		}
	case "enter":
		m.jiraSubStep = jiraSubStepMaxTickets
		m.jiraInput.SetValue(strconv.Itoa(m.jiraMaxTickets))
		m.jiraInput.Placeholder = "10"
		m.jiraInput.Focus()
	}
	return m, nil
}

func (m *setupModel) handleJiraPingInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.jiraPinging {
		// Still waiting for ping — ignore keystrokes
		return m, nil
	}
	if msg.String() == "enter" {
		m.applyJiraConfig()
		if err := writeGlobalConfigToPath(m.cfg, m.configPath); err != nil {
			m.jiraErr = err.Error()
			return m, nil
		}
		return m, m.setStep(stepPromptCompression)
	}
	return m, nil
}

// handleSystemdInput handles keyboard input for the systemd setup step.
func (m *setupModel) handleSystemdInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// On non-Linux or no systemd, skip silently.
	if !systemdAvailable() {
		return m, m.setStep(stepPreview)
	}

	switch m.systemdSubStep {
	case 0: // enable prompt
		switch msg.String() {
		case "up":
			if m.systemdCursor > 0 {
				m.systemdCursor--
			}
		case "down":
			if m.systemdCursor < 1 {
				m.systemdCursor++
			}
		case "y", "Y":
			m.systemdCursor = 0
			m.systemdSubStep = 1
			m.systemdInput.SetValue(m.systemdOnCalendar)
			m.systemdInput.Placeholder = "*-*-* 22:00:00"
			m.systemdInput.Focus()
		case "n", "N":
			m.cfg.Jira.SystemdEnabled = false
			if err := writeGlobalConfigToPath(m.cfg, m.configPath); err != nil {
				m.systemdErr = err.Error()
				return m, nil
			}
			return m, m.setStep(stepPreview)
		case "enter":
			if m.systemdCursor == 0 {
				m.systemdSubStep = 1
				m.systemdInput.SetValue(m.systemdOnCalendar)
				m.systemdInput.Placeholder = "*-*-* 22:00:00"
				m.systemdInput.Focus()
			} else {
				m.cfg.Jira.SystemdEnabled = false
				if err := writeGlobalConfigToPath(m.cfg, m.configPath); err != nil {
					m.systemdErr = err.Error()
					return m, nil
				}
				return m, m.setStep(stepPreview)
			}
		}
	case 1: // OnCalendar prompt
		switch msg.String() {
		case "esc":
			m.systemdErr = ""
			m.systemdInput.Blur()
			m.systemdSubStep = 0
			m.systemdCursor = 0
		case "enter":
			val := strings.TrimSpace(m.systemdInput.Value())
			if val == "" {
				val = "*-*-* 22:00:00"
			}
			if err := validateOnCalendar(val); err != nil {
				m.systemdErr = err.Error()
				return m, nil
			}
			m.systemdOnCalendar = val
			return m, m.applyAndInstallSystemd()
		default:
			var cmd tea.Cmd
			m.systemdInput, cmd = m.systemdInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// applyAndInstallSystemd writes systemd config and installs units.
func (m *setupModel) applyAndInstallSystemd() tea.Cmd {
	m.cfg.Jira.SystemdEnabled = true
	m.cfg.Jira.SystemdOnCalendar = m.systemdOnCalendar
	if err := writeGlobalConfigToPath(m.cfg, m.configPath); err != nil {
		m.systemdErr = err.Error()
		return nil
	}
	// Install with output suppressed (TUI will show a completion message instead)
	if err := installSystemdJiraWithExec(m.cfg, defaultExecRunner, io.Discard); err != nil {
		m.systemdErr = err.Error()
		return nil
	}
	return m.setStep(stepPreview)
}

// renderSystemdStep renders the systemd wizard step.
func renderSystemdStep(b *strings.Builder, m *setupModel) {
	if !systemdAvailable() {
		b.WriteString(styleDim.Render("systemd not available on this system — skipping."))
		b.WriteString("\n")
		return
	}

	switch m.systemdSubStep {
	case 0:
		b.WriteString("Install systemd user service for Jira pipeline?\n\n")
		options := []string{"Yes", "No"}
		for i, opt := range options {
			cursor := " "
			if i == m.systemdCursor {
				cursor = ">"
			}
			fmt.Fprintf(b, " %s %s\n", cursor, opt)
		}
		b.WriteString("\nUse ↑/↓ to select, Enter to confirm, or press y/n.\n")
	case 1:
		b.WriteString("OnCalendar schedule:\n\n")
		b.WriteString(m.systemdInput.View() + "\n")
		if m.systemdErr != "" {
			b.WriteString("Error: " + m.systemdErr + "\n")
		}
		b.WriteString("\nPress Enter to confirm, Esc to go back.\n")
	}
	if m.systemdErr != "" && m.systemdSubStep != 2 {
		b.WriteString("\nError: " + m.systemdErr + "\n")
	}
}

// applyJiraConfig populates cfg.Jira from wizard state.
func (m *setupModel) applyJiraConfig() {
	if !m.jiraEnabled {
		m.cfg.Jira = jiraconfig.JiraConfig{}
		return
	}

	projects := make([]jiraconfig.ProjectConfig, 0, len(m.jiraProjects))
	for _, jp := range m.jiraProjects {
		repos := make([]jiraconfig.RepoConfig, 0, len(jp.Repos))
		for _, r := range jp.Repos {
			branch := r.BaseBranch
			if branch == "" {
				branch = "main"
			}
			repos = append(repos, jiraconfig.RepoConfig{
				Name:       repoNameFromURL(r.URL),
				URL:        r.URL,
				BaseBranch: branch,
			})
		}
		label := jp.Label
		if label == "" {
			label = "nightshift"
		}
		projects = append(projects, jiraconfig.ProjectConfig{
			Key:   jp.Key,
			Label: label,
			Repos: repos,
		})
	}

	phases := [4]jiraconfig.PhaseConfig{}
	for i := range phases {
		provider := m.jiraPhaseProvider[i]
		if provider == "" {
			provider = "claude"
		}
		models := jiraPhaseModelsForProvider(provider)
		model := ""
		if m.jiraPhaseModelIdx[i] < len(models) {
			model = models[m.jiraPhaseModelIdx[i]]
		}
		efforts := jiraPhaseEffortsForProvider(provider)
		effort := ""
		if m.jiraPhaseEffortIdx[i] < len(efforts) {
			effort = effortValue(efforts[m.jiraPhaseEffortIdx[i]])
		}
		timeout := m.jiraPhaseTimeout[i]
		if timeout == "" {
			timeout = [4]string{"2m", "5m", "30m", "20m"}[i]
		}
		phases[i] = jiraconfig.PhaseConfig{
			Provider:        provider,
			Model:           model,
			Timeout:         timeout,
			ReasoningEffort: effort,
		}
	}

	m.cfg.Jira = jiraconfig.JiraConfig{
		Site:          m.jiraSite,
		Email:         m.jiraEmail,
		TokenEnv:      m.jiraTokenEnv,
		MaxTickets:    m.jiraMaxTickets,
		BudgetEnabled: true,
		Projects:      projects,
		Validation:    phases[0],
		Plan:          phases[1],
		Implement:     phases[2],
		ReviewFix:     phases[3],
	}
}

// jiraProjectsToMaps serialises Jira project configs into plain maps so viper
// writes mapstructure-tagged key names (e.g. "base_branch") and omits empty
// optional fields (lint_command, test_command, per-project phase overrides).
func jiraProjectsToMaps(projects []jiraconfig.ProjectConfig) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(projects))
	for _, proj := range projects {
		repoMaps := make([]map[string]interface{}, 0, len(proj.Repos))
		for _, r := range proj.Repos {
			repo := map[string]interface{}{
				"name":        r.Name,
				"url":         r.URL,
				"base_branch": r.BaseBranch,
			}
			if r.LintCommand != "" {
				repo["lint_command"] = r.LintCommand
			}
			if r.TestCommand != "" {
				repo["test_command"] = r.TestCommand
			}
			repoMaps = append(repoMaps, repo)
		}
		p := map[string]interface{}{
			"key":   proj.Key,
			"repos": repoMaps,
		}
		if proj.Label != "" {
			p["label"] = proj.Label
		}
		result = append(result, p)
	}
	return result
}

// repoNameFromURL derives a short repo name from a git SSH or HTTPS URL.
func repoNameFromURL(url string) string {
	base := filepath.Base(url)
	return strings.TrimSuffix(base, ".git")
}

// runJiraPingCmd returns a tea.Cmd that pings the Jira API asynchronously.
func runJiraPingCmd(m *setupModel) tea.Cmd {
	cfg := jiraconfig.JiraConfig{
		Site:     m.jiraSite,
		Email:    m.jiraEmail,
		TokenEnv: m.jiraTokenEnv,
	}
	return func() tea.Msg {
		client, err := jiraconfig.NewClient(cfg)
		if err != nil {
			return jiraPingMsg{ok: false, err: err.Error()}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Ping(ctx); err != nil {
			return jiraPingMsg{ok: false, err: err.Error()}
		}
		return jiraPingMsg{ok: true}
	}
}

// renderJiraStep renders the Jira wizard step based on the current sub-step.
func renderJiraStep(b *strings.Builder, m *setupModel) {
	switch m.jiraSubStep {
	case jiraSubStepEnable:
		b.WriteString("Connect Nightshift to Jira for autonomous ticket processing?\n\n")
		options := []string{"Yes, enable Jira integration", "No, skip"}
		for i, opt := range options {
			cursor := " "
			if i == m.jiraEnableCursor {
				cursor = ">"
			}
			fmt.Fprintf(b, " %s %s\n", cursor, opt)
		}
		b.WriteString("\nUse ↑/↓ to select, y/n or Enter to choose.\n")

	case jiraSubStepSite:
		b.WriteString("Jira instance URL\n")
		b.WriteString(styleNote.Render("Enter your subdomain (e.g. mysite) or full URL (https://mysite.atlassian.net)"))
		b.WriteString("\n\n")
		b.WriteString(m.jiraInput.View() + "\n")
		if m.jiraErr != "" {
			b.WriteString(styleWarn.Render("Error: "+m.jiraErr) + "\n")
		}
		b.WriteString("\nPress Enter to continue.\n")

	case jiraSubStepEmail:
		b.WriteString("Jira account email\n\n")
		b.WriteString(m.jiraInput.View() + "\n")
		if m.jiraErr != "" {
			b.WriteString(styleWarn.Render("Error: "+m.jiraErr) + "\n")
		}
		b.WriteString("\nPress Enter to continue.\n")

	case jiraSubStepTokenEnv:
		b.WriteString("API token environment variable\n")
		b.WriteString(styleNote.Render("Name of the env var holding your Jira API token (default: JIRA_API_TOKEN)"))
		b.WriteString("\n\n")
		b.WriteString(m.jiraInput.View() + "\n")
		envName := strings.TrimSpace(m.jiraInput.Value())
		if envName == "" {
			envName = m.jiraTokenEnv
		}
		if os.Getenv(envName) != "" {
			b.WriteString(styleOk.Render("✓ env var is set") + "\n")
		} else {
			b.WriteString(styleWarn.Render("✗ env var not set — set it before running nightshift jira run") + "\n")
		}
		if m.jiraErr != "" {
			b.WriteString(styleWarn.Render("Error: "+m.jiraErr) + "\n")
		}
		b.WriteString("\nPress Enter to continue.\n")

	case jiraSubStepProjects:
		renderJiraProjectsStep(b, m)

	case jiraSubStepPhases:
		renderJiraPhasesStep(b, m)

	case jiraSubStepMaxTickets:
		b.WriteString("Max tickets per run\n")
		b.WriteString(styleNote.Render("Maximum number of tickets to process in a single run (default: 10)"))
		b.WriteString("\n\n")
		b.WriteString(m.jiraInput.View() + "\n")
		if m.jiraErr != "" {
			b.WriteString(styleWarn.Render("Error: "+m.jiraErr) + "\n")
		}
		b.WriteString("\nPress Enter to continue (will test connection).\n")

	case jiraSubStepPing:
		b.WriteString("Testing Jira connection...\n\n")
		if m.jiraPinging {
			b.WriteString(styleNote.Render("Connecting to Jira...") + "\n")
		} else if m.jiraPingOK {
			b.WriteString(styleOk.Render("✓ Connected to Jira successfully") + "\n")
		} else {
			b.WriteString(styleWarn.Render("✗ Connection failed: "+m.jiraPingErr) + "\n")
			b.WriteString(styleNote.Render("You can still continue — verify credentials before running nightshift jira run.") + "\n")
		}
		if m.jiraErr != "" {
			b.WriteString(styleWarn.Render("Error: "+m.jiraErr) + "\n")
		}
		if !m.jiraPinging {
			b.WriteString("\nPress Enter to save and continue.\n")
		}
	}
}

func renderJiraProjectsStep(b *strings.Builder, m *setupModel) {
	if m.jiraProjectEditMode {
		renderJiraProjectEditStep(b, m)
		return
	}
	b.WriteString("Jira projects\n")
	b.WriteString("Use ↑/↓ to navigate, 'a' to add, 'd' to delete.\n\n")
	if len(m.jiraProjects) == 0 {
		b.WriteString(styleDim.Render("  (no projects configured)") + "\n")
	}
	for i, proj := range m.jiraProjects {
		cursor := " "
		if i == m.jiraProjectCursor {
			cursor = ">"
		}
		repoCount := len(proj.Repos)
		fmt.Fprintf(b, " %s %s  [label: %s, %d repo(s)]\n", cursor, proj.Key, proj.Label, repoCount)
	}
	if m.jiraErr != "" {
		b.WriteString("\n" + styleWarn.Render("Error: "+m.jiraErr) + "\n")
	}
	b.WriteString("\nPress Enter to continue.\n")
}

func renderJiraProjectEditStep(b *strings.Builder, m *setupModel) {
	switch m.jiraProjectEditSubStep {
	case 0:
		b.WriteString("Project key\n")
		b.WriteString(styleNote.Render("e.g. PROJ or VC"))
		b.WriteString("\n\n")
		b.WriteString(m.jiraInput.View() + "\n")
		if m.jiraErr != "" {
			b.WriteString(styleWarn.Render("Error: "+m.jiraErr) + "\n")
		}
		b.WriteString("\nPress Enter to continue, Esc to cancel.\n")
	case 1:
		b.WriteString("Ticket label filter\n")
		b.WriteString(styleNote.Render("Only tickets with this label will be processed (default: nightshift)"))
		b.WriteString("\n\n")
		b.WriteString(m.jiraInput.View() + "\n")
		if m.jiraErr != "" {
			b.WriteString(styleWarn.Render("Error: "+m.jiraErr) + "\n")
		}
		b.WriteString("\nPress Enter to continue, Esc to go back.\n")
	case 2:
		renderJiraReposStep(b, m)
	}
}

func renderJiraReposStep(b *strings.Builder, m *setupModel) {
	if m.jiraRepoEditing {
		if m.jiraRepoField == 0 {
			b.WriteString("Repository SSH URL\n")
			b.WriteString(styleNote.Render("e.g. git@github.com:org/repo.git — SSH required for non-interactive git"))
			b.WriteString("\n\n")
			b.WriteString(m.jiraInput.View() + "\n")
			url := strings.TrimSpace(m.jiraInput.Value())
			if strings.HasPrefix(url, "https://") {
				b.WriteString(styleWarn.Render("⚠ HTTPS URL detected — SSH is required (use git@github.com:...)") + "\n")
			}
		} else {
			fmt.Fprintf(b, "Base branch for %s\n", repoNameFromURL(m.jiraRepoEditURL))
			b.WriteString(styleNote.Render("Default branch for new feature branches (default: main)"))
			b.WriteString("\n\n")
			b.WriteString(m.jiraInput.View() + "\n")
		}
		if m.jiraErr != "" {
			b.WriteString(styleWarn.Render("Error: "+m.jiraErr) + "\n")
		}
		b.WriteString("\nPress Enter to confirm, Esc to cancel.\n")
		return
	}

	b.WriteString("Repositories\n")
	b.WriteString("Use ↑/↓ to navigate, 'a' to add, 'd' to delete.\n\n")
	if len(m.jiraRepos) == 0 {
		b.WriteString(styleDim.Render("  (no repositories configured)") + "\n")
	}
	for i, repo := range m.jiraRepos {
		cursor := " "
		if i == m.jiraRepoCursor {
			cursor = ">"
		}
		name := repoNameFromURL(repo.URL)
		fmt.Fprintf(b, " %s %s  [%s]\n", cursor, name, repo.URL)
		warn := ""
		if strings.HasPrefix(repo.URL, "https://") {
			warn = "  " + styleWarn.Render("⚠ HTTPS URL — SSH recommended")
		}
		if warn != "" {
			b.WriteString(warn + "\n")
		}
	}
	if m.jiraErr != "" {
		b.WriteString("\n" + styleWarn.Render("Error: "+m.jiraErr) + "\n")
	}
	b.WriteString("\nPress Enter to continue.\n")
}

func renderJiraPhasesStep(b *strings.Builder, m *setupModel) {
	b.WriteString("Phase models\n")
	b.WriteString("Use ↑/↓ to select phase, ←/→ to change model, Tab to change provider, [e] to cycle effort, [t] to edit timeout.\n\n")

	phaseLabels := [4]string{"Validation ", "Plan       ", "Implement  ", "Review-fix "}
	for i, label := range phaseLabels {
		cursor := " "
		if i == m.jiraPhaseCursor {
			cursor = ">"
		}
		provider := m.jiraPhaseProvider[i]
		if provider == "" {
			provider = "claude"
		}
		models := jiraPhaseModelsForProvider(provider)
		modelName := ""
		if m.jiraPhaseModelIdx[i] < len(models) {
			modelName = models[m.jiraPhaseModelIdx[i]]
		}
		efforts := jiraPhaseEffortsForProvider(provider)
		effortName := "default"
		if m.jiraPhaseEffortIdx[i] < len(efforts) {
			effortName = efforts[m.jiraPhaseEffortIdx[i]]
		}
		timeout := m.jiraPhaseTimeout[i]
		if timeout == "" {
			timeout = [4]string{"2m", "5m", "30m", "20m"}[i]
		}
		fmt.Fprintf(b, " %s %-11s  %-8s  ← %s →  ← %s →  timeout=%s\n", cursor, label, provider, modelName, effortName, timeout)
	}
	if m.jiraPhaseTimeoutEdit {
		b.WriteString("\nTimeout for focused phase: ")
		b.WriteString(m.jiraPhaseTimeoutInput.View())
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(styleNote.Render("Tip: haiku is cheaper/faster for validation; sonnet for implementation."))
	b.WriteString("\n\nPress Enter to continue.\n")
}
