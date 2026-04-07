package jira

// mockJiraServer creates a fake Jira REST API server for unit testing.
// It covers the endpoints used by Client methods, enabling full coverage without
// a live Jira connection.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	atlassianjira "github.com/ctreminiom/go-atlassian/v2/jira/v3"
)

// mockServerConfig controls which endpoints succeed and which fail.
type mockServerConfig struct {
	myselfStatus      int
	statusesPayload   string // JSON for project statuses
	commentStatus     int
	transitionsGet    string // JSON for GET transitions
	transitionsPost   int    // HTTP status for POST transitions (move)
	searchPayload     string // JSON for POST /rest/api/3/search
	failTransitions   bool   // force GET transitions to return error
}

func defaultMockConfig() mockServerConfig {
	return mockServerConfig{
		myselfStatus: http.StatusOK,
		// Project statuses: array of ProjectStatusPageScheme (one per issue type),
		// each containing a nested "statuses" array of ProjectStatusDetailsScheme.
		statusesPayload: `[{"id":"1","name":"Story","subtask":false,"statuses":[
			{"id":"10001","name":"To Do","statusCategory":{"key":"new"}},
			{"id":"10002","name":"In Progress","statusCategory":{"key":"indeterminate"}},
			{"id":"10003","name":"In Review","statusCategory":{"key":"indeterminate"}},
			{"id":"10004","name":"Done","statusCategory":{"key":"done"}}
		]}]`,
		commentStatus: http.StatusCreated,
		transitionsGet: `{"transitions":[
			{"id":"21","name":"Start","to":{"id":"10002","statusCategory":{"key":"indeterminate"}}},
			{"id":"31","name":"Review","to":{"id":"10003","statusCategory":{"key":"indeterminate"}}},
			{"id":"41","name":"Done","to":{"id":"10004","statusCategory":{"key":"done"}}},
			{"id":"11","name":"Todo","to":{"id":"10001","statusCategory":{"key":"new"}}}
		]}`,
		transitionsPost: http.StatusNoContent,
		searchPayload: `{"total":0,"issues":[]}`,
	}
}

// newMockJiraClient builds a *Client pointed at a test HTTP server.
// The caller must call server.Close() when done.
func newMockJiraClient(t *testing.T, cfg mockServerConfig) (*Client, *httptest.Server) {
	t.Helper()

	mux := http.NewServeMux()

	// GET /rest/api/3/myself
	mux.HandleFunc("/rest/api/3/myself", func(w http.ResponseWriter, r *http.Request) {
		if cfg.myselfStatus != http.StatusOK {
			http.Error(w, "unauthorized", cfg.myselfStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"accountId": "abc123", "displayName": "Test User"})
	})

	// GET /rest/api/3/project/{key}/statuses
	mux.HandleFunc("/rest/api/3/project/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/statuses") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(cfg.statusesPayload))
			return
		}
		http.NotFound(w, r)
	})

	// POST /rest/api/3/issue/{key}/comment  (AddComment)
	// GET/POST /rest/api/3/issue/{key}/transitions
	mux.HandleFunc("/rest/api/3/issue/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/comment"):
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if cfg.commentStatus != http.StatusCreated {
				http.Error(w, "comment failed", cfg.commentStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "999"})

		case strings.HasSuffix(r.URL.Path, "/transitions"):
			switch r.Method {
			case http.MethodGet:
				if cfg.failTransitions {
					http.Error(w, "transitions unavailable", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(cfg.transitionsGet))
			case http.MethodPost:
				if cfg.transitionsPost != http.StatusNoContent {
					http.Error(w, "transition failed", cfg.transitionsPost)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}

		default:
			http.NotFound(w, r)
		}
	})

	// POST /rest/api/3/search/jql  (fetchTickets via SearchJQL)
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cfg.searchPayload))
	})

	srv := httptest.NewServer(mux)

	atlClient, err := atlassianjira.New(srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("atlassianjira.New: %v", err)
	}
	atlClient.Auth.SetBasicAuth("test@test.com", "test-token")

	jiraCfg := JiraConfig{
		Site:     "test",
		Email:    "test@test.com",
		TokenEnv: "NIGHTSHIFT_JIRA_TOKEN",
		Project:  "VC",
		Label:    "nightshift",
	}
	client := &Client{
		jira: atlClient,
		cfg:  jiraCfg,
	}
	return client, srv
}

// ── Ping ─────────────────────────────────────────────────────────────────────

func TestClientPing_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	if err := client.Ping(testCtx(t)); err != nil {
		t.Errorf("Ping() error = %v", err)
	}
}

func TestClientPing_Failure(t *testing.T) {
	cfg := defaultMockConfig()
	cfg.myselfStatus = http.StatusUnauthorized
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	if err := client.Ping(testCtx(t)); err == nil {
		t.Error("Ping() should fail with unauthorized")
	}
}

// ── AddComment ────────────────────────────────────────────────────────────────

func TestAddComment_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	if err := client.AddComment(testCtx(t), "VC-1", "test comment\n\nsecond paragraph"); err != nil {
		t.Errorf("AddComment() error = %v", err)
	}
}

func TestAddComment_Multiline(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	// Multiple paragraphs with newlines
	if err := client.AddComment(testCtx(t), "VC-1", "line1\nline2\n\nline3"); err != nil {
		t.Errorf("AddComment() multiline error = %v", err)
	}
}

func TestAddComment_Failure(t *testing.T) {
	cfg := defaultMockConfig()
	cfg.commentStatus = http.StatusForbidden
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	if err := client.AddComment(testCtx(t), "VC-1", "test"); err == nil {
		t.Error("AddComment() should fail with forbidden status")
	}
}

// ── PostComment ───────────────────────────────────────────────────────────────

func TestPostComment_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	comment := NightshiftComment{
		Type:     CommentPlan,
		Provider: "claude",
		Model:    "claude-sonnet-4.5",
		Body:     "Here is the plan.",
	}
	if err := client.PostComment(testCtx(t), "VC-1", comment); err != nil {
		t.Errorf("PostComment() error = %v", err)
	}
}

// ── DiscoverStatuses ──────────────────────────────────────────────────────────

func TestDiscoverStatuses_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	sm, err := client.DiscoverStatuses(testCtx(t))
	if err != nil {
		t.Fatalf("DiscoverStatuses() error = %v", err)
	}
	if len(sm.TodoStatuses) == 0 {
		t.Error("expected at least one todo status")
	}
	if len(sm.InProgressStatuses) == 0 {
		t.Error("expected at least one in-progress status")
	}
	if len(sm.DoneStatuses) == 0 {
		t.Error("expected at least one done status")
	}
}

func TestDiscoverStatuses_Cached(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	sm1, err := client.DiscoverStatuses(testCtx(t))
	if err != nil {
		t.Fatalf("first DiscoverStatuses: %v", err)
	}
	sm2, err := client.DiscoverStatuses(testCtx(t))
	if err != nil {
		t.Fatalf("second DiscoverStatuses: %v", err)
	}
	if sm1 != sm2 {
		t.Error("DiscoverStatuses should return same cached pointer")
	}
}

// ── FindTransition / TransitionTo ─────────────────────────────────────────────

func TestFindTransition_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	tid, err := client.FindTransition(testCtx(t), "VC-1", "indeterminate")
	if err != nil {
		t.Fatalf("FindTransition() error = %v", err)
	}
	if tid == "" {
		t.Error("expected non-empty transition ID")
	}
}

func TestFindTransition_NotFound(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	_, err := client.FindTransition(testCtx(t), "VC-1", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent category")
	}
}

func TestFindTransition_TransitionsError(t *testing.T) {
	cfg := defaultMockConfig()
	cfg.failTransitions = true
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	_, err := client.FindTransition(testCtx(t), "VC-1", "done")
	if err == nil {
		t.Error("expected error when transitions endpoint fails")
	}
}

func TestTransitionTo_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	if err := client.TransitionTo(testCtx(t), "VC-1", "done"); err != nil {
		t.Errorf("TransitionTo() error = %v", err)
	}
}

// ── TransitionToInProgress ────────────────────────────────────────────────────

func TestTransitionToInProgress_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	if err := client.TransitionToInProgress(testCtx(t), "VC-1"); err != nil {
		t.Errorf("TransitionToInProgress() error = %v", err)
	}
}

func TestTransitionToInProgress_NoInProgressStatus(t *testing.T) {
	cfg := defaultMockConfig()
	// Only todo and done — no indeterminate status
	cfg.statusesPayload = `[{"id":"1","name":"Story","subtask":false,"statuses":[
		{"id":"1","name":"To Do","statusCategory":{"key":"new"}},
		{"id":"4","name":"Done","statusCategory":{"key":"done"}}
	]}]`
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	if err := client.TransitionToInProgress(testCtx(t), "VC-1"); err == nil {
		t.Error("expected error when no in-progress status exists")
	}
}

func TestTransitionToInProgress_TransitionFails(t *testing.T) {
	cfg := defaultMockConfig()
	cfg.transitionsPost = http.StatusInternalServerError
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	if err := client.TransitionToInProgress(testCtx(t), "VC-1"); err == nil {
		t.Error("expected error when transition POST fails")
	}
}

// ── TransitionToReview ────────────────────────────────────────────────────────

func TestTransitionToReview_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	if err := client.TransitionToReview(testCtx(t), "VC-1"); err != nil {
		t.Errorf("TransitionToReview() error = %v", err)
	}
}

func TestTransitionToReview_NoReviewStatus(t *testing.T) {
	cfg := defaultMockConfig()
	cfg.statusesPayload = `[{"id":"1","name":"Story","subtask":false,"statuses":[
		{"id":"1","name":"To Do","statusCategory":{"key":"new"}},
		{"id":"2","name":"In Progress","statusCategory":{"key":"indeterminate"}},
		{"id":"4","name":"Done","statusCategory":{"key":"done"}}
	]}]`
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	if err := client.TransitionToReview(testCtx(t), "VC-1"); err == nil {
		t.Error("expected error when no review status exists")
	}
}

// ── TransitionToDone ──────────────────────────────────────────────────────────

func TestTransitionToDone_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	if err := client.TransitionToDone(testCtx(t), "VC-1"); err != nil {
		t.Errorf("TransitionToDone() error = %v", err)
	}
}

// ── TransitionToNeedsInfo ─────────────────────────────────────────────────────

func TestTransitionToNeedsInfo_FallsBackToTodo(t *testing.T) {
	// No "needs info" status, should fall back to first "new" status.
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	if err := client.TransitionToNeedsInfo(testCtx(t), "VC-1"); err != nil {
		t.Errorf("TransitionToNeedsInfo() error = %v", err)
	}
}

func TestTransitionToNeedsInfo_WithNeedsInfoStatus(t *testing.T) {
	cfg := defaultMockConfig()
	cfg.statusesPayload = `[{"id":"1","name":"Story","subtask":false,"statuses":[
		{"id":"1","name":"To Do","statusCategory":{"key":"new"}},
		{"id":"5","name":"Needs Info","statusCategory":{"key":"indeterminate"}},
		{"id":"2","name":"In Progress","statusCategory":{"key":"indeterminate"}},
		{"id":"3","name":"In Review","statusCategory":{"key":"indeterminate"}},
		{"id":"4","name":"Done","statusCategory":{"key":"done"}}
	]}]`
	cfg.transitionsGet = `{"transitions":[
		{"id":"21","name":"Start","to":{"id":"2","statusCategory":{"key":"indeterminate"}}},
		{"id":"51","name":"Needs Info","to":{"id":"5","statusCategory":{"key":"indeterminate"}}},
		{"id":"31","name":"Review","to":{"id":"3","statusCategory":{"key":"indeterminate"}}},
		{"id":"41","name":"Done","to":{"id":"4","statusCategory":{"key":"done"}}},
		{"id":"11","name":"Todo","to":{"id":"1","statusCategory":{"key":"new"}}}
	]}`
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	if err := client.TransitionToNeedsInfo(testCtx(t), "VC-1"); err != nil {
		t.Errorf("TransitionToNeedsInfo() with needs-info status: error = %v", err)
	}
}

// ── HandleInvalidTicket ───────────────────────────────────────────────────────

func TestHandleInvalidTicket_Success(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	vr := &ValidationResult{
		Valid:       false,
		Score:       3,
		Issues:      []string{"no objective"},
		Missing:     []string{"acceptance criteria"},
		Suggestions: []string{"add AC"},
	}
	if err := client.HandleInvalidTicket(testCtx(t), "VC-1", vr); err != nil {
		t.Errorf("HandleInvalidTicket() error = %v", err)
	}
}

func TestHandleInvalidTicket_CommentFails(t *testing.T) {
	cfg := defaultMockConfig()
	cfg.commentStatus = http.StatusForbidden
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	vr := &ValidationResult{Score: 2, Issues: []string{"bad"}}
	if err := client.HandleInvalidTicket(testCtx(t), "VC-1", vr); err == nil {
		t.Error("expected error when comment fails")
	}
}

// ── FetchTodoTickets / FetchReviewTickets ─────────────────────────────────────

func TestFetchTodoTickets_Empty(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	tickets, err := client.FetchTodoTickets(testCtx(t))
	if err != nil {
		t.Fatalf("FetchTodoTickets() error = %v", err)
	}
	if len(tickets) != 0 {
		t.Errorf("expected 0 tickets, got %d", len(tickets))
	}
}

func TestFetchTodoTickets_WithResults(t *testing.T) {
	cfg := defaultMockConfig()
	cfg.searchPayload = `{"total":1,"issues":[{
		"key":"VC-42",
		"fields":{
			"summary":"Test ticket",
			"labels":["nightshift"],
			"status":{"id":"1","name":"To Do","statusCategory":{"key":"new"}},
			"description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Do the thing."}]}]},
			"comment":{"comments":[]},
			"issuelinks":[]
		}
	}]}`
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	tickets, err := client.FetchTodoTickets(testCtx(t))
	if err != nil {
		t.Fatalf("FetchTodoTickets() error = %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	if tickets[0].Key != "VC-42" {
		t.Errorf("Key = %q, want VC-42", tickets[0].Key)
	}
	if tickets[0].Summary != "Test ticket" {
		t.Errorf("Summary = %q", tickets[0].Summary)
	}
}

func TestFetchReviewTickets_NilStatusMap(t *testing.T) {
	client, srv := newMockJiraClient(t, defaultMockConfig())
	defer srv.Close()

	tickets, err := client.FetchReviewTickets(testCtx(t), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tickets != nil {
		t.Errorf("expected nil for nil statusMap, got %v", tickets)
	}
}

func TestFetchReviewTickets_WithStatusMap(t *testing.T) {
	cfg := defaultMockConfig()
	cfg.searchPayload = `{"total":0,"issues":[]}`
	client, srv := newMockJiraClient(t, cfg)
	defer srv.Close()

	// First discover statuses to get a valid StatusMap
	sm, err := client.DiscoverStatuses(testCtx(t))
	if err != nil {
		t.Fatalf("DiscoverStatuses: %v", err)
	}

	tickets, err := client.FetchReviewTickets(testCtx(t), sm)
	if err != nil {
		t.Fatalf("FetchReviewTickets() error = %v", err)
	}
	if len(tickets) != 0 {
		t.Errorf("expected 0 tickets, got %d", len(tickets))
	}
}

// ── helpers ────────────────────────────────────────────────────────────────────

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
