package jira

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/marcus/nightshift/internal/agents"
)

// stubAgent is a mock agents.Agent for unit testing.
type stubAgent struct {
	name        string
	output      string
	err         error
	capturedOpts agents.ExecuteOptions
}

func (s *stubAgent) Name() string { return s.name }
func (s *stubAgent) Execute(_ context.Context, opts agents.ExecuteOptions) (*agents.ExecuteResult, error) {
	s.capturedOpts = opts
	if s.err != nil {
		return nil, s.err
	}
	return &agents.ExecuteResult{Output: s.output}, nil
}

// ── parseValidationResponse ────────────────────────────────────────────────

func TestParseValidationResponse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
		wantScore int
		wantErr   bool
	}{
		{
			name:      "valid ticket score 8",
			input:     `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": ["Add unit tests"]}`,
			wantValid: true,
			wantScore: 8,
		},
		{
			name:      "rejected ticket score 3",
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
			name:      "markdown wrapped json",
			input:     "```json\n{\"valid\": true, \"score\": 7, \"issues\": [], \"missing\": [], \"suggestions\": []}\n```",
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
		{
			name:      "score exactly 6 is valid threshold",
			input:     `{"valid": true, "score": 6, "issues": [], "missing": [], "suggestions": []}`,
			wantValid: true,
			wantScore: 6,
		},
		{
			name:      "all fields populated",
			input:     `{"valid": false, "score": 4, "issues": ["vague objective"], "missing": ["acceptance criteria", "scope"], "suggestions": ["add AC", "add scope"]}`,
			wantValid: false,
			wantScore: 4,
		},
		{
			// LLM returns valid=false but score=7 — code must derive Valid from Score.
			name:      "llm valid false overridden by score 7",
			input:     `{"valid": false, "score": 7, "issues": [], "missing": [], "suggestions": []}`,
			wantValid: true,
			wantScore: 7,
		},
		{
			// LLM returns valid=true but score=5 — code must derive Valid from Score.
			name:      "llm valid true overridden by score 5",
			input:     `{"valid": true, "score": 5, "issues": [], "missing": [], "suggestions": []}`,
			wantValid: false,
			wantScore: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseValidationResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
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

func TestParseValidationResponse_Fields(t *testing.T) {
	input := `{"valid": false, "score": 4, "issues": ["i1", "i2"], "missing": ["m1"], "suggestions": ["s1", "s2", "s3"]}`
	got, err := parseValidationResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Issues) != 2 {
		t.Errorf("Issues len = %d, want 2", len(got.Issues))
	}
	if len(got.Missing) != 1 {
		t.Errorf("Missing len = %d, want 1", len(got.Missing))
	}
	if len(got.Suggestions) != 3 {
		t.Errorf("Suggestions len = %d, want 3", len(got.Suggestions))
	}
}

// ── buildValidationPrompt ──────────────────────────────────────────────────

func TestBuildValidationPrompt(t *testing.T) {
	ticket := Ticket{
		Key:                "PROJ-42",
		Summary:            "Add login feature",
		Description:        "Users should be able to log in with email and password.",
		AcceptanceCriteria: "User can log in with valid credentials.",
		Comments: []Comment{
			{Author: "alice", Body: "Please add OAuth support too."},
		},
	}

	prompt := buildValidationPrompt(ticket)

	for _, want := range []string{ticket.Key, ticket.Summary, ticket.Description, ticket.AcceptanceCriteria} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if !strings.Contains(prompt, "alice") {
		t.Error("prompt missing comment author")
	}
	if !strings.Contains(prompt, "OAuth support") {
		t.Error("prompt missing comment body")
	}
}

func TestBuildValidationPrompt_NoComments(t *testing.T) {
	ticket := Ticket{
		Key:         "X-1",
		Summary:     "Bare ticket",
		Description: "No comments.",
	}
	prompt := buildValidationPrompt(ticket)
	if !strings.Contains(prompt, ticket.Key) {
		t.Errorf("prompt missing ticket key")
	}
	// Should not panic or produce empty output
	if len(prompt) == 0 {
		t.Error("prompt is empty")
	}
}

// ── ValidateTicket ─────────────────────────────────────────────────────────

func TestValidateTicket_Success(t *testing.T) {
	agent := &stubAgent{
		name:   "stub",
		output: `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": []}`,
	}
	ticket := Ticket{Key: "TEST-1", Summary: "Well-defined ticket", Description: "Detailed description."}

	result, err := ValidateTicket(context.Background(), agent, ticket)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Error("expected Valid=true")
	}
	if result.Score != 8 {
		t.Errorf("Score = %d, want 8", result.Score)
	}
}

func TestValidateTicket_AgentError(t *testing.T) {
	agent := &stubAgent{
		name: "stub",
		err:  errors.New("agent timed out"),
	}
	ticket := Ticket{Key: "TEST-2", Summary: "Some ticket"}

	_, err := ValidateTicket(context.Background(), agent, ticket)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "TEST-2") {
		t.Errorf("error should reference ticket key, got: %v", err)
	}
}

func TestValidateTicket_ParseError(t *testing.T) {
	agent := &stubAgent{
		name:   "stub",
		output: "Sorry, I cannot evaluate this ticket.",
	}
	ticket := Ticket{Key: "TEST-3", Summary: "Another ticket"}

	_, err := ValidateTicket(context.Background(), agent, ticket)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestValidateTicket_InvalidTicket(t *testing.T) {
	agent := &stubAgent{
		name:   "stub",
		output: `{"valid": false, "score": 2, "issues": ["no objective", "no AC"], "missing": ["description", "acceptance criteria"], "suggestions": ["add context"]}`,
	}
	ticket := Ticket{Key: "TEST-4", Summary: "Vague ticket", Description: "Do the thing."}

	result, err := ValidateTicket(context.Background(), agent, ticket)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false for low-score ticket")
	}
	if len(result.Issues) != 2 {
		t.Errorf("Issues = %v, want 2 items", result.Issues)
	}
	if len(result.Missing) != 2 {
		t.Errorf("Missing = %v, want 2 items", result.Missing)
	}
}

// TestValidateTicket_TimeoutAppliedOnce ensures ValidateTicket does not set
// opts.Timeout alongside context.WithTimeout, which would create two nested
// deadlines racing each other (regression for VC-42).
func TestValidateTicket_TimeoutAppliedOnce(t *testing.T) {
	agent := &stubAgent{
		name:   "stub",
		output: `{"valid": true, "score": 7, "issues": [], "missing": [], "suggestions": []}`,
	}
	ticket := Ticket{Key: "TEST-5", Summary: "Timeout test ticket"}

	_, err := ValidateTicket(context.Background(), agent, ticket)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.capturedOpts.Timeout != 0 {
		t.Errorf("opts.Timeout should be zero (timeout applied via context only), got %v", agent.capturedOpts.Timeout)
	}
}
