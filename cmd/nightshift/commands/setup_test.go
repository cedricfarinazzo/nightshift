package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cedricfarinazzo/nightshift/internal/config"
	"github.com/cedricfarinazzo/nightshift/internal/setup"
	"github.com/cedricfarinazzo/nightshift/internal/tasks"
)

func TestApplyBudgetEdit_MaxPercentBounds(t *testing.T) {
	m := &setupModel{
		cfg:         &config.Config{},
		budgetInput: textinput.New(),
	}

	m.budgetCursor = 0
	m.budgetInput.SetValue("101")
	if err := m.applyBudgetEdit(); err == nil {
		t.Fatal("expected max_percent > 100 to fail")
	}

	m.budgetInput.SetValue("100")
	if err := m.applyBudgetEdit(); err != nil {
		t.Fatalf("expected max_percent=100 to pass: %v", err)
	}
}

func TestHandleProjectsInput_RejectsFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	m := &setupModel{
		projectEditing: true,
		projectInput:   textinput.New(),
	}
	m.projectInput.SetValue(filePath)

	model, _ := m.handleProjectsInput(tea.KeyMsg{Type: tea.KeyEnter})
	got := model.(*setupModel)
	if got.projectErr != "path must be a directory" {
		t.Fatalf("projectErr = %q, want %q", got.projectErr, "path must be a directory")
	}
}

func TestHandleTaskInput_RequiresSelection(t *testing.T) {
	m := &setupModel{
		taskItems: []taskItem{
			{
				def:      tasks.TaskDefinition{Type: tasks.TaskType("unit-test-task")},
				selected: false,
			},
		},
	}

	model, cmd := m.handleTaskInput(tea.KeyMsg{Type: tea.KeyEnter})
	got := model.(*setupModel)
	if cmd != nil {
		t.Fatal("expected no transition cmd when no tasks selected")
	}
	if got.taskErr != "select at least one task" {
		t.Fatalf("taskErr = %q, want %q", got.taskErr, "select at least one task")
	}
}

func TestHandleTaskInput_NoTasksDoesNotPanic(t *testing.T) {
	m := &setupModel{}
	if _, _ = m.handleTaskInput(tea.KeyMsg{Type: tea.KeySpace}); m.taskErr != "" {
		t.Fatalf("taskErr = %q, want empty for non-enter input", m.taskErr)
	}
}

func TestHandleTaskInput_ViewportOffsetClamps(t *testing.T) {
	// Create a model with many tasks and simulate cursor navigation
	// to verify viewport offset advances and clamps correctly.
	m := &setupModel{
		windowHeight: 30, // terminal height
		taskItems:    make([]taskItem, 0, 50),
	}

	// Create 50 dummy tasks
	for i := 0; i < 50; i++ {
		m.taskItems = append(m.taskItems, taskItem{
			def:      tasks.TaskDefinition{Type: tasks.TaskType("task-" + fmt.Sprintf("%02d", i))},
			selected: false,
		})
	}

	// calculateTaskViewportHeight should return ~22 (30 - 8 reserved)
	vh := m.calculateTaskViewportHeight()
	if vh < 5 {
		t.Fatalf("calculateTaskViewportHeight = %d, want >= 5", vh)
	}

	// Initial state: cursor at 0, viewport starts at 0
	if m.taskViewportOffset != 0 {
		t.Fatalf("initial taskViewportOffset = %d, want 0", m.taskViewportOffset)
	}

	// Move down past the viewport: cursor goes beyond offset+vh
	for i := 0; i < vh+5; i++ {
		m.handleTaskInput(tea.KeyMsg{Type: tea.KeyDown})
	}

	// After moving past viewport, offset should advance to keep cursor visible
	expectedOffset := m.taskCursor - vh + 1
	if m.taskViewportOffset != expectedOffset {
		t.Fatalf("after moving down past viewport, offset = %d, want %d", m.taskViewportOffset, expectedOffset)
	}

	// Move back up to position 5: offset should clamp down when cursor reaches viewport start
	for i := 0; i < vh; i++ { // Move back to position vh+5-vh = 5
		m.handleTaskInput(tea.KeyMsg{Type: tea.KeyUp})
	}

	// At cursor position 5, offset should clamp to 5 (since 5 < offset)
	if m.taskViewportOffset != m.taskCursor {
		t.Fatalf("after moving up to top of view, offset = %d, want %d (cursor position)", m.taskViewportOffset, m.taskCursor)
	}
}

func TestEnsurePathInShell_SubstringDoesNotBlockInsert(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".zshrc")
	if err := os.WriteFile(cfgPath, []byte("export PATH=\"$PATH:/opt/bin2\"\n"), 0644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	changed, err := ensurePathInShell(cfgPath, "zsh", "/opt/bin")
	if err != nil {
		t.Fatalf("ensurePathInShell: %v", err)
	}
	if !changed {
		t.Fatal("expected config to change when only substring path exists")
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !shellConfigHasPath(string(data), "/opt/bin") {
		t.Fatal("expected new path token to be present")
	}
}

func TestEnsurePathInShell_ExactPathNoChange(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".zshrc")
	if err := os.WriteFile(cfgPath, []byte("export PATH=\"$PATH:/opt/bin\"\n"), 0644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	changed, err := ensurePathInShell(cfgPath, "zsh", "/opt/bin")
	if err != nil {
		t.Fatalf("ensurePathInShell: %v", err)
	}
	if changed {
		t.Fatal("expected no change when exact path token exists")
	}
}

func TestMakeTaskItems_UsesSortedDefinitions(t *testing.T) {
	cfg := &config.Config{}
	items := makeTaskItems(cfg, nil, setup.PresetBalanced)
	defs := tasks.AllDefinitionsSorted()

	if len(items) != len(defs) {
		t.Fatalf("len(items)=%d len(defs)=%d", len(items), len(defs))
	}

	for i := range defs {
		if items[i].def.Type != defs[i].Type {
			t.Fatalf("item[%d].Type=%q want %q", i, items[i].def.Type, defs[i].Type)
		}
	}
}

func TestMakeTaskItems_PreservesExplicitEnabledTasks(t *testing.T) {
	cfg := &config.Config{
		Tasks: config.TasksConfig{
			Enabled: []string{string(tasks.TaskBugFinder)},
		},
	}

	items := makeTaskItems(cfg, nil, setup.PresetBalanced)
	found := false
	for _, item := range items {
		if item.def.Type != tasks.TaskBugFinder {
			continue
		}
		found = true
		if !item.selected {
			t.Fatal("expected explicitly enabled task to remain selected")
		}
	}
	if !found {
		t.Fatal("expected bug-finder task to exist in setup list")
	}
}

func TestMakeTaskItems_ExcludesPresetWhenEnabledNonEmpty(t *testing.T) {
	// When cfg.Tasks.Enabled is non-empty, preset selections must be ignored.
	// Only the explicitly enabled tasks should be selected.
	cfg := &config.Config{
		Tasks: config.TasksConfig{
			// Enable only TaskBugFinder, which may not be in the preset
			Enabled: []string{string(tasks.TaskBugFinder)},
		},
	}

	items := makeTaskItems(cfg, nil, setup.PresetBalanced)

	// Verify only the explicitly enabled task is selected
	selectedCount := 0
	for _, item := range items {
		if item.selected {
			selectedCount++
			if item.def.Type != tasks.TaskBugFinder {
				t.Fatalf("expected only TaskBugFinder to be selected, but %s is also selected", item.def.Type)
			}
		}
	}

	if selectedCount != 1 {
		t.Fatalf("expected exactly 1 selected task, got %d", selectedCount)
	}
}

func TestWriteGlobalConfig_ProviderYAMLKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Seed an initial config so WriteConfig can overwrite it.
	if err := os.WriteFile(cfgPath, []byte("# nightshift config\n"), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cfg := &config.Config{}
	cfg.Providers.Claude.Enabled = true
	cfg.Providers.Claude.DataPath = "/tmp/claude-data"
	cfg.Providers.Claude.DangerouslySkipPermissions = true
	cfg.Providers.Claude.DangerouslyBypassApprovalsAndSandbox = true
	cfg.Providers.Codex.DataPath = "/tmp/codex-data"

	if err := writeGlobalConfigToPath(cfg, cfgPath); err != nil {
		t.Fatalf("writeGlobalConfigToPath: %v", err)
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(raw)

	// Must use snake_case mapstructure tag names, not lowercased Go field names.
	wantKeys := []string{
		"data_path",
		"dangerously_skip_permissions",
		"dangerously_bypass_approvals_and_sandbox",
	}
	for _, key := range wantKeys {
		if !containsStr(content, key) {
			t.Errorf("expected YAML key %q in config output, got:\n%s", key, content)
		}
	}

	// Must NOT contain the lowercased Go field name variants.
	badKeys := []string{
		"datapath",
		"dangerouslyskippermissions",
		"dangerouslybypassapprovalsandsandbox",
	}
	for _, key := range badKeys {
		if containsStr(content, key) {
			t.Errorf("found incorrect lowercased key %q in config output, got:\n%s", key, content)
		}
	}
}

func TestEffortIndex(t *testing.T) {
	tests := []struct {
		efforts []string
		value   string
		want    int
	}{
		{claudeEfforts, "", 0},
		{claudeEfforts, "default", 0},
		{claudeEfforts, "low", 1},
		{claudeEfforts, "max", 5},
		{claudeEfforts, "unknown", 0},
		{codexEfforts, "none", 1},
		{codexEfforts, "minimal", 2},
		{copilotEfforts, "xhigh", 4},
	}
	for _, tt := range tests {
		got := effortIndex(tt.efforts, tt.value)
		if got != tt.want {
			t.Errorf("effortIndex(%v, %q) = %d, want %d", tt.efforts, tt.value, got, tt.want)
		}
	}
}

func TestEffortValue(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"default", ""},
		{"", ""},
		{"high", "high"},
		{"max", "max"},
	}
	for _, tt := range tests {
		if got := effortValue(tt.in); got != tt.want {
			t.Errorf("effortValue(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHandleModelInput_ECyclesEffort(t *testing.T) {
	m := &setupModel{cfg: &config.Config{}, modelCursor: 0}

	// e advances effort index
	m.handleModelInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if m.claudeEffortIdx != 1 {
		t.Errorf("claudeEffortIdx after e = %d, want 1", m.claudeEffortIdx)
	}

	// e wraps around at end
	m.claudeEffortIdx = len(claudeEfforts) - 1
	m.handleModelInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if m.claudeEffortIdx != 0 {
		t.Errorf("claudeEffortIdx after wrap = %d, want 0", m.claudeEffortIdx)
	}
}

func TestHandleModelInput_EOnlyAffectsFocusedRow(t *testing.T) {
	m := &setupModel{cfg: &config.Config{}, modelCursor: 1} // codex row

	m.handleModelInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	if m.claudeEffortIdx != 0 {
		t.Errorf("claude effort changed unexpectedly: %d", m.claudeEffortIdx)
	}
	if m.codexEffortIdx != 1 {
		t.Errorf("codex effort = %d, want 1", m.codexEffortIdx)
	}
	if m.copilotEffortIdx != 0 {
		t.Errorf("copilot effort changed unexpectedly: %d", m.copilotEffortIdx)
	}
}

func TestHandleModelInput_LeftRightAlwaysNavigatesModel(t *testing.T) {
	m := &setupModel{cfg: &config.Config{}, modelCursor: 0, claudeModelIdx: 2}

	m.handleModelInput(tea.KeyMsg{Type: tea.KeyRight})
	if m.claudeModelIdx != 3 {
		t.Errorf("claudeModelIdx after right = %d, want 3", m.claudeModelIdx)
	}

	m.handleModelInput(tea.KeyMsg{Type: tea.KeyLeft})
	if m.claudeModelIdx != 2 {
		t.Errorf("claudeModelIdx after left = %d, want 2", m.claudeModelIdx)
	}
}

func TestHandleModelInput_EnterSavesEffort(t *testing.T) {
	m := &setupModel{
		cfg:              &config.Config{},
		claudeEffortIdx:  3, // "high"
		codexEffortIdx:   4, // "medium"
		copilotEffortIdx: 1, // "low"
	}

	m.handleModelInput(tea.KeyMsg{Type: tea.KeyEnter})

	if m.cfg.Providers.Claude.ReasoningEffort != "high" {
		t.Errorf("claude effort = %q, want high", m.cfg.Providers.Claude.ReasoningEffort)
	}
	if m.cfg.Providers.Codex.ReasoningEffort != "medium" {
		t.Errorf("codex effort = %q, want medium", m.cfg.Providers.Codex.ReasoningEffort)
	}
	if m.cfg.Providers.Copilot.ReasoningEffort != "low" {
		t.Errorf("copilot effort = %q, want low", m.cfg.Providers.Copilot.ReasoningEffort)
	}
}

func TestHandleModelInput_EnterDefaultEffortMapsToEmpty(t *testing.T) {
	m := &setupModel{
		cfg:             &config.Config{},
		claudeEffortIdx: 0, // "default"
	}

	m.handleModelInput(tea.KeyMsg{Type: tea.KeyEnter})

	if m.cfg.Providers.Claude.ReasoningEffort != "" {
		t.Errorf("claude effort = %q, want empty string for default", m.cfg.Providers.Claude.ReasoningEffort)
	}
}

func TestWriteGlobalConfig_ReasoningEffortKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(cfgPath, []byte("# nightshift config\n"), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cfg := &config.Config{}
	cfg.Providers.Claude.ReasoningEffort = "high"
	cfg.Providers.Codex.ReasoningEffort = "medium"
	cfg.Providers.Copilot.ReasoningEffort = "low"

	if err := writeGlobalConfigToPath(cfg, cfgPath); err != nil {
		t.Fatalf("writeGlobalConfigToPath: %v", err)
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(raw)

	if !containsStr(content, "reasoning_effort") {
		t.Errorf("expected reasoning_effort in config output, got:\n%s", content)
	}
	if !containsStr(content, "high") {
		t.Errorf("expected effort value 'high' in config output, got:\n%s", content)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
