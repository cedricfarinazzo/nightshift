package jira

import (
	"testing"
)

func makeTicket(key string, links []IssueLink) Ticket {
	return Ticket{Key: key, Summary: key + " summary", IssueLinks: links}
}

func blocksLink(blocker, blocked string) IssueLink {
	return IssueLink{
		Type:       "Blocks",
		InwardKey:  blocker,
		OutwardKey: blocked,
		Direction:  "outward",
	}
}

func TestBuildDependencyGraph_NoDependencies(t *testing.T) {
	// no dependencies — all tickets ready
	tickets := []Ticket{
		makeTicket("A-1", nil),
		makeTicket("A-2", nil),
		makeTicket("A-3", nil),
	}
	g := BuildDependencyGraph(tickets)
	ready, blocked := g.ResolveOrder()
	if len(blocked) != 0 {
		t.Errorf("expected no blocked tickets, got %d", len(blocked))
	}
	if len(ready) != 3 {
		t.Errorf("expected 3 ready tickets, got %d", len(ready))
	}
}

func TestBuildDependencyGraph_SimpleChain(t *testing.T) {
	// linear chain: A-1 blocks A-2 blocks A-3
	// Only A-1 (no in-graph blockers) is immediately ready.
	// A-2 and A-3 are "waiting for dependency".
	tickets := []Ticket{
		makeTicket("A-1", []IssueLink{blocksLink("A-1", "A-2")}),
		makeTicket("A-2", []IssueLink{blocksLink("A-2", "A-3")}),
		makeTicket("A-3", nil),
	}
	g := BuildDependencyGraph(tickets)
	if cycles := g.DetectCycles(); len(cycles) != 0 {
		t.Errorf("expected no cycles in simple linear chain, got %v", cycles)
	}
	ready, blocked := g.ResolveOrder()
	if len(ready)+len(blocked) != 3 {
		t.Errorf("expected 3 total tickets, got ready=%d blocked=%d", len(ready), len(blocked))
	}
	if len(ready) != 1 || ready[0].Key != "A-1" {
		t.Errorf("expected only A-1 in ready, got %v", ready)
	}
	if len(blocked) != 2 {
		t.Errorf("expected A-2 and A-3 blocked (waiting for dependency), got %d blocked", len(blocked))
	}
	for _, b := range blocked {
		if b.Reason != "waiting for dependency" {
			t.Errorf("ticket %s: expected reason 'waiting for dependency', got %q", b.Ticket.Key, b.Reason)
		}
	}
}

func TestBuildDependencyGraph_CycleDetection(t *testing.T) {
	// circular dependency: A-1 blocks A-2, A-2 blocks A-1
	tickets := []Ticket{
		makeTicket("A-1", []IssueLink{blocksLink("A-1", "A-2")}),
		makeTicket("A-2", []IssueLink{blocksLink("A-2", "A-1")}),
	}
	g := BuildDependencyGraph(tickets)
	cycles := g.DetectCycles()
	if len(cycles) == 0 {
		t.Error("expected at least one cycle, got none")
	}
	_, blocked := g.ResolveOrder()
	if len(blocked) == 0 {
		t.Error("expected blocked tickets due to circular dependency")
	}
	for _, b := range blocked {
		if b.Reason != "circular dependency" {
			t.Errorf("expected reason 'circular dependency', got %q", b.Reason)
		}
	}
}

func TestContainsKey(t *testing.T) {
	if !containsKey([]string{"a", "b", "c"}, "b") {
		t.Error("containsKey should return true for existing key")
	}
	if containsKey([]string{"a", "b", "c"}, "z") {
		t.Error("containsKey should return false for missing key")
	}
	if containsKey(nil, "a") {
		t.Error("containsKey should return false for nil slice")
	}
}

func TestBuildDependencyGraph_ExternalBlocker(t *testing.T) {
	// A-1 is blocked by EXTERNAL-99 which is not in the set
	tickets := []Ticket{
		makeTicket("A-1", []IssueLink{{
			Type:       "Blocks",
			InwardKey:  "EXTERNAL-99",
			OutwardKey: "A-1",
			Direction:  "inward",
		}}),
		makeTicket("A-2", nil),
	}
	g := BuildDependencyGraph(tickets)
	ready, blocked := g.ResolveOrder()
	if len(blocked) == 0 {
		t.Fatal("expected A-1 to be blocked by external blocker outside set")
	}
	found := false
	for _, b := range blocked {
		if b.Ticket.Key == "A-1" {
			found = true
			if b.Reason != "external blocker" {
				t.Errorf("expected reason 'external blocker', got %q", b.Reason)
			}
		}
	}
	if !found {
		t.Error("A-1 not in blocked list")
	}
	// A-2 should be ready
	for _, r := range ready {
		if r.Key == "A-2" {
			return
		}
	}
	t.Error("expected A-2 to be ready")
}
