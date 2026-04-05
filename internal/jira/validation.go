package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/nightshift/internal/agents"
)

const validationTimeout = 2 * time.Minute

// ValidationResult holds the outcome of LLM-based ticket quality evaluation.
type ValidationResult struct {
	Valid       bool     `json:"valid"`
	Score       int      `json:"score"`
	Issues      []string `json:"issues"`
	Missing     []string `json:"missing"`
	Suggestions []string `json:"suggestions"`
}

// ValidateTicket uses an LLM agent to evaluate whether a ticket has enough
// information for autonomous implementation. Returns a ValidationResult where
// Valid is true if the ticket meets the quality threshold (score >= 6).
func ValidateTicket(ctx context.Context, agent agents.Agent, ticket Ticket) (*ValidationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	opts := agents.ExecuteOptions{
		Prompt:  buildValidationPrompt(ticket),
		Timeout: validationTimeout,
	}
	result, err := agent.Execute(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("jira: validation agent error for %s: %w", ticket.Key, err)
	}
	vr, err := parseValidationResponse(result.Output)
	if err != nil {
		return nil, fmt.Errorf("jira: parse validation response for %s: %w", ticket.Key, err)
	}
	return vr, nil
}

// buildValidationPrompt constructs the prompt sent to the LLM validator.
func buildValidationPrompt(ticket Ticket) string {
	var comments strings.Builder
	for _, c := range ticket.Comments {
		comments.WriteString(fmt.Sprintf("- %s: %s\n", c.Author, c.Body))
	}

	return fmt.Sprintf(`You are a ticket quality validator for an autonomous coding system.
Evaluate whether this Jira ticket has enough information for an AI agent to implement it autonomously.

Ticket: %s
Title: %s
Description: %s
Comments:
%s
Evaluate: CLEAR OBJECTIVE, SUFFICIENT CONTEXT, ACCEPTANCE CRITERIA, SCOPE, NO AMBIGUITY

Respond in JSON only (no markdown, no code fences):
{"valid": bool, "score": 1-10, "issues": [...], "missing": [...], "suggestions": [...]}

A ticket is valid if score >= 6 and has no critical issues.`,
		ticket.Key,
		ticket.Summary,
		ticket.Description,
		comments.String(),
	)
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
	return &vr, nil
}

// HandleInvalidTicket posts a structured rejection comment and transitions
// the ticket to the NEEDS INFO status.
func (c *Client) HandleInvalidTicket(ctx context.Context, ticketKey string, result *ValidationResult) error {
	var sb strings.Builder
	sb.WriteString("❌ **Nightshift — Ticket Rejected**\n")
	sb.WriteString("**Reason**: Not enough information for autonomous execution.\n\n")

	if len(result.Issues) > 0 {
		sb.WriteString("**Issues found:**\n")
		for _, issue := range result.Issues {
			sb.WriteString(fmt.Sprintf("- %s\n", issue))
		}
		sb.WriteString("\n")
	}

	if len(result.Missing) > 0 {
		sb.WriteString("**To fix, please add:**\n")
		for _, m := range result.Missing {
			sb.WriteString(fmt.Sprintf("- %s\n", m))
		}
		sb.WriteString("\n")
	}

	if len(result.Suggestions) > 0 {
		sb.WriteString("**Suggestions:**\n")
		for _, s := range result.Suggestions {
			sb.WriteString(fmt.Sprintf("- %s\n", s))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("**Quality score:** %d/10", result.Score))

	if err := c.AddComment(ctx, ticketKey, sb.String()); err != nil {
		return fmt.Errorf("jira: handle invalid ticket %s: %w", ticketKey, err)
	}
	if err := c.TransitionToNeedsInfo(ctx, ticketKey); err != nil {
		return fmt.Errorf("jira: transition invalid ticket %s to needs-info: %w", ticketKey, err)
	}
	return nil
}
