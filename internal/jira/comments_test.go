package jira

import (
	"strings"
	"testing"
	"time"
)

func TestFormatComment(t *testing.T) {
	c := NightshiftComment{
		Type:      CommentPR,
		Timestamp: time.Date(2026, 3, 29, 2, 45, 0, 0, time.UTC),
		Provider:  "claude",
		Model:     "claude-sonnet-4.5",
		Duration:  15 * time.Minute,
		Body:      "PR: https://github.com/org/repo/pull/42",
		Metadata:  map[string]string{"pr_url": "https://github.com/org/repo/pull/42"},
	}
	body := formatComment(c)
	for _, want := range []string{
		"🤖 Nightshift — PR Created",
		"Provider: claude",
		"nightshift:type=pr",
		"nightshift:meta",
		"pr_url=",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in formatted comment", want)
		}
	}
	// URL special chars in values must be encoded
	if strings.Contains(body, "https://github.com/org/repo/pull/42 ") {
		t.Error("metadata value with URL should be encoded, not raw")
	}
}

func TestFormatComment_NoMarkdown(t *testing.T) {
	c := NightshiftComment{Type: CommentPlan, Timestamp: time.Now(), Body: "plan body"}
	body := formatComment(c)
	if strings.Contains(body, "**") {
		t.Error("formatComment must not emit Markdown bold (** renders as literal asterisks in Jira ADF)")
	}
}

func TestFormatComment_MetadataURLEncoded(t *testing.T) {
	c := NightshiftComment{
		Type:     CommentPR,
		Body:     "body",
		Metadata: map[string]string{"key": "value with spaces", "url": "https://x.com/a=b"},
	}
	body := formatComment(c)
	if strings.Contains(body, "value with spaces") {
		t.Error("space in metadata value must be URL-encoded")
	}
	if strings.Contains(body, "https://x.com/a=b ") {
		t.Error("URL in metadata value must be URL-encoded")
	}
}

func TestCommentTypeTitle(t *testing.T) {
	tests := []struct {
		ct   CommentType
		want string
	}{
		{CommentValidation, "Validation"},
		{CommentRejection, "Rejected"},
		{CommentPlan, "Plan"},
		{CommentImplement, "Implementation"},
		{CommentPR, "PR Created"},
		{CommentRework, "Rework"},
		{CommentError, "Error"},
		{CommentStatusChange, "Status Change"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		if got := tt.ct.Title(); got != tt.want {
			t.Errorf("CommentType(%q).Title() = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestParseCommentMeta(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantType CommentType
		wantOk   bool
	}{
		{
			"valid meta",
			"...<!-- nightshift:type=pr provider=claude model=sonnet duration=15m -->...",
			CommentPR, true,
		},
		{
			"no meta",
			"just a regular comment",
			"", false,
		},
		{
			"type only",
			"<!-- nightshift:type=plan -->",
			CommentPlan, true,
		},
		{
			"with nightshift:meta",
			"<!-- nightshift:type=validation provider=claude model=haiku duration=45s -->\n<!-- nightshift:meta score=8 valid=true -->",
			CommentValidation, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct, _, ok := parseCommentMeta(tt.body)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && ct != tt.wantType {
				t.Errorf("type = %q, want %q", ct, tt.wantType)
			}
		})
	}
}

func TestParseCommentMeta_ExtractsKV(t *testing.T) {
	body := "<!-- nightshift:type=validation provider=claude model=haiku duration=45s -->\n<!-- nightshift:meta score=8 valid=true -->"
	_, meta, ok := parseCommentMeta(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if meta["score"] != "8" {
		t.Errorf("meta[score] = %q, want %q", meta["score"], "8")
	}
	if meta["valid"] != "true" {
		t.Errorf("meta[valid] = %q, want %q", meta["valid"], "true")
	}
}

func TestParseCommentMeta_DecodesURLValues(t *testing.T) {
	body := "<!-- nightshift:type=pr provider=claude model=sonnet duration=1m -->\n<!-- nightshift:meta url=https%3A%2F%2Fgithub.com%2Forg%2Frepo%2Fpull%2F42 -->"
	_, meta, ok := parseCommentMeta(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := "https://github.com/org/repo/pull/42"
	if meta["url"] != want {
		t.Errorf("meta[url] = %q, want %q", meta["url"], want)
	}
}

func TestParseCommentMeta_InvalidPercentEncoding(t *testing.T) {
	// %ZZ is not valid percent-encoding; raw value must be preserved, not discarded
	body := "<!-- nightshift:type=pr provider=%ZZclaude model=sonnet duration=1m -->\n<!-- nightshift:meta url=%ZZbad -->"
	_, meta, ok := parseCommentMeta(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if meta["provider"] != "%ZZclaude" {
		t.Errorf("meta[provider] = %q, want %q", meta["provider"], "%ZZclaude")
	}
	if meta["url"] != "%ZZbad" {
		t.Errorf("meta[url] = %q, want %q", meta["url"], "%ZZbad")
	}
}

func TestGetLastCommentOfType(t *testing.T) {
	now := time.Now()
	comments := []NightshiftComment{
		{Type: CommentPlan, Timestamp: now.Add(-2 * time.Hour)},
		{Type: CommentPR, Timestamp: now.Add(-1 * time.Hour)},
		{Type: CommentPlan, Timestamp: now},
	}
	last := GetLastCommentOfType(comments, CommentPlan)
	if last == nil {
		t.Fatal("expected non-nil result")
	} else if last.Timestamp.Before(now.Add(-30 * time.Minute)) {
		t.Error("should return the most recent plan comment")
	}
}

func TestGetLastCommentOfType_NotFound(t *testing.T) {
	comments := []NightshiftComment{
		{Type: CommentPlan, Timestamp: time.Now()},
	}
	if got := GetLastCommentOfType(comments, CommentPR); got != nil {
		t.Errorf("expected nil for missing type, got %+v", got)
	}
}

func TestExtractBody(t *testing.T) {
	raw := "🤖 Nightshift — Plan (2026-03-29 02:00)\nProvider: claude | Model: haiku | Duration: 1m\n\nplan body content\n\n<!-- nightshift:type=plan provider=claude model=haiku duration=1m -->"
	got := extractBody(raw)
	if got != "plan body content" {
		t.Errorf("extractBody = %q, want %q", got, "plan body content")
	}
}

func TestParseNightshiftComments(t *testing.T) {
	now := time.Now()
	raw := []Comment{
		{
			Body:    "just a human comment",
			Created: now,
		},
		{
			Body:    "🤖 Nightshift — Plan (2026-03-29 02:00)\nProvider: claude | Model: haiku | Duration: 1m\n\nbody\n\n<!-- nightshift:type=plan provider=claude model=haiku duration=1m -->",
			Created: now,
		},
		{
			Body:    "🤖 no meta marker here",
			Created: now,
		},
	}
	result := ParseNightshiftComments(raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 nightshift comment, got %d", len(result))
	}
	if result[0].Type != CommentPlan {
		t.Errorf("type = %q, want %q", result[0].Type, CommentPlan)
	}
	if result[0].Provider != "claude" {
		t.Errorf("provider = %q, want %q", result[0].Provider, "claude")
	}
	if result[0].Duration != time.Minute {
		t.Errorf("duration = %v, want %v", result[0].Duration, time.Minute)
	}
	if result[0].Body != "body" {
		t.Errorf("body = %q, want %q", result[0].Body, "body")
	}
}
