package commands

import (
	"os"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/nightshift/internal/config"
	"github.com/spf13/viper"
)

// helpers

func newJiraModel() *setupModel {
	ti := textinput.New()
	ti.Prompt = "> "
	return &setupModel{
		cfg:               &config.Config{},
		jiraInput:         ti,
		jiraTokenEnv:      "JIRA_API_TOKEN",
		jiraLabel:         "nightshift",
		jiraMaxTickets:    10,
		jiraPhaseModelIdx: [4]int{0, 1, 1, 1},
		jiraPhaseProvider: [4]string{"claude", "claude", "claude", "claude"},
		jiraEnableCursor:  0,
	}
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func enterMsg() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter}
}

// ── Enable sub-step ───────────────────────────────────────────────────────────

func TestJiraEnableInput_Yes(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepEnable

	model, _ := m.handleJiraEnableInput(keyMsg("y"))
	got := model.(*setupModel)
	if !got.jiraEnabled {
		t.Fatal("expected jiraEnabled=true after pressing y")
	}
	if got.jiraSubStep != jiraSubStepSite {
		t.Fatalf("expected sub-step %d, got %d", jiraSubStepSite, got.jiraSubStep)
	}
}

func TestJiraEnableInput_No(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepEnable

	model, _ := m.handleJiraEnableInput(keyMsg("n"))
	got := model.(*setupModel)
	if got.jiraEnabled {
		t.Fatal("expected jiraEnabled=false after pressing n")
	}
}

func TestJiraEnableInput_EnterSelectsYes(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepEnable
	m.jiraEnableCursor = 0

	model, _ := m.handleJiraEnableInput(enterMsg())
	got := model.(*setupModel)
	if !got.jiraEnabled {
		t.Fatal("expected jiraEnabled=true when cursor=0 and Enter pressed")
	}
}

func TestJiraEnableInput_EnterSelectsNo(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepEnable
	m.jiraEnableCursor = 1

	model, _ := m.handleJiraEnableInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraEnabled {
		t.Fatal("expected jiraEnabled=false when cursor=1 and Enter pressed")
	}
}

func TestJiraEnableInput_CursorNavigation(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepEnable
	m.jiraEnableCursor = 0

	model, _ := m.handleJiraEnableInput(tea.KeyMsg{Type: tea.KeyDown})
	got := model.(*setupModel)
	if got.jiraEnableCursor != 1 {
		t.Fatalf("expected cursor 1 after Down, got %d", got.jiraEnableCursor)
	}

	model, _ = got.handleJiraEnableInput(tea.KeyMsg{Type: tea.KeyDown})
	got = model.(*setupModel)
	if got.jiraEnableCursor != 1 {
		t.Fatal("cursor should not exceed 1")
	}

	model, _ = got.handleJiraEnableInput(tea.KeyMsg{Type: tea.KeyUp})
	got = model.(*setupModel)
	if got.jiraEnableCursor != 0 {
		t.Fatalf("expected cursor 0 after Up, got %d", got.jiraEnableCursor)
	}
}

// ── Text input sub-steps ──────────────────────────────────────────────────────

func TestJiraTextInput_SiteRequired(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepSite
	m.jiraInput.SetValue("")

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraErr == "" {
		t.Fatal("expected error for empty site")
	}
}

func TestJiraTextInput_SiteStripsURL(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepSite
	m.jiraInput.SetValue("https://mysite.atlassian.net")

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraSite != "mysite" {
		t.Fatalf("expected site=mysite, got %q", got.jiraSite)
	}
	if got.jiraSubStep != jiraSubStepEmail {
		t.Fatalf("expected sub-step %d, got %d", jiraSubStepEmail, got.jiraSubStep)
	}
}

func TestJiraTextInput_SiteSubdomainOnly(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepSite
	m.jiraInput.SetValue("mysite")

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraSite != "mysite" {
		t.Fatalf("expected site=mysite, got %q", got.jiraSite)
	}
}

func TestJiraTextInput_EmailRequired(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepEmail
	m.jiraInput.SetValue("")

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraErr == "" {
		t.Fatal("expected error for empty email")
	}
}

func TestJiraTextInput_EmailAdvances(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepEmail
	m.jiraInput.SetValue("user@example.com")

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraEmail != "user@example.com" {
		t.Fatalf("expected email stored, got %q", got.jiraEmail)
	}
	if got.jiraSubStep != jiraSubStepTokenEnv {
		t.Fatalf("expected sub-step %d, got %d", jiraSubStepTokenEnv, got.jiraSubStep)
	}
}

func TestJiraTextInput_TokenEnvDefault(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepTokenEnv
	m.jiraInput.SetValue("") // empty → default applied

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraTokenEnv != "JIRA_API_TOKEN" {
		t.Fatalf("expected default JIRA_API_TOKEN, got %q", got.jiraTokenEnv)
	}
	if got.jiraSubStep != jiraSubStepProject {
		t.Fatalf("expected sub-step %d, got %d", jiraSubStepProject, got.jiraSubStep)
	}
}

func TestJiraTextInput_ProjectKeyUppercased(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepProject
	m.jiraInput.SetValue("vc")

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraProjectKey != "VC" {
		t.Fatalf("expected VC, got %q", got.jiraProjectKey)
	}
}

func TestJiraTextInput_LabelDefault(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepLabel
	m.jiraInput.SetValue("") // empty → default "nightshift"

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraLabel != "nightshift" {
		t.Fatalf("expected default 'nightshift', got %q", got.jiraLabel)
	}
	if got.jiraSubStep != jiraSubStepRepos {
		t.Fatalf("expected sub-step %d, got %d", jiraSubStepRepos, got.jiraSubStep)
	}
}

func TestJiraTextInput_MaxTicketsDefault(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepMaxTickets
	m.jiraInput.SetValue("")

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraMaxTickets != 10 {
		t.Fatalf("expected 10, got %d", got.jiraMaxTickets)
	}
	// Sub-step should advance to ping, and pinging should be true
	if got.jiraSubStep != jiraSubStepPing {
		t.Fatalf("expected sub-step %d, got %d", jiraSubStepPing, got.jiraSubStep)
	}
	if !got.jiraPinging {
		t.Fatal("expected jiraPinging=true after advancing to ping step")
	}
}

func TestJiraTextInput_MaxTicketsInvalid(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepMaxTickets
	m.jiraInput.SetValue("abc")

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraErr == "" {
		t.Fatal("expected error for non-integer max tickets")
	}
	if got.jiraSubStep != jiraSubStepMaxTickets {
		t.Fatal("should stay on max-tickets sub-step on error")
	}
}

func TestJiraTextInput_MaxTicketsZeroRejected(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepMaxTickets
	m.jiraInput.SetValue("0")

	model, _ := m.handleJiraTextInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraErr == "" {
		t.Fatal("expected error for zero max tickets")
	}
}

// ── Repo sub-step ─────────────────────────────────────────────────────────────

func TestJiraRepoInput_RequiresAtLeastOneRepo(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepRepos
	m.jiraRepos = nil

	model, _ := m.handleJiraRepoInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraErr == "" {
		t.Fatal("expected error when no repos configured")
	}
}

func TestJiraRepoInput_AddRepo(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepRepos
	m.jiraRepos = nil

	// Press 'a' to start adding
	model, _ := m.handleJiraRepoInput(keyMsg("a"))
	got := model.(*setupModel)
	if !got.jiraRepoEditing {
		t.Fatal("expected editing=true after pressing a")
	}
	if got.jiraRepoField != 0 {
		t.Fatal("expected field 0 (URL) first")
	}

	// Enter the URL
	got.jiraInput.SetValue("git@github.com:org/repo.git")
	model, _ = got.handleJiraRepoInput(enterMsg())
	got = model.(*setupModel)
	if got.jiraRepoField != 1 {
		t.Fatalf("expected field 1 (BaseBranch), got %d", got.jiraRepoField)
	}
	if got.jiraRepoEditURL != "git@github.com:org/repo.git" {
		t.Fatalf("expected URL saved, got %q", got.jiraRepoEditURL)
	}

	// Enter the base branch
	got.jiraInput.SetValue("main")
	model, _ = got.handleJiraRepoInput(enterMsg())
	got = model.(*setupModel)
	if got.jiraRepoEditing {
		t.Fatal("expected editing=false after completing repo entry")
	}
	if len(got.jiraRepos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(got.jiraRepos))
	}
	if got.jiraRepos[0].URL != "git@github.com:org/repo.git" {
		t.Fatalf("expected URL, got %q", got.jiraRepos[0].URL)
	}
	if got.jiraRepos[0].BaseBranch != "main" {
		t.Fatalf("expected branch=main, got %q", got.jiraRepos[0].BaseBranch)
	}
}

func TestJiraRepoInput_DeleteRepo(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepRepos
	m.jiraRepos = []jiraRepoEntry{
		{URL: "git@github.com:org/repo.git", BaseBranch: "main"},
	}
	m.jiraRepoCursor = 0

	model, _ := m.handleJiraRepoInput(keyMsg("d"))
	got := model.(*setupModel)
	if len(got.jiraRepos) != 0 {
		t.Fatalf("expected 0 repos after delete, got %d", len(got.jiraRepos))
	}
}

func TestJiraRepoInput_CancelEditing(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepRepos
	m.jiraRepoEditing = true
	m.jiraRepoField = 0

	model, _ := m.handleJiraRepoInput(tea.KeyMsg{Type: tea.KeyEsc})
	got := model.(*setupModel)
	if got.jiraRepoEditing {
		t.Fatal("expected editing=false after Esc")
	}
}

func TestJiraRepoInput_EnterAdvancesWithRepo(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepRepos
	m.jiraRepos = []jiraRepoEntry{
		{URL: "git@github.com:org/repo.git", BaseBranch: "main"},
	}

	model, _ := m.handleJiraRepoInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraSubStep != jiraSubStepPhases {
		t.Fatalf("expected sub-step %d, got %d", jiraSubStepPhases, got.jiraSubStep)
	}
}

// ── Phase sub-step ────────────────────────────────────────────────────────────

func TestJiraPhaseInput_Navigation(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepPhases
	m.jiraPhaseCursor = 0

	model, _ := m.handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyDown})
	got := model.(*setupModel)
	if got.jiraPhaseCursor != 1 {
		t.Fatalf("expected cursor 1, got %d", got.jiraPhaseCursor)
	}

	model, _ = got.handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyDown})
	model, _ = model.(*setupModel).handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyDown})
	model, _ = model.(*setupModel).handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyDown})
	got = model.(*setupModel)
	if got.jiraPhaseCursor != 3 {
		t.Fatal("cursor should not exceed 3")
	}
}

func TestJiraPhaseInput_ModelCycling(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepPhases
	m.jiraPhaseCursor = 0
	m.jiraPhaseModelIdx[0] = 0

	// Cycle right
	model, _ := m.handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyRight})
	got := model.(*setupModel)
	if got.jiraPhaseModelIdx[0] != 1 {
		t.Fatalf("expected model index 1, got %d", got.jiraPhaseModelIdx[0])
	}

	// Cycle left
	model, _ = got.handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyLeft})
	got = model.(*setupModel)
	if got.jiraPhaseModelIdx[0] != 0 {
		t.Fatalf("expected model index 0 after cycling back, got %d", got.jiraPhaseModelIdx[0])
	}

	// Don't go below 0
	model, _ = got.handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyLeft})
	got = model.(*setupModel)
	if got.jiraPhaseModelIdx[0] != 0 {
		t.Fatal("index should not go below 0")
	}
}

func TestJiraPhaseInput_EnterAdvances(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepPhases

	model, _ := m.handleJiraPhaseInput(enterMsg())
	got := model.(*setupModel)
	if got.jiraSubStep != jiraSubStepMaxTickets {
		t.Fatalf("expected sub-step %d, got %d", jiraSubStepMaxTickets, got.jiraSubStep)
	}
}

// ── Ping sub-step ─────────────────────────────────────────────────────────────

func TestJiraPingInput_WaitsWhilePinging(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepPing
	m.jiraPinging = true

	model, _ := m.handleJiraPingInput(enterMsg())
	got := model.(*setupModel)
	// Should stay on ping step while still pinging
	if got.jiraSubStep != jiraSubStepPing {
		t.Fatal("should stay on ping step while pinging")
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func TestRepoNameFromURL_SSH(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:org/repo.git", "repo"},
		{"git@github.com:org/nightshift.git", "nightshift"},
		{"https://github.com/org/repo.git", "repo"},
		{"https://github.com/org/repo", "repo"},
	}
	for _, tt := range tests {
		got := repoNameFromURL(tt.url)
		if got != tt.want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestJiraModelIndex_Found(t *testing.T) {
	if len(jiraPhaseModels) == 0 {
		t.Skip("jiraPhaseModels is empty")
	}
	idx := jiraModelIndex(jiraPhaseModels[0])
	if idx != 0 {
		t.Fatalf("expected 0, got %d", idx)
	}
}

func TestJiraModelIndex_NotFound(t *testing.T) {
	idx := jiraModelIndex("nonexistent-model")
	if idx != 0 {
		t.Fatalf("expected 0 (default), got %d", idx)
	}
}

// ── applyJiraConfig ───────────────────────────────────────────────────────────

func TestApplyJiraConfig_Disabled(t *testing.T) {
	m := newJiraModel()
	m.jiraEnabled = false
	m.cfg.Jira.Site = "should-be-cleared"

	m.applyJiraConfig()
	if m.cfg.Jira.Site != "" {
		t.Fatal("expected Jira config cleared when disabled")
	}
}

func TestApplyJiraConfig_PopulatesFields(t *testing.T) {
	m := newJiraModel()
	m.jiraEnabled = true
	m.jiraSite = "mysite"
	m.jiraEmail = "user@example.com"
	m.jiraTokenEnv = "MY_JIRA_TOKEN"
	m.jiraProjectKey = "PROJ"
	m.jiraLabel = "nightshift"
	m.jiraMaxTickets = 5
	m.jiraPhaseModelIdx = [4]int{0, 1, 1, 1}
	m.jiraRepos = []jiraRepoEntry{
		{URL: "git@github.com:org/repo.git", BaseBranch: "main"},
	}

	m.applyJiraConfig()

	cfg := m.cfg.Jira
	if cfg.Site != "mysite" {
		t.Errorf("Site = %q, want mysite", cfg.Site)
	}
	if cfg.Email != "user@example.com" {
		t.Errorf("Email = %q, want user@example.com", cfg.Email)
	}
	if cfg.TokenEnv != "MY_JIRA_TOKEN" {
		t.Errorf("TokenEnv = %q, want MY_JIRA_TOKEN", cfg.TokenEnv)
	}
	if cfg.MaxTickets != 5 {
		t.Errorf("MaxTickets = %d, want 5", cfg.MaxTickets)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(cfg.Projects))
	}
	if cfg.Projects[0].Key != "PROJ" {
		t.Errorf("Projects[0].Key = %q, want PROJ", cfg.Projects[0].Key)
	}
	if len(cfg.Projects[0].Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(cfg.Projects[0].Repos))
	}
	if cfg.Projects[0].Repos[0].Name != "repo" {
		t.Errorf("Repos[0].Name = %q, want repo", cfg.Projects[0].Repos[0].Name)
	}
}

// ── Token env var check ───────────────────────────────────────────────────────

func TestTokenEnvVar_Check(t *testing.T) {
	const envKey = "NIGHTSHIFT_TEST_JIRA_TOKEN_CHECK"
	os.Unsetenv(envKey)

	// Should be unset
	if os.Getenv(envKey) != "" {
		t.Fatal("expected env var to be unset")
	}

	// Set it
	os.Setenv(envKey, "test-token")
	defer os.Unsetenv(envKey)

	if os.Getenv(envKey) == "" {
		t.Fatal("expected env var to be set")
	}
}

// ── Provider cycling (Tab key) ────────────────────────────────────────────────

func TestJiraPhaseInput_ProviderCycling(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepPhases
	m.jiraPhaseCursor = 0
	m.jiraPhaseProvider[0] = "claude"
	m.jiraPhaseModelIdx[0] = 2 // non-zero to confirm reset

	// Tab should cycle to codex and reset model index.
	model, _ := m.handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyTab})
	got := model.(*setupModel)
	if got.jiraPhaseProvider[0] != "codex" {
		t.Fatalf("expected provider=codex after Tab, got %q", got.jiraPhaseProvider[0])
	}
	if got.jiraPhaseModelIdx[0] != 0 {
		t.Fatal("expected model index reset to 0 after provider change")
	}

	// Tab again: codex → copilot
	model, _ = got.handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyTab})
	got = model.(*setupModel)
	if got.jiraPhaseProvider[0] != "copilot" {
		t.Fatalf("expected provider=copilot, got %q", got.jiraPhaseProvider[0])
	}

	// Tab wraps: copilot → claude
	model, _ = got.handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyTab})
	got = model.(*setupModel)
	if got.jiraPhaseProvider[0] != "claude" {
		t.Fatalf("expected provider=claude after wrap, got %q", got.jiraPhaseProvider[0])
	}
}

func TestJiraPhaseInput_ModelBoundsPerProvider(t *testing.T) {
	m := newJiraModel()
	m.jiraSubStep = jiraSubStepPhases
	m.jiraPhaseCursor = 0
	m.jiraPhaseProvider[0] = "codex"
	maxIdx := len(jiraPhaseModelsByProvider["codex"]) - 1
	m.jiraPhaseModelIdx[0] = maxIdx

	// Right at max should not exceed bounds.
	model, _ := m.handleJiraPhaseInput(tea.KeyMsg{Type: tea.KeyRight})
	got := model.(*setupModel)
	if got.jiraPhaseModelIdx[0] != maxIdx {
		t.Fatalf("expected index capped at %d, got %d", maxIdx, got.jiraPhaseModelIdx[0])
	}
}

// ── defaultJiraPhaseProviders ─────────────────────────────────────────────────

func TestDefaultJiraPhaseProviders_Empty(t *testing.T) {
	p := defaultJiraPhaseProviders(nil)
	for i, v := range p {
		if v != "claude" {
			t.Errorf("index %d: expected claude, got %q", i, v)
		}
	}
}

func TestDefaultJiraPhaseProviders_FromPreference(t *testing.T) {
	p := defaultJiraPhaseProviders([]string{"codex"})
	for i, v := range p {
		if v != "codex" {
			t.Errorf("index %d: expected codex, got %q", i, v)
		}
	}
}

// ── writeGlobalConfigToPath clears jira when disabled ────────────────────────

func TestWriteGlobalConfigToPath_ClearsJiraWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"

	// Write a config with jira enabled first.
	cfgEnabled := &config.Config{}
	cfgEnabled.Jira.Site = "mysite"
	cfgEnabled.Jira.Email = "user@example.com"
	if err := writeGlobalConfigToPath(cfgEnabled, path); err != nil {
		t.Fatalf("write enabled: %v", err)
	}

	// Now write with jira disabled (Site is empty).
	cfgDisabled := &config.Config{}
	if err := writeGlobalConfigToPath(cfgDisabled, path); err != nil {
		t.Fatalf("write disabled: %v", err)
	}

	// Re-read via viper and verify jira site is cleared.
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if site := v.GetString("jira.site"); site != "" {
		t.Errorf("expected jira.site cleared, got %q", site)
	}
	if email := v.GetString("jira.email"); email != "" {
		t.Errorf("expected jira.email cleared, got %q", email)
	}
}
