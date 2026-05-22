package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cedricfarinazzo/nightshift/internal/agents"
)

const validationTimeout = 2 * time.Minute

// ValidationResult holds the outcome of LLM-based ticket quality evaluation.
type ValidationResult struct {
	Valid       bool     `json:"valid"`
	Score       float64  `json:"score"`
	Issues      []string `json:"issues"`
	Missing     []string `json:"missing"`
	Suggestions []string `json:"suggestions"`
}

// ValidateTicket uses an LLM agent to evaluate whether a ticket has enough
// information for autonomous implementation. Returns a ValidationResult where
// Valid is true if the ticket meets the quality threshold (score >= 6).
// onCompress is called immediately after compression completes, before the agent spawns; nil = no callback.
func ValidateTicket(ctx context.Context, agent agents.Agent, ticket Ticket, compression *agents.CompressConfig, onCompress func(*agents.CompressStats)) (*ValidationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	opts := agents.ExecuteOptions{
		Prompt:       buildValidationContent(ticket),
		PromptPrefix: validationRolePrefix,
		PromptSuffix: validationFormatInstructions,
		Compression:  compression,
		OnCompress:   onCompress,
	}
	result, err := agent.Execute(ctx, opts)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Error
		}
		return nil, fmt.Errorf("jira: validation agent error for %s: %w%s", ticket.Key, err, stderrSuffix(stderr))
	}
	vr, err := parseValidationResponse(result.Output)
	if err != nil {
		return nil, fmt.Errorf("jira: parse validation response for %s: %w\nraw output:\n%s", ticket.Key, err, result.Output)
	}
	return vr, nil
}

// validationRolePrefix is the role/task context prepended after compression
// so the compressor cannot strip the agent's role definition.
const validationRolePrefix = `Ticket quality validator. Assess if ticket has enough info for autonomous AI implementation.

Criteria: CLEAR OBJECTIVE, SUFFICIENT CONTEXT, ACCEPTANCE CRITERIA, SCOPE, NO AMBIGUITY

`

// validationFormatInstructions is the output-format spec appended after
// compression so the compressor cannot mangle the JSON schema.
const validationFormatInstructions = `
Respond in JSON only (no markdown, no code fences):
{"valid": bool, "score": 1-10, "issues": [...], "missing": [...], "suggestions": [...]}

Valid if score >= 6 and no critical issues.`

// buildValidationContent returns only the compressible ticket data portion.
// validationRolePrefix and validationFormatInstructions are delivered via
// PromptPrefix/PromptSuffix and never pass through the compressor.
func buildValidationContent(ticket Ticket) string {
	var comments strings.Builder
	for _, c := range ticket.Comments {
		fmt.Fprintf(&comments, "- %s: %s\n", c.Author, c.Body)
	}

	return fmt.Sprintf(`Ticket: %s
Title: %s
Description: %s
Acceptance Criteria: %s
Comments:
%s`,
		ticket.Key,
		ticket.Summary,
		ticket.Description,
		ticket.AcceptanceCriteria,
		comments.String(),
	)
}

// buildValidationPrompt returns the full prompt (prefix + content + suffix).
// Used by tests and callers that do not use compression.
func buildValidationPrompt(ticket Ticket) string {
	return validationRolePrefix + buildValidationContent(ticket) + validationFormatInstructions
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

// HandleInvalidTicket transitions the ticket to the NEEDS INFO status.
// Comment posting is handled by the orchestrator via postPhaseComment(CommentRejection).
func (c *Client) HandleInvalidTicket(ctx context.Context, ticketKey string) error {
	if err := c.TransitionToNeedsInfo(ctx, ticketKey); err != nil {
		return fmt.Errorf("jira: transition invalid ticket %s to needs-info: %w", ticketKey, err)
	}
	return nil
}
