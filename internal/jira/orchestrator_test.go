package jira

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marcus/nightshift/internal/agents"
)

// stubJiraClient implements the jiraClient interface for testing.
type stubJiraClient struct {
	postCommentCalls   []NightshiftComment
	handleInvalidCalls []string // ticket keys
	transitionCalls    []string // "inprogress:KEY" or "review:KEY"

	postCommentErr   error
	handleInvalidErr error
	transitionErr    error // returned by both transition methods
}

func (s *stubJiraClient) PostComment(_ context.Context, ticketKey string, comment NightshiftComment) error {
	s.postCommentCalls = append(s.postCommentCalls, comment)
	return s.postCommentErr
}

func (s *stubJiraClient) HandleInvalidTicket(_ context.Context, ticketKey string, _ *ValidationResult) error {
	s.handleInvalidCalls = append(s.handleInvalidCalls, ticketKey)
	return s.handleInvalidErr
}

func (s *stubJiraClient) TransitionToInProgress(_ context.Context, issueKey string) error {
	s.transitionCalls = append(s.transitionCalls, "inprogress:"+issueKey)
	return s.transitionErr
}

func (s *stubJiraClient) TransitionToReview(_ context.Context, issueKey string) error {
	s.transitionCalls = append(s.transitionCalls, "review:"+issueKey)
	return s.transitionErr
}

// ── NewOrchestrator ───────────────────────────────────────────────────────────

func TestNewOrchestrator_Options(t *testing.T) {
	va := &stubAgent{name: "validator"}
	ia := &stubAgent{name: "impl"}
	ra := &stubAgent{name: "reviewer"}
	sm := &StatusMap{}

	o := &Orchestrator{}
	WithValidationAgent(va)(o)
	WithImplAgent(ia)(o)
	WithReviewFixAgent(ra)(o)
	WithStatusMap(sm)(o)

	if o.validationAgent != va {
		t.Error("WithValidationAgent not set")
	}
	if o.implAgent != ia {
		t.Error("WithImplAgent not set")
	}
	if o.reviewFixAgent != ra {
		t.Error("WithReviewFixAgent not set")
	}
	if o.statusMap != sm {
		t.Error("WithStatusMap not set")
	}
}

// ── ProcessTicket ─────────────────────────────────────────────────────────────

func TestProcessTicket_NilAgents(t *testing.T) {
	sc := &stubJiraClient{}
	o := &Orchestrator{client: sc, cfg: JiraConfig{}}

	_, err := o.ProcessTicket(context.Background(), Ticket{Key: "X-1"}, &Workspace{})
	if err == nil {
		t.Fatal("expected error for nil agents")
	}
}

func TestProcessTicket_HappyPath(t *testing.T) {
	sc := &stubJiraClient{}
	va := &stubAgent{
		name:   "validator",
		output: `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": []}`,
	}
	ia := &stubAgent{
		name:   "impl",
		output: "implementation done",
	}
	o := &Orchestrator{
		client:          sc,
		cfg:             JiraConfig{Plan: PhaseConfig{Model: "test-model"}, Implement: PhaseConfig{Model: "test-model"}},
		validationAgent: va,
		implAgent:       ia,
	}

	ticket := Ticket{Key: "TEST-1", Summary: "Test ticket", Description: "Do the thing."}
	ws := &Workspace{TicketKey: "TEST-1"} // no repos — skips commit/PR

	result, err := o.ProcessTicket(context.Background(), ticket, ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TicketCompleted {
		t.Errorf("Status = %q, want %q", result.Status, TicketCompleted)
	}
	if result.TicketKey != "TEST-1" {
		t.Errorf("TicketKey = %q, want TEST-1", result.TicketKey)
	}
	if result.Plan == "" {
		t.Error("Plan should not be empty")
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}

	// Verify transitions were called
	wantTransitions := []string{"inprogress:TEST-1", "review:TEST-1"}
	if len(sc.transitionCalls) != len(wantTransitions) {
		t.Fatalf("transitions = %v, want %v", sc.transitionCalls, wantTransitions)
	}
	for i, want := range wantTransitions {
		if sc.transitionCalls[i] != want {
			t.Errorf("transition[%d] = %q, want %q", i, sc.transitionCalls[i], want)
		}
	}

	// Verify comments were posted (validation, plan, implement, status)
	if len(sc.postCommentCalls) < 3 {
		t.Errorf("expected at least 3 comments, got %d", len(sc.postCommentCalls))
	}
}

func TestProcessTicket_ValidationRejects(t *testing.T) {
	sc := &stubJiraClient{}
	va := &stubAgent{
		name:   "validator",
		output: `{"valid": false, "score": 3, "issues": ["no AC"], "missing": ["scope"], "suggestions": ["add AC"]}`,
	}
	ia := &stubAgent{name: "impl"}
	o := &Orchestrator{
		client:          sc,
		cfg:             JiraConfig{},
		validationAgent: va,
		implAgent:       ia,
	}

	ticket := Ticket{Key: "TEST-2", Summary: "Vague ticket"}
	result, err := o.ProcessTicket(context.Background(), ticket, &Workspace{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TicketRejected {
		t.Errorf("Status = %q, want %q", result.Status, TicketRejected)
	}
	if len(sc.handleInvalidCalls) != 1 || sc.handleInvalidCalls[0] != "TEST-2" {
		t.Errorf("HandleInvalidTicket not called correctly: %v", sc.handleInvalidCalls)
	}
	// Should NOT have transitioned to in-progress
	if len(sc.transitionCalls) != 0 {
		t.Errorf("expected no transitions, got %v", sc.transitionCalls)
	}
}

func TestProcessTicket_ValidationError(t *testing.T) {
	sc := &stubJiraClient{}
	va := &stubAgent{name: "validator", err: errors.New("agent timeout")}
	ia := &stubAgent{name: "impl"}
	o := &Orchestrator{
		client:          sc,
		cfg:             JiraConfig{},
		validationAgent: va,
		implAgent:       ia,
	}

	result, err := o.ProcessTicket(context.Background(), Ticket{Key: "TEST-3"}, &Workspace{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TicketFailed {
		t.Errorf("Status = %q, want %q", result.Status, TicketFailed)
	}
	if result.Phase != PhaseValidate {
		t.Errorf("Phase = %q, want %q", result.Phase, PhaseValidate)
	}
	// Error comment should have been posted
	hasErrorComment := false
	for _, c := range sc.postCommentCalls {
		if c.Type == CommentError {
			hasErrorComment = true
		}
	}
	if !hasErrorComment {
		t.Error("expected error comment to be posted")
	}
}

func TestProcessTicket_PlanError(t *testing.T) {
	sc := &stubJiraClient{}
	callCount := 0
	// Validation succeeds, plan fails
	va := &stubAgent{
		name:   "validator",
		output: `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": []}`,
	}
	ia := &stubAgent{name: "impl"}
	o := &Orchestrator{
		client:          sc,
		cfg:             JiraConfig{},
		validationAgent: va,
		implAgent:       ia,
	}
	// Override implAgent to fail on first call (plan phase)
	o.implAgent = &callCountAgent{
		calls: []*agents.ExecuteResult{nil, {Output: "ok"}},
		errs:  []error{errors.New("plan timeout"), nil},
		count: &callCount,
	}

	result, err := o.ProcessTicket(context.Background(), Ticket{Key: "TEST-4", Summary: "Test"}, &Workspace{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TicketFailed {
		t.Errorf("Status = %q, want %q", result.Status, TicketFailed)
	}
	if result.Phase != PhasePlan {
		t.Errorf("Phase = %q, want %q", result.Phase, PhasePlan)
	}
}

func TestProcessTicket_ImplementError(t *testing.T) {
	sc := &stubJiraClient{}
	callCount := 0
	va := &stubAgent{
		name:   "validator",
		output: `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": []}`,
	}
	// Plan succeeds (call 0), implement fails (call 1)
	ia := &callCountAgent{
		calls: []*agents.ExecuteResult{{Output: "the plan"}, nil},
		errs:  []error{nil, errors.New("impl crashed")},
		count: &callCount,
	}
	o := &Orchestrator{
		client:          sc,
		cfg:             JiraConfig{},
		validationAgent: va,
		implAgent:       ia,
	}

	result, err := o.ProcessTicket(context.Background(), Ticket{Key: "TEST-5", Summary: "Test"}, &Workspace{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TicketFailed {
		t.Errorf("Status = %q, want %q", result.Status, TicketFailed)
	}
	if result.Phase != PhaseImplement {
		t.Errorf("Phase = %q, want %q", result.Phase, PhaseImplement)
	}
}

// ── buildImplementPrompt ──────────────────────────────────────────────────────

func TestBuildImplementPrompt(t *testing.T) {
	o := &Orchestrator{cfg: JiraConfig{}}
	ticket := Ticket{
		Key:                "PROJ-42",
		Summary:            "Add login",
		Description:        "Users should be able to log in.",
		AcceptanceCriteria: "Login works with email/password.",
	}
	ws := &Workspace{
		TicketKey: "PROJ-42",
		Repos: []RepoWorkspace{
			{Name: "api", Path: "/ws/api", Branch: "feature/PROJ-42", BaseBranch: "main"},
		},
	}

	prompt := o.buildImplementPrompt(ticket, "Step 1: do X", ws)

	for _, want := range []string{ticket.Key, ticket.Description, "Step 1: do X", "api", "/ws/api"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if !strings.Contains(prompt, "Do not commit") {
		t.Error("prompt should instruct agent not to commit")
	}
}

func TestBuildImplementPrompt_MultiRepo(t *testing.T) {
	o := &Orchestrator{cfg: JiraConfig{}}
	ticket := Ticket{Key: "X-1", Description: "Multi-repo work."}
	ws := &Workspace{
		Repos: []RepoWorkspace{
			{Name: "frontend", Path: "/ws/frontend"},
			{Name: "backend", Path: "/ws/backend"},
			{Name: "shared", Path: "/ws/shared"},
		},
	}

	prompt := o.buildImplementPrompt(ticket, "plan", ws)

	for _, name := range []string{"frontend", "backend", "shared"} {
		if !strings.Contains(prompt, name) {
			t.Errorf("prompt missing repo %q", name)
		}
	}
}

// ── buildPlanPrompt ───────────────────────────────────────────────────────────

func TestBuildPlanPrompt(t *testing.T) {
	o := &Orchestrator{cfg: JiraConfig{}}
	ticket := Ticket{
		Key:                "PROJ-10",
		Summary:            "Add caching",
		Description:        "Implement Redis caching layer.",
		AcceptanceCriteria: "Cache hit rate > 90%.",
		Comments: []Comment{
			{Author: "bob", Body: "Use redis cluster."},
		},
	}

	prompt := o.buildPlanPrompt(ticket)

	for _, want := range []string{ticket.Key, ticket.Summary, ticket.Description, ticket.AcceptanceCriteria, "bob", "redis cluster"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// ── parseTimeout ──────────────────────────────────────────────────────────────

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		input    string
		fallback time.Duration
		want     time.Duration
	}{
		{"5m", 10 * time.Minute, 5 * time.Minute},
		{"", 10 * time.Minute, 10 * time.Minute},
		{"invalid", 10 * time.Minute, 10 * time.Minute},
		{"2h", 10 * time.Minute, 2 * time.Hour},
	}
	for _, tt := range tests {
		got := parseTimeout(tt.input, tt.fallback)
		if got != tt.want {
			t.Errorf("parseTimeout(%q, %s) = %s, want %s", tt.input, tt.fallback, got, tt.want)
		}
	}
}

// ── TransitionToInProgress error ─────────────────────────────────────────────

func TestProcessTicket_TransitionToInProgressError(t *testing.T) {
	sc := &stubJiraClient{transitionErr: errors.New("jira unavailable")}
	va := &stubAgent{
		name:   "validator",
		output: `{"valid": true, "score": 9, "issues": [], "missing": [], "suggestions": []}`,
	}
	ia := &stubAgent{name: "impl"}
	o := &Orchestrator{
		client:          sc,
		cfg:             JiraConfig{},
		validationAgent: va,
		implAgent:       ia,
	}

	result, err := o.ProcessTicket(context.Background(), Ticket{Key: "TEST-6", Summary: "Test"}, &Workspace{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TicketFailed {
		t.Errorf("Status = %q, want %q", result.Status, TicketFailed)
	}
	if result.Error == "" {
		t.Error("result.Error should be set on transition failure")
	}
}

// ── TransitionToReview error ──────────────────────────────────────────────────

func TestProcessTicket_TransitionToReviewError(t *testing.T) {
	callCount := 0
	// inprogress succeeds, review fails
	sc := &stubJiraClientPerMethod{
		inprogressErr: nil,
		reviewErr:     errors.New("review transition failed"),
	}
	va := &stubAgent{
		name:   "validator",
		output: `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": []}`,
	}
	// plan and impl both succeed
	ia := &callCountAgent{
		calls: []*agents.ExecuteResult{{Output: "plan text"}, {Output: "impl done"}},
		errs:  []error{nil, nil},
		count: &callCount,
	}
	o := &Orchestrator{
		client:          sc,
		cfg:             JiraConfig{},
		validationAgent: va,
		implAgent:       ia,
	}

	result, err := o.ProcessTicket(context.Background(), Ticket{Key: "TEST-7", Summary: "Test"}, &Workspace{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TicketFailed {
		t.Errorf("Status = %q, want %q", result.Status, TicketFailed)
	}
	if result.Phase != PhaseStatus {
		t.Errorf("Phase = %q, want %q", result.Phase, PhaseStatus)
	}
}

// ── PostComment error is non-fatal ────────────────────────────────────────────

func TestProcessTicket_PostCommentErrorIsNonFatal(t *testing.T) {
	sc := &stubJiraClient{postCommentErr: errors.New("comment API down")}
	va := &stubAgent{
		name:   "validator",
		output: `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": []}`,
	}
	ia := &stubAgent{name: "impl", output: "done"}
	o := &Orchestrator{
		client:          sc,
		cfg:             JiraConfig{},
		validationAgent: va,
		implAgent:       ia,
	}

	// Even though PostComment always fails, ProcessTicket should complete.
	result, err := o.ProcessTicket(context.Background(), Ticket{Key: "TEST-8", Summary: "Test"}, &Workspace{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TicketCompleted {
		t.Errorf("Status = %q, want %q", result.Status, TicketCompleted)
	}
}

// ── Comment types are posted in order ────────────────────────────────────────

func TestProcessTicket_CommentTypes(t *testing.T) {
	sc := &stubJiraClient{}
	va := &stubAgent{
		name:   "validator",
		output: `{"valid": true, "score": 8, "issues": [], "missing": [], "suggestions": []}`,
	}
	ia := &stubAgent{name: "impl", output: "impl output"}
	o := &Orchestrator{
		client:          sc,
		cfg:             JiraConfig{},
		validationAgent: va,
		implAgent:       ia,
	}

	_, err := o.ProcessTicket(context.Background(), Ticket{Key: "TEST-9", Summary: "Test"}, &Workspace{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantTypes := []CommentType{CommentValidation, CommentPlan, CommentImplement, CommentStatusChange}
	if len(sc.postCommentCalls) < len(wantTypes) {
		t.Fatalf("expected at least %d comments, got %d", len(wantTypes), len(sc.postCommentCalls))
	}
	for i, want := range wantTypes {
		if sc.postCommentCalls[i].Type != want {
			t.Errorf("comment[%d].Type = %q, want %q", i, sc.postCommentCalls[i].Type, want)
		}
	}
}

// ── buildImplementPrompt edge cases ──────────────────────────────────────────

func TestBuildImplementPrompt_NilWorkspace(t *testing.T) {
	o := &Orchestrator{cfg: JiraConfig{}}
	ticket := Ticket{Key: "X-1", Description: "desc"}
	// Should not panic with nil workspace.
	prompt := o.buildImplementPrompt(ticket, "plan", nil)
	if !strings.Contains(prompt, "X-1") {
		t.Error("prompt missing ticket key")
	}
}

func TestBuildImplementPrompt_AcceptanceCriteria(t *testing.T) {
	o := &Orchestrator{cfg: JiraConfig{}}
	ticket := Ticket{
		Key:                "X-2",
		Description:        "do something",
		AcceptanceCriteria: "must pass all tests",
	}
	prompt := o.buildImplementPrompt(ticket, "plan", &Workspace{})
	if !strings.Contains(prompt, "must pass all tests") {
		t.Error("prompt missing acceptance criteria")
	}
}

// ── buildPlanPrompt edge cases ────────────────────────────────────────────────

func TestBuildPlanPrompt_NoComments(t *testing.T) {
	o := &Orchestrator{cfg: JiraConfig{}}
	ticket := Ticket{Key: "X-3", Summary: "bare", Description: "just a description"}
	prompt := o.buildPlanPrompt(ticket)
	if !strings.Contains(prompt, ticket.Key) {
		t.Error("prompt missing key")
	}
	if strings.Contains(prompt, "## Comments") {
		t.Error("prompt should not contain Comments section when there are none")
	}
}

func TestBuildPlanPrompt_NoAcceptanceCriteria(t *testing.T) {
	o := &Orchestrator{cfg: JiraConfig{}}
	ticket := Ticket{Key: "X-4", Summary: "bare", Description: "desc"}
	prompt := o.buildPlanPrompt(ticket)
	if strings.Contains(prompt, "Acceptance Criteria") {
		t.Error("prompt should not contain Acceptance Criteria section when empty")
	}
}

// ── stubJiraClientPerMethod ───────────────────────────────────────────────────

// stubJiraClientPerMethod allows independent error control per transition method.
type stubJiraClientPerMethod struct {
	postCommentCalls []NightshiftComment
	inprogressErr    error
	reviewErr        error
}

func (s *stubJiraClientPerMethod) PostComment(_ context.Context, _ string, comment NightshiftComment) error {
	s.postCommentCalls = append(s.postCommentCalls, comment)
	return nil
}
func (s *stubJiraClientPerMethod) HandleInvalidTicket(_ context.Context, _ string, _ *ValidationResult) error {
	return nil
}
func (s *stubJiraClientPerMethod) TransitionToInProgress(_ context.Context, _ string) error {
	return s.inprogressErr
}
func (s *stubJiraClientPerMethod) TransitionToReview(_ context.Context, _ string) error {
	return s.reviewErr
}

// ── helpers ───────────────────────────────────────────────────────────────────

// callCountAgent returns different results on successive Execute calls.
type callCountAgent struct {
	calls []*agents.ExecuteResult
	errs  []error
	count *int
}

func (a *callCountAgent) Name() string { return "call-count" }
func (a *callCountAgent) Execute(_ context.Context, _ agents.ExecuteOptions) (*agents.ExecuteResult, error) {
	i := *a.count
	*a.count++
	if i < len(a.calls) {
		return a.calls[i], a.errs[i]
	}
	return &agents.ExecuteResult{Output: ""}, nil
}

