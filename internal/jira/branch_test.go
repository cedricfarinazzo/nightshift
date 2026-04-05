package jira

import (
	"testing"
)

func TestBranchName(t *testing.T) {
	tests := []struct {
		ticketKey string
		want      string
	}{
		{"PROJ-123", "feature/PROJ-123"},
		{"VC-7", "feature/VC-7"},
		{"VC-1", "feature/VC-1"},
	}
	for _, tt := range tests {
		got := BranchName(tt.ticketKey)
		if got != tt.want {
			t.Errorf("BranchName(%q) = %q, want %q", tt.ticketKey, got, tt.want)
		}
	}
}

func TestCommitMessage(t *testing.T) {
	tests := []struct {
		ticketKey   string
		scope       string
		description string
		want        string
	}{
		{"VC-7", "api", "add login endpoint", "feat(api): VC-7: add login endpoint"},
		{"VC-7", "", "add login endpoint", "feat: VC-7: add login endpoint"},
		{"PROJ-123", "auth", "implement OAuth", "feat(auth): PROJ-123: implement OAuth"},
	}
	for _, tt := range tests {
		got := CommitMessage(tt.ticketKey, tt.scope, tt.description)
		if got != tt.want {
			t.Errorf("CommitMessage(%q, %q, %q) = %q, want %q",
				tt.ticketKey, tt.scope, tt.description, got, tt.want)
		}
	}
}

func TestPRTitle(t *testing.T) {
	tests := []struct {
		ticketKey   string
		scope       string
		description string
		want        string
	}{
		{"VC-7", "api", "add workspace support", "feat(api): VC-7: add workspace support"},
		{"VC-7", "", "add workspace support", "feat: VC-7: add workspace support"},
	}
	for _, tt := range tests {
		got := PRTitle(tt.ticketKey, tt.scope, tt.description)
		if got != tt.want {
			t.Errorf("PRTitle(%q, %q, %q) = %q, want %q",
				tt.ticketKey, tt.scope, tt.description, got, tt.want)
		}
	}
}
