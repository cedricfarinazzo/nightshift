package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/nightshift/internal/agents"
	"github.com/marcus/nightshift/internal/logging"
)

const validationTimeout = 2 * time.Minute

// ValidationResult holds the outcome of LLM-based ticket quality evaluation.
type ValidationResult struct {
	Valid         bool                  `json:"valid"`
	Score         float64               `json:"score"`
	Issues        []string              `json:"issues"`
	Missing       []string              `json:"missing"`
	Suggestions   []string              `json:"suggestions"`
	CompressStats *agents.CompressStats `json:"compress_stats,omitempty"`
}

// ValidateTicket uses an LLM agent to evaluate whether a ticket has enough
// information for autonomous implementation. Returns a ValidationResult where
// Valid is true if the ticket meets the quality threshold (score >= 6).
func ValidateTicket(ctx context.Context, agent agents.Agent, ticket Ticket, compression *agents.CompressConfig) (*ValidationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	opts := agents.ExecuteOptions{
		Prompt:       buildValidationContent(ticket),
		PromptSuffix: validationFormatInstructions,
		Compression:  compression,
		OnCompress: func(s *agents.CompressStats) {
			// Log compression metrics immediately so they appear while the agent runs.
			logger := logging.Component("jira.validation")
			logger.Infof("validation compress %d→%d chars (-%d%%) via %s", s.OriginalLen, s.CompressedLen, s.ReductionPct, s.Provider)
		},
	}
	result, err := agent.Execute(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("jira: validation agent error for %s: %w", ticket.Key, err)
	}
	vr, err := parseValidationResponse(result.Output)
	if err != nil {
		return nil, fmt.Errorf("jira: parse validation response for %s: %w\nraw output:\n%s", ticket.Key, err, result.Output)
	}
	vr.CompressStats = result.CompressStats
	return vr, nil
}

// validationFormatInstructions is the critical output-format spec appended after
// compression so the compressor cannot mangle the JSON schema.
const validationFormatInstructions = `
Respond in JSON only (no markdown, no code fences):
{"valid": bool, "score": 1-10, "issues": [...], "missing": [...], "suggestions": [...]}

Valid if score >= 6 and no critical issues.`

// buildValidationContent returns the compressible portion of the validation prompt
// (ticket data only). validationFormatInstructions is appended separately via
// ExecuteOptions.PromptSuffix so it is never passed through the compressor.
func buildValidationContent(ticket Ticket) string {
	var comments strings.Builder
	for _, c := range ticket.Comments {
		fmt.Fprintf(&comments, "- %s: %s\n", c.Author, c.Body)
	}

	return fmt.Sprintf(`Ticket quality validator. Assess if ticket has enough info for autonomous AI implementation.

Ticket: %s
Title: %s
Description: %s
Acceptance Criteria: %s
Comments:
%s
Criteria: CLEAR OBJECTIVE, SUFFICIENT CONTEXT, ACCEPTANCE CRITERIA, SCOPE, NO AMBIGUITY`,
		ticket.Key,
		ticket.Summary,
		ticket.Description,
		ticket.AcceptanceCriteria,
		comments.String(),
	)
}

// buildValidationPrompt returns the full prompt (content + format instructions).
// Used by tests and callers that do not use compression.
func buildValidationPrompt(ticket Ticket) string {
	return buildValidationContent(ticket) + validationFormatInstructions
}

// parseValidationResponse parses the LLM output into a ValidationResult.
// Handles markdown-wrapped JSON (```json ... ```) and plain JSON.
func parseValidationResponse(output string) (*ValidationResult, error) {
	cleaned := strings.TrimSpace(output)

	// Strip markdown code fences if present
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		// Remove first line (```json or ```) and last line (```)
		if len(lines) >= 3 {
			cleaned = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	// Extract JSON object if there's surrounding text
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start >= 0 && end > start {
		cleaned = cleaned[start : end+1]
	}

	var vr ValidationResult
	if err := json.Unmarshal([]byte(cleaned), &vr); err != nil {
		return nil, fmt.Errorf("not valid json: %w", err)
	}
	// Derive Valid from Score to avoid inconsistency with the LLM's boolean.
	// A ticket is valid when score >= 6 and there are no critical issues.
	vr.Valid = vr.Score >= 6
	return &vr, nil
}

// buildValidationComment formats a validation result as a Jira comment body.
func buildValidationComment(vr *ValidationResult) string {
	var sb strings.Builder
	if vr.Valid {
		fmt.Fprintf(&sb, "✅ Nightshift — Ticket Validated\nQuality score: %.1f/10", vr.Score)
	} else {
		fmt.Fprintf(&sb, "❌ Nightshift — Ticket Rejected\nReason: Not enough information for autonomous execution.\nQuality score: %.1f/10", vr.Score)
	}
	if len(vr.Issues) > 0 {
		sb.WriteString("\n\nIssues found:\n")
		for _, issue := range vr.Issues {
			fmt.Fprintf(&sb, "• %s\n", issue)
		}
	}
	if len(vr.Missing) > 0 {
		sb.WriteString("\n\nTo fix, please add:\n")
		for _, m := range vr.Missing {
			fmt.Fprintf(&sb, "• %s\n", m)
		}
	}
	if len(vr.Suggestions) > 0 {
		sb.WriteString("\n\nSuggestions:\n")
		for _, s := range vr.Suggestions {
			fmt.Fprintf(&sb, "• %s\n", s)
		}
	}
	return sb.String()
}

// HandleInvalidTicket posts a structured rejection comment and transitions
// the ticket to the NEEDS INFO status.
func (c *Client) HandleInvalidTicket(ctx context.Context, ticketKey string, result *ValidationResult) error {
	if err := c.AddComment(ctx, ticketKey, buildValidationComment(result)); err != nil {
		return fmt.Errorf("jira: handle invalid ticket %s: %w", ticketKey, err)
	}
	if err := c.TransitionToNeedsInfo(ctx, ticketKey); err != nil {
		return fmt.Errorf("jira: transition invalid ticket %s to needs-info: %w", ticketKey, err)
	}
	return nil
}
