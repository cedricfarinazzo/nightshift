package jira

import (
	"strings"
	"testing"
)

func TestParseValidationResponse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
		wantScore int
		wantErr   bool
	}{
		{
			name:      "valid ticket with score 8",
			input:     `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": ["Add unit tests"]}`,
			wantValid: true,
			wantScore: 8,
		},
		{
			name:      "invalid rejected ticket with score 3",
			input:     `{"valid": false, "score": 3, "issues": ["No acceptance criteria"], "missing": ["Expected behavior", "Definition of done"], "suggestions": []}`,
			wantValid: false,
			wantScore: 3,
		},
		{
			name:    "malformed not valid json",
			input:   `this is not json at all`,
			wantErr: true,
		},
		{
			name: "markdown code block wrapped json",
			input: "```json\n{\"valid\": true, \"score\": 7, \"issues\": [], \"missing\": [], \"suggestions\": []}\n```",
			wantValid: true,
			wantScore: 7,
		},
		{
			name:      "plain code block without language tag",
			input:     "```\n{\"valid\": false, \"score\": 2, \"issues\": [\"unclear\"], \"missing\": [], \"suggestions\": []}\n```",
			wantValid: false,
			wantScore: 2,
		},
		{
			name:      "json with surrounding text",
			input:     "Here is the evaluation:\n{\"valid\": true, \"score\": 9, \"issues\": [], \"missing\": [], \"suggestions\": []}\nEnd.",
			wantValid: true,
			wantScore: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseValidationResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Valid != tt.wantValid {
				t.Errorf("Valid = %v, want %v", got.Valid, tt.wantValid)
			}
			if got.Score != tt.wantScore {
				t.Errorf("Score = %d, want %d", got.Score, tt.wantScore)
			}
		})
	}
}

func TestBuildValidationPrompt(t *testing.T) {
	ticket := Ticket{
		Key:         "PROJ-42",
		Summary:     "Add login feature",
		Description: "Users should be able to log in with email and password.",
		Comments: []Comment{
			{Author: "alice", Body: "Please add OAuth support too."},
		},
	}

	prompt := buildValidationPrompt(ticket)

	for _, want := range []string{ticket.Key, ticket.Summary, ticket.Description} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
