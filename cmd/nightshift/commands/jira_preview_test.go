package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/budget"
	"github.com/marcus/nightshift/internal/jira"
)

// ── renderJiraPreviewText ─────────────────────────────────────────────────────

func TestRenderJiraPreviewText_ConnectionOK(t *testing.T) {
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		JiraUser:     "user@example.com",
		ConnectionOK: true,
		Phases: []jiraPreviewPhase{
			{Name: "validation", Provider: "claude", Model: "claude-haiku-4-5"},
			{Name: "plan", Provider: "claude", Model: "claude-sonnet-4-6"},
			{Name: "implement", Provider: "claude", Model: "claude-sonnet-4-6"},
			{Name: "review_fix", Provider: "claude", Model: "claude-sonnet-4-6"},
		},
	}
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{})
	for _, want := range []string{"OK", "PROJ", "user@example.com", "Phase Assignments"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRenderJiraPreviewText_ConnectionFailed(t *testing.T) {
	result := &jiraPreviewResult{
		GeneratedAt:   time.Now(),
		JiraProject:   "PROJ",
		ConnectionErr: "401 Unauthorized",
		Phases:        []jiraPreviewPhase{},
	}
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{})
	if !strings.Contains(out, "FAILED") {
		t.Errorf("expected FAILED in output:\n%s", out)
	}
	if !strings.Contains(out, "401 Unauthorized") {
		t.Errorf("expected error message in output:\n%s", out)
	}
}

func TestRenderJiraPreviewText_TodoTickets(t *testing.T) {
	score := 8
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		ConnectionOK: true,
		TodoTickets: []jiraPreviewTicket{
			{
				Key:             "PROJ-1",
				Summary:         "Fix the thing",
				Status:          "À faire",
				BranchName:      "feature/PROJ-1",
				ValidationScore: &score,
			},
		},
		ExecutionOrder: []string{"PROJ-1"},
		Phases:         []jiraPreviewPhase{},
	}
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{})
	for _, want := range []string{"PROJ-1", "Fix the thing", "feature/PROJ-1", "score 8/10"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRenderJiraPreviewText_BlockedTickets(t *testing.T) {
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		ConnectionOK: true,
		ExecutionOrder: []string{"PROJ-1"},
		FullOrder: []jiraPreviewOrderEntry{
			{Key: "PROJ-1", Ready: true},
			{Key: "PROJ-2", Ready: false, Reason: "waiting for dependency", Blocker: "PROJ-1"},
		},
		BlockedTickets: []jiraPreviewBlocked{
			{Key: "PROJ-2", Reason: "waiting for dependency", Blockers: []string{"PROJ-1"}},
		},
		Phases: []jiraPreviewPhase{},
	}
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{})
	if !strings.Contains(out, "PROJ-2") {
		t.Errorf("missing blocked ticket key:\n%s", out)
	}
	if !strings.Contains(out, "PROJ-1") {
		t.Errorf("missing blocker key:\n%s", out)
	}
	if !strings.Contains(out, "blocked") {
		t.Errorf("expected 'blocked' in execution order:\n%s", out)
	}
}

func TestRenderJiraPreviewText_ReviewTickets(t *testing.T) {
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		ConnectionOK: true,
		ReviewTickets: []jiraPreviewTicket{
			{Key: "PROJ-3", Summary: "Needs rework", Status: "Revue en cours"},
		},
		Phases: []jiraPreviewPhase{},
	}
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{})
	if !strings.Contains(out, "PROJ-3") || !strings.Contains(out, "Needs rework") {
		t.Errorf("review ticket not shown:\n%s", out)
	}
}

func TestRenderJiraPreviewText_BudgetSummary(t *testing.T) {
	allowance := int64(500000)
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		ConnectionOK: true,
		Budget: &budget.AllowanceResult{
			Allowance:    allowance,
			UsedPercent:  30.5,
			BudgetSource: "calibrated",
		},
		Phases: []jiraPreviewPhase{},
	}

	// Without --explain: show summary line only.
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{Explain: false})
	if !strings.Contains(out, "30.5%") {
		t.Errorf("expected used percent in summary:\n%s", out)
	}
	if !strings.Contains(out, "calibrated") {
		t.Errorf("expected budget source in summary:\n%s", out)
	}
}

func TestRenderJiraPreviewText_BudgetExplain(t *testing.T) {
	allowance := int64(500000)
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		ConnectionOK: true,
		Budget: &budget.AllowanceResult{
			Allowance:    allowance,
			UsedPercent:  30.5,
			BudgetSource: "calibrated",
		},
		Phases: []jiraPreviewPhase{},
	}

	// With --explain: should show expanded budget section.
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{Explain: true})
	if !strings.Contains(out, "Budget") {
		t.Errorf("expected Budget section:\n%s", out)
	}
}

func TestRenderJiraPreviewText_BudgetError(t *testing.T) {
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		ConnectionOK: true,
		BudgetErr:    "db not found",
		Phases:       []jiraPreviewPhase{},
	}
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{})
	if !strings.Contains(out, "budget unavailable") {
		t.Errorf("expected budget unavailable message:\n%s", out)
	}
}

func TestRenderJiraPreviewText_SkippedTickets(t *testing.T) {
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		ConnectionOK: true,
		SkippedTickets: []jiraPreviewSkipped{
			{Key: "*", Reason: "create validation agent: binary not found"},
		},
		Phases: []jiraPreviewPhase{},
	}
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{})
	if !strings.Contains(out, "Skipped") {
		t.Errorf("expected Skipped section:\n%s", out)
	}
	if !strings.Contains(out, "binary not found") {
		t.Errorf("expected skip reason:\n%s", out)
	}
}

func TestRenderJiraPreviewText_NoTodoTickets(t *testing.T) {
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		ConnectionOK: true,
		Phases: []jiraPreviewPhase{},
	}
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{})
	if !strings.Contains(out, "none") {
		t.Errorf("expected 'none' when no todo tickets:\n%s", out)
	}
}

func TestRenderJiraPreviewText_PhaseAssignments(t *testing.T) {
	result := &jiraPreviewResult{
		GeneratedAt:  time.Now(),
		JiraProject:  "PROJ",
		ConnectionOK: true,
		Phases: []jiraPreviewPhase{
			{Name: "validation", Provider: "claude", Model: "claude-haiku-4-5", Timeout: "2m"},
			{Name: "plan", Provider: "codex", Model: "o3", Timeout: "5m"},
			{Name: "implement", Provider: "copilot", Model: "", Timeout: "30m"},
			{Name: "review_fix", Provider: "claude", Model: "claude-sonnet-4-6", Timeout: "20m"},
		},
	}
	out := renderJiraPreviewText(result, jiraPreviewTextOptions{})
	for _, want := range []string{
		"validation", "plan", "implement", "review_fix",
		"claude", "codex", "copilot",
		"claude-haiku-4-5", "o3", "claude-sonnet-4-6",
		"2m", "5m", "20m",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("phase assignments missing %q:\n%s", want, out)
		}
	}
}

// ── writeJiraPreviewJSON ──────────────────────────────────────────────────────

func TestWriteJiraPreviewJSON(t *testing.T) {
	score := 7
	now := time.Now().Truncate(time.Second)
	result := &jiraPreviewResult{
		GeneratedAt:  now,
		JiraProject:  "PROJ",
		JiraUser:     "user@example.com",
		ConnectionOK: true,
		TodoTickets: []jiraPreviewTicket{
			{Key: "PROJ-1", Summary: "Fix something", Status: "À faire", BranchName: "feature/PROJ-1", ValidationScore: &score},
		},
		ExecutionOrder: []string{"PROJ-1"},
		Phases:         []jiraPreviewPhase{{Name: "validation", Provider: "claude", Model: "claude-haiku-4-5"}},
	}

	var buf strings.Builder
	if err := writeJiraPreviewJSON(&buf, result); err != nil {
		t.Fatalf("writeJiraPreviewJSON error: %v", err)
	}

	var decoded jiraPreviewResult
	if err := json.Unmarshal([]byte(buf.String()), &decoded); err != nil {
		t.Fatalf("JSON decode error: %v\nraw:\n%s", err, buf.String())
	}
	if decoded.JiraProject != "PROJ" {
		t.Errorf("JiraProject = %q, want PROJ", decoded.JiraProject)
	}
	if len(decoded.TodoTickets) != 1 || decoded.TodoTickets[0].Key != "PROJ-1" {
		t.Errorf("unexpected TodoTickets: %+v", decoded.TodoTickets)
	}
	if decoded.TodoTickets[0].ValidationScore == nil || *decoded.TodoTickets[0].ValidationScore != 7 {
		t.Errorf("unexpected ValidationScore: %v", decoded.TodoTickets[0].ValidationScore)
	}
}

func TestWriteJiraPreviewJSON_EmptyResult(t *testing.T) {
	result := &jiraPreviewResult{
		GeneratedAt: time.Now(),
		JiraProject: "PROJ",
		Phases:      []jiraPreviewPhase{},
	}
	var buf strings.Builder
	if err := writeJiraPreviewJSON(&buf, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), `"jira_project"`) {
		t.Errorf("expected jira_project in JSON output:\n%s", buf.String())
	}
}

// ── buildJiraPreviewTicket ────────────────────────────────────────────────────

func TestBuildJiraPreviewTicket_Dependencies(t *testing.T) {
	ticket := jira.Ticket{
		Key:     "PROJ-5",
		Summary: "Do something",
		Status:  jira.Status{Name: "À faire"},
		IssueLinks: []jira.IssueLink{
			{Type: "Blocks", Direction: "inward", InwardKey: "PROJ-4"},
			{Type: "Blocks", Direction: "outward", OutwardKey: "PROJ-6"},
			{Type: "Relates", Direction: "inward", InwardKey: "PROJ-99"}, // should be ignored
		},
	}
	pt := buildJiraPreviewTicket(ticket)
	if pt.Key != "PROJ-5" {
		t.Errorf("Key = %q", pt.Key)
	}
	if len(pt.Dependencies) != 1 || pt.Dependencies[0] != "PROJ-4" {
		t.Errorf("Dependencies = %v, want [PROJ-4]", pt.Dependencies)
	}
	if len(pt.Blocks) != 1 || pt.Blocks[0] != "PROJ-6" {
		t.Errorf("Blocks = %v, want [PROJ-6]", pt.Blocks)
	}
}

func TestBuildJiraPreviewTicket_BranchName(t *testing.T) {
	ticket := jira.Ticket{Key: "PROJ-42", Summary: "something"}
	pt := buildJiraPreviewTicket(ticket)
	want := jira.BranchName("PROJ-42")
	if pt.BranchName != want {
		t.Errorf("BranchName = %q, want %q", pt.BranchName, want)
	}
}

// ── jiraPreviewCmd flags ──────────────────────────────────────────────────────

func TestJiraPreviewCmd_Flags(t *testing.T) {
	flags := []struct {
		name string
	}{
		{"project"},
		{"label"},
		{"json"},
		{"plain"},
		{"validate"},
		{"explain"},
		{"type"},
	}
	for _, f := range flags {
		if jiraPreviewCmd.Flags().Lookup(f.name) == nil {
			t.Errorf("flag --%s not registered on jira preview command", f.name)
		}
	}
}
