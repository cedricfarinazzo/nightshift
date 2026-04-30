package jira

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// CommentType identifies the kind of nightshift comment.
type CommentType string

const (
	CommentValidation   CommentType = "validation"
	CommentRejection    CommentType = "rejection"
	CommentPlan         CommentType = "plan"
	CommentImplement    CommentType = "implementation"
	CommentPR           CommentType = "pr"
	CommentRework       CommentType = "rework"
	CommentError        CommentType = "error"
	CommentStatusChange CommentType = "status_change"
)

// Title returns a human-readable phase name for the comment type.
func (ct CommentType) Title() string {
	switch ct {
	case CommentValidation:
		return "Validation"
	case CommentRejection:
		return "Rejected"
	case CommentPlan:
		return "Plan"
	case CommentImplement:
		return "Implementation"
	case CommentPR:
		return "PR Created"
	case CommentRework:
		return "Rework"
	case CommentError:
		return "Error"
	case CommentStatusChange:
		return "Status Change"
	default:
		return string(ct)
	}
}

// NightshiftComment is a structured comment posted to Jira.
type NightshiftComment struct {
	Type      CommentType
	Timestamp time.Time
	Provider  string // e.g., "claude"
	Model     string // e.g., "claude-sonnet-4.5"
	Duration  time.Duration
	Body      string            // human-readable body content only (no header/markers)
	Metadata  map[string]string // machine-parseable key-value pairs
}

// PostComment posts a formatted nightshift comment to a Jira ticket.
func (c *Client) PostComment(ctx context.Context, ticketKey string, comment NightshiftComment) error {
	body := formatComment(comment)
	return c.AddComment(ctx, ticketKey, body)
}

// ParseNightshiftComments extracts nightshift comments from raw Jira comments.
// Returns only comments posted by nightshift (identified by 🤖 prefix).
func ParseNightshiftComments(comments []Comment) []NightshiftComment {
	var out []NightshiftComment
	for _, c := range comments {
		if !strings.HasPrefix(c.Body, "🤖") {
			continue
		}
		ct, meta, ok := parseCommentMeta(c.Body)
		if !ok {
			continue
		}
		nc := NightshiftComment{
			Type:      ct,
			Timestamp: c.Created,
			Body:      extractBody(c.Body),
			Metadata:  meta,
		}
		if v, exists := meta["provider"]; exists {
			nc.Provider = v
		}
		if v, exists := meta["model"]; exists {
			nc.Model = v
		}
		if v, exists := meta["duration"]; exists {
			if d, err := time.ParseDuration(v); err == nil {
				nc.Duration = d
			}
		}
		out = append(out, nc)
	}
	return out
}

// GetLastCommentOfType finds the most recent nightshift comment of a given type.
func GetLastCommentOfType(comments []NightshiftComment, ct CommentType) *NightshiftComment {
	var last *NightshiftComment
	for i := range comments {
		if comments[i].Type != ct {
			continue
		}
		if last == nil || comments[i].Timestamp.After(last.Timestamp) {
			last = &comments[i]
		}
	}
	return last
}

var (
	reTypeLine = regexp.MustCompile(`<!-- nightshift:type=\S+((?:\s+\S+=\S+)*)\s*-->`)
	reTypeVal  = regexp.MustCompile(`<!-- nightshift:type=(\S+?)[\s>]`)
	reMeta     = regexp.MustCompile(`<!-- nightshift:meta((?:\s+\S+=\S+)+)\s*-->`)
	reKV       = regexp.MustCompile(`(\S+)=(\S+)`)
)

// parseCommentMeta extracts the comment type and metadata from HTML comment markers.
// Metadata values are URL-decoded (see formatComment for encoding).
func parseCommentMeta(body string) (CommentType, map[string]string, bool) {
	m := reTypeVal.FindStringSubmatch(body)
	if m == nil {
		return "", nil, false
	}
	ct := CommentType(m[1])

	meta := map[string]string{}
	// extract key=value pairs from the nightshift:type line (provider, model, duration)
	if tl := reTypeLine.FindStringSubmatch(body); tl != nil {
		for _, kv := range reKV.FindAllStringSubmatch(tl[1], -1) {
			if decoded, err := url.QueryUnescape(kv[2]); err == nil {
				meta[kv[1]] = decoded
			} else {
				meta[kv[1]] = kv[2]
			}
		}
	}
	// extract additional key=value pairs from the nightshift:meta line
	if mm := reMeta.FindStringSubmatch(body); mm != nil {
		for _, kv := range reKV.FindAllStringSubmatch(mm[1], -1) {
			if decoded, err := url.QueryUnescape(kv[2]); err == nil {
				meta[kv[1]] = decoded
			} else {
				meta[kv[1]] = kv[2]
			}
		}
	}
	return ct, meta, true
}

// extractBody returns only the human-readable body from a formatted nightshift comment,
// stripping the 2-line header and the trailing <!-- nightshift:* --> markers.
func extractBody(raw string) string {
	// split off everything from the first nightshift marker onward
	parts := strings.SplitN(raw, "\n\n<!-- nightshift:", 2)
	// parts[0] is: "🤖 ...\nProvider: ...\n\n{body}"
	// split header block from body on first blank line
	sections := strings.SplitN(parts[0], "\n\n", 2)
	if len(sections) < 2 {
		return ""
	}
	return strings.TrimSpace(sections[1])
}

// formatComment builds the full Jira comment string for a NightshiftComment.
// The format is plain text (no Markdown) since AddComment renders via ADF.
// Metadata values are URL-encoded so spaces and special characters are safe.
func formatComment(c NightshiftComment) string {
	var b strings.Builder
	ts := c.Timestamp.Format("2006-01-02 15:04")
	fmt.Fprintf(&b, "🤖 Nightshift — %s (%s)\n", c.Type.Title(), ts)
	fmt.Fprintf(&b, "Provider: %s | Model: %s | Duration: %s\n\n",
		c.Provider, c.Model, c.Duration.Round(time.Second))
	b.WriteString(c.Body)
	fmt.Fprintf(&b, "\n\n<!-- nightshift:type=%s provider=%s model=%s duration=%s -->\n",
		c.Type,
		url.QueryEscape(c.Provider),
		url.QueryEscape(c.Model),
		url.QueryEscape(c.Duration.Round(time.Second).String()))
	if len(c.Metadata) > 0 {
		b.WriteString("<!-- nightshift:meta")
		for k, v := range c.Metadata {
			fmt.Fprintf(&b, " %s=%s", k, url.QueryEscape(v))
		}
		b.WriteString(" -->\n")
	}
	return b.String()
}
