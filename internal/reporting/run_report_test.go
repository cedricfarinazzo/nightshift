package reporting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/usage"
)

func makeResults(tasks []TaskResult) *RunResults {
	now := time.Date(2024, 6, 1, 22, 0, 0, 0, time.UTC)
	return &RunResults{
		Date:      now,
		StartTime: now,
		EndTime:   now.Add(30 * time.Minute),
		Tasks:     tasks,
	}
}

func TestRenderRunReport_Basic(t *testing.T) {
	r := makeResults([]TaskResult{
		{Project: "/proj", TaskType: "lint-fix", Title: "Fixed lint", Status: "completed"},
	})
	out, err := RenderRunReport(r, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "# Nightshift Run") {
		t.Error("missing header")
	}
	if !strings.Contains(out, "Tasks Completed") {
		t.Error("missing completed section")
	}
	if !strings.Contains(out, "lint-fix") {
		t.Error("missing task type")
	}
}

func TestRenderRunReport_NilResults(t *testing.T) {
	_, err := RenderRunReport(nil, "")
	if err == nil {
		t.Error("expected error for nil results")
	}
}

func TestRenderRunReport_WithLogPath(t *testing.T) {
	r := makeResults(nil)
	out, err := RenderRunReport(r, "/var/log/nightshift.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "/var/log/nightshift.log") {
		t.Error("log path not in output")
	}
}

func TestRenderRunReport_AllStatuses(t *testing.T) {
	r := makeResults([]TaskResult{
		{Project: "/a", TaskType: "lint-fix", Title: "ok", Status: "completed", TokensUsed: 1000, Duration: time.Minute},
		{Project: "/b", TaskType: "security-footgun", Title: "bad", Status: "failed"},
		{Project: "/c", TaskType: "doc-drift", Title: "skip", Status: "skipped", SkipReason: "budget"},
	})
	out, err := RenderRunReport(r, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Tasks Completed") {
		t.Error("missing completed section")
	}
	if !strings.Contains(out, "Tasks Failed") {
		t.Error("missing failed section")
	}
	if !strings.Contains(out, "Tasks Skipped") {
		t.Error("missing skipped section")
	}
	if !strings.Contains(out, "Skip reason: budget") {
		t.Error("missing skip reason")
	}
}

func TestRenderRunReport_WithOutputRef(t *testing.T) {
	r := makeResults([]TaskResult{
		{Project: "/p", TaskType: "lint-fix", Title: "t", Status: "completed", OutputRef: "https://github.com/org/repo/pull/42"},
	})
	out, _ := RenderRunReport(r, "")
	if !strings.Contains(out, "https://github.com/org/repo/pull/42") {
		t.Error("output ref not in report")
	}
}

func TestRenderRunReport_WithCostSnapshots(t *testing.T) {
	budget := 100.0
	used := 20.0
	remaining := 80.0
	overageRemaining := 5.0
	r := makeResults(nil)
	r.CostSnapshots = []usage.CostSnapshot{
		{Provider: "anthropic", TotalBudget: &budget, Used: &used, Remaining: &remaining},
		{Provider: "codex", Remaining: &remaining},
		{Provider: "copilot", TotalBudget: &budget, Remaining: &overageRemaining, OverageCount: 3},
	}
	out, err := RenderRunReport(r, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Cost Summary") {
		t.Error("missing cost section")
	}
	if !strings.Contains(out, "$20.00") {
		t.Error("missing anthropic used")
	}
	if !strings.Contains(out, "credits") {
		t.Error("missing codex credits label")
	}
	if !strings.Contains(out, "3 overage") {
		t.Error("missing copilot overage count")
	}
}

func TestRenderRunReport_EmptyCostSnapshotFields(t *testing.T) {
	r := makeResults(nil)
	r.CostSnapshots = []usage.CostSnapshot{
		{Provider: "anthropic"}, // all nil pointers
		{Provider: "copilot"},   // zero overage
	}
	out, err := RenderRunReport(r, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Cost Summary") {
		t.Error("missing cost section")
	}
	if !strings.Contains(out, "0 overage") {
		t.Error("expected '0 overage' for copilot with no overages")
	}
}

func TestDefaultRunReportPath(t *testing.T) {
	ts := time.Date(2024, 6, 1, 22, 30, 0, 0, time.UTC)
	p := DefaultRunReportPath(ts)
	if !strings.HasSuffix(p, "run-2024-06-01-223000.md") {
		t.Errorf("unexpected path format: %s", p)
	}
	if !strings.Contains(p, "reports") {
		t.Error("path should be under reports dir")
	}
}

func TestSaveRunReport_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.md")
	r := makeResults([]TaskResult{
		{Project: "/proj", TaskType: "lint-fix", Title: "done", Status: "completed"},
	})
	if err := SaveRunReport(r, path, "/log/path"); err != nil {
		t.Fatalf("SaveRunReport error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if !strings.Contains(string(data), "Nightshift Run") {
		t.Error("saved file missing header")
	}
}

func TestSaveRunReport_NilResults(t *testing.T) {
	dir := t.TempDir()
	err := SaveRunReport(nil, filepath.Join(dir, "run.md"), "")
	if err == nil {
		t.Error("expected error for nil results")
	}
}

func TestSaveRunReport_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c", "run.md")
	r := makeResults(nil)
	if err := SaveRunReport(r, nested, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("file not created: %v", err)
	}
}
