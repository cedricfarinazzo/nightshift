package jira

import (
	"testing"

	model "github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"
)

func TestIsReviewStatus(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"In Review", true},
		{"Revue en cours", true},
		{"QA", true},
		{"Testing", true},
		{"Validation", true},
		{"En cours", false},
		{"Done", false},
		{"À faire", false},
		{"In Progress", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isReviewStatus(tt.name)
			if got != tt.want {
				t.Errorf("isReviewStatus(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsNeedsInfoStatus(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Needs Info", true},
		{"Needs-Info", true},
		{"Need Info", true},
		{"More Info", true},
		{"more info needed", true},
		{"Waiting for Info", false},
		{"In Review", false},
		{"In Progress", false},
		{"Done", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNeedsInfoStatus(tt.name)
			if got != tt.want {
				t.Errorf("isNeedsInfoStatus(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func statusScheme(id, name, categoryKey string) *model.ProjectStatusDetailsScheme {
	return &model.ProjectStatusDetailsScheme{
		ID:   id,
		Name: name,
		StatusCategory: &model.StatusCategoryScheme{
			Key: categoryKey,
		},
	}
}

func TestBuildStatusMap_French(t *testing.T) {
	// VC project uses French status names
	statuses := []*model.ProjectStatusDetailsScheme{
		statusScheme("1", "À faire", "new"),
		statusScheme("2", "En cours", "indeterminate"),
		statusScheme("3", "Revue en cours", "indeterminate"),
		statusScheme("4", "Terminé", "done"),
	}
	sm := buildStatusMap(statuses)

	if len(sm.TodoStatuses) != 1 || sm.TodoStatuses[0].Name != "À faire" {
		t.Errorf("TodoStatuses: got %v, want [À faire]", sm.TodoStatuses)
	}
	if len(sm.InProgressStatuses) != 1 || sm.InProgressStatuses[0].Name != "En cours" {
		t.Errorf("InProgressStatuses: got %v, want [En cours]", sm.InProgressStatuses)
	}
	if len(sm.ReviewStatuses) != 1 || sm.ReviewStatuses[0].Name != "Revue en cours" {
		t.Errorf("ReviewStatuses: got %v, want [Revue en cours]", sm.ReviewStatuses)
	}
	if len(sm.DoneStatuses) != 1 || sm.DoneStatuses[0].Name != "Terminé" {
		t.Errorf("DoneStatuses: got %v, want [Terminé]", sm.DoneStatuses)
	}
	if sm.NeedsInfoStatus != nil {
		t.Errorf("NeedsInfoStatus: got %v, want nil", sm.NeedsInfoStatus)
	}
}

func TestBuildStatusMap_NoReviewStatus(t *testing.T) {
	// Minimal project without review status
	statuses := []*model.ProjectStatusDetailsScheme{
		statusScheme("1", "To Do", "new"),
		statusScheme("2", "In Progress", "indeterminate"),
		statusScheme("3", "Done", "done"),
	}
	sm := buildStatusMap(statuses)

	if len(sm.ReviewStatuses) != 0 {
		t.Errorf("ReviewStatuses: got %v, want []", sm.ReviewStatuses)
	}
	if len(sm.InProgressStatuses) != 1 {
		t.Errorf("InProgressStatuses: got %v, want [In Progress]", sm.InProgressStatuses)
	}
}

func TestBuildStatusMap_NeedsInfo(t *testing.T) {
	statuses := []*model.ProjectStatusDetailsScheme{
		statusScheme("1", "To Do", "new"),
		statusScheme("2", "Needs Info", "new"),
		statusScheme("3", "In Progress", "indeterminate"),
		statusScheme("4", "Done", "done"),
	}
	sm := buildStatusMap(statuses)

	if sm.NeedsInfoStatus == nil {
		t.Fatal("NeedsInfoStatus: got nil, want non-nil")
	}
	if sm.NeedsInfoStatus.Name != "Needs Info" {
		t.Errorf("NeedsInfoStatus.Name = %q, want %q", sm.NeedsInfoStatus.Name, "Needs Info")
	}
	// NeedsInfoStatus in "new" category should still appear in TodoStatuses
	if len(sm.TodoStatuses) != 2 {
		t.Errorf("TodoStatuses len: got %d, want 2", len(sm.TodoStatuses))
	}
}

func TestBuildStatusMap_CategoryKeys(t *testing.T) {
	statuses := []*model.ProjectStatusDetailsScheme{
		statusScheme("1", "Todo", "new"),
		statusScheme("2", "In Progress", "indeterminate"),
		statusScheme("3", "In Review", "indeterminate"),
		statusScheme("4", "Done", "done"),
	}
	sm := buildStatusMap(statuses)

	for _, s := range sm.TodoStatuses {
		if s.CategoryKey != "new" {
			t.Errorf("TodoStatus %q has CategoryKey %q, want new", s.Name, s.CategoryKey)
		}
	}
	for _, s := range sm.InProgressStatuses {
		if s.CategoryKey != "indeterminate" {
			t.Errorf("InProgressStatus %q has CategoryKey %q, want indeterminate", s.Name, s.CategoryKey)
		}
	}
	for _, s := range sm.ReviewStatuses {
		if s.CategoryKey != "indeterminate" {
			t.Errorf("ReviewStatus %q has CategoryKey %q, want indeterminate", s.Name, s.CategoryKey)
		}
	}
	for _, s := range sm.DoneStatuses {
		if s.CategoryKey != "done" {
			t.Errorf("DoneStatus %q has CategoryKey %q, want done", s.Name, s.CategoryKey)
		}
	}
}
