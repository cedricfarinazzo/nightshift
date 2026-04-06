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
		"🤖 **Nightshift — PR Created**",
		"Provider: claude",
		"nightshift:type=pr",
		"nightshift:meta",
		"pr_url=",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in formatted comment", want)
		}
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
	}
	if last.Timestamp.Before(now.Add(-30 * time.Minute)) {
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

func TestParseNightshiftComments(t *testing.T) {
	now := time.Now()
	raw := []Comment{
		{
			Body:    "just a human comment",
			Created: now,
		},
		{
			Body:    "🤖 **Nightshift — Plan** (2026-03-29 02:00)\nProvider: claude | Model: haiku | Duration: 1m\n\nbody\n\n<!-- nightshift:type=plan provider=claude model=haiku duration=1m -->",
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
}
