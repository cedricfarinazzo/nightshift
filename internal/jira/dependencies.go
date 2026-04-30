package jira

// DependencyGraph represents the dependency relationships between tickets.
type DependencyGraph struct {
	tickets   map[string]*Ticket
	blockedBy map[string][]string // key is blocked by each of the values
	blocks    map[string][]string // key blocks each of the values
}

// BlockedTicket is a ticket that cannot be started due to outstanding blockers.
type BlockedTicket struct {
	Ticket   Ticket
	Reason   string
	Blockers []string
}

// BuildDependencyGraph builds a dependency graph from a set of tickets.
// Only "Blocks" link types are used to establish edges. Duplicate edges are
// deduplicated so each blocker appears at most once per ticket.
func BuildDependencyGraph(tickets []Ticket) *DependencyGraph {
	g := &DependencyGraph{
		tickets:   make(map[string]*Ticket, len(tickets)),
		blockedBy: make(map[string][]string),
		blocks:    make(map[string][]string),
	}
	for i := range tickets {
		t := &tickets[i]
		g.tickets[t.Key] = t
	}
	for _, t := range tickets {
		for _, link := range t.IssueLinks {
			if link.Type != "Blocks" {
				continue
			}
			// Direction "outward": t.Key blocks OutwardKey
			if link.Direction == "outward" && link.OutwardKey != "" {
				g.addEdge(t.Key, link.OutwardKey)
			}
			// Direction "inward": InwardKey blocks t.Key — skip if the blocker is already done.
			if link.Direction == "inward" && link.InwardKey != "" {
				if link.BlockerStatusCategory == "done" {
					continue
				}
				g.addEdge(link.InwardKey, t.Key)
			}
		}
	}
	return g
}

// addEdge records that blocker blocks blocked, deduplicating if the edge already exists.
func (g *DependencyGraph) addEdge(blocker, blocked string) {
	if !containsKey(g.blockedBy[blocked], blocker) {
		g.blockedBy[blocked] = append(g.blockedBy[blocked], blocker)
	}
	if !containsKey(g.blocks[blocker], blocked) {
		g.blocks[blocker] = append(g.blocks[blocker], blocked)
	}
}

func containsKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// ResolveOrder returns tickets that are immediately ready to process (no unresolved blockers)
// and tickets that are blocked (external blockers, unfinished in-graph dependencies, or cycles).
//
// A ticket is ready only when it has no blockers at all — including other TODO tickets in the
// same graph. This prevents implementing a ticket before its in-graph dependency has been
// completed and merged. Tickets blocked by in-graph dependencies appear in the blocked list
// with reason "waiting for dependency"; cyclic tickets appear with "circular dependency".
func (g *DependencyGraph) ResolveOrder() (ready []Ticket, blocked []BlockedTicket) {
	// Compute in-degree and separate external from in-graph blockers.
	inDegree := make(map[string]int, len(g.tickets))
	externalBlockers := make(map[string][]string)
	for key := range g.tickets {
		inDegree[key] = 0
	}
	for key := range g.tickets {
		for _, blocker := range g.blockedBy[key] {
			if _, inGraph := g.tickets[blocker]; inGraph {
				inDegree[key]++
			} else {
				externalBlockers[key] = append(externalBlockers[key], blocker)
			}
		}
	}

	// Tickets with external blockers are immediately blocked.
	externallyBlocked := make(map[string]bool)
	for key, ext := range externalBlockers {
		if len(ext) > 0 {
			externallyBlocked[key] = true
			blocked = append(blocked, BlockedTicket{
				Ticket:   *g.tickets[key],
				Reason:   "external blocker",
				Blockers: ext,
			})
		}
	}

	// Run Kahn's algorithm to find the topological order and detect cycles.
	// We track two sets: "immediately ready" (initial inDegree=0) and "eventually
	// reachable" (reachable after processing their dependencies). Tickets never
	// reached are in a cycle.
	workDegree := make(map[string]int, len(g.tickets))
	for k, v := range inDegree {
		workDegree[k] = v
	}

	queue := make([]string, 0)
	for key := range g.tickets {
		if workDegree[key] == 0 && !externallyBlocked[key] {
			queue = append(queue, key)
		}
	}

	visited := make(map[string]bool)
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if visited[key] {
			continue
		}
		visited[key] = true
		if inDegree[key] == 0 {
			// No in-graph dependencies at all: immediately ready.
			ready = append(ready, *g.tickets[key])
		} else {
			// Reachable via Kahn's but has in-graph dependencies: waiting.
			blocked = append(blocked, BlockedTicket{
				Ticket:   *g.tickets[key],
				Reason:   "waiting for dependency",
				Blockers: g.blockedBy[key],
			})
		}
		for _, dependent := range g.blocks[key] {
			if _, inGraph := g.tickets[dependent]; !inGraph {
				continue
			}
			workDegree[dependent]--
			if workDegree[dependent] == 0 && !externallyBlocked[dependent] {
				queue = append(queue, dependent)
			}
		}
	}

	// Tickets not visited by Kahn's (workDegree still > 0) are in a cycle.
	for key := range g.tickets {
		if !visited[key] && !externallyBlocked[key] {
			blocked = append(blocked, BlockedTicket{
				Ticket:   *g.tickets[key],
				Reason:   "circular dependency",
				Blockers: g.blockedBy[key],
			})
		}
	}

	return ready, blocked
}

// DetectCycles returns all cycles in the dependency graph as slices of ticket keys.
func (g *DependencyGraph) DetectCycles() [][]string {
	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := make(map[string]int, len(g.tickets))
	stack := make([]string, 0)
	var cycles [][]string

	var dfs func(key string)
	dfs = func(key string) {
		state[key] = inStack
		stack = append(stack, key)
		for _, dep := range g.blocks[key] {
			if _, inGraph := g.tickets[dep]; !inGraph {
				continue
			}
			switch state[dep] {
			case unvisited:
				dfs(dep)
			case inStack:
				cycle := extractCycle(stack, dep)
				cycles = append(cycles, cycle)
			}
		}
		stack = stack[:len(stack)-1]
		state[key] = done
	}

	for key := range g.tickets {
		if state[key] == unvisited {
			dfs(key)
		}
	}
	return cycles
}

// extractCycle returns the portion of the stack that forms the cycle ending at cycleStart.
func extractCycle(stack []string, cycleStart string) []string {
	for i, key := range stack {
		if key == cycleStart {
			cycle := make([]string, len(stack)-i)
			copy(cycle, stack[i:])
			return cycle
		}
	}
	return nil
}
