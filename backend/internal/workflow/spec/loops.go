package spec

import (
	"fmt"
	"strings"
)

// Loops (#134): a node's maxVisits bounds how often it runs, and onExhausted says where the run goes after.
const (
	// CodeExhaustedNowhere is an onExhausted that names no node.
	CodeExhaustedNowhere = "exhausted_to_nowhere"
	// CodeUnboundedLoop warns of a cycle in which no node carries maxVisits, which only limits.maxSteps ends.
	CodeUnboundedLoop = "unbounded_loop"
)

// unboundedLoops finds the cycles a run can go round with no node counting its visits, one warning per cycle at its
// first node. A way into a counted node may end at its onExhausted instead, so those ways skip the node.
func (s *Spec) unboundedLoops(index map[string]int) []Problem {
	// lands lists the uncounted nodes a way into id may run next: id itself, or past a counted node its onExhausted.
	var lands func(id string, seen map[string]bool) []string
	lands = func(id string, seen map[string]bool) []string {
		i, ok := index[id]
		if !ok || seen[id] {
			return nil
		}
		seen[id] = true
		n := s.Nodes[i]
		if n.MaxVisits == 0 {
			return []string{id}
		}
		if n.OnExhausted == "" {
			return nil
		}
		return lands(n.OnExhausted, seen)
	}
	follows := map[string][]string{}
	for i, n := range s.Nodes {
		if n.MaxVisits > 0 || index[n.ID] != i {
			continue
		}
		for _, to := range ways(s, n) {
			follows[n.ID] = append(follows[n.ID], lands(to, map[string]bool{})...)
		}
	}
	after := map[string]map[string]bool{}
	for id := range follows {
		after[id] = closure(follows, id)
	}
	var out []Problem
	grouped := map[string]bool{}
	for i, n := range s.Nodes {
		if !after[n.ID][n.ID] || grouped[n.ID] {
			continue
		}
		var loop []string
		for _, m := range s.Nodes {
			if !grouped[m.ID] && after[n.ID][m.ID] && after[m.ID][n.ID] {
				grouped[m.ID] = true
				loop = append(loop, m.ID)
			}
		}
		out = append(out, Problem{Path: fmt.Sprintf("/nodes/%d", i), Code: CodeUnboundedLoop, Message: fmt.Sprintf(
			"%s can run again and again, since no node of the loop sets maxVisits, and only limits.maxSteps ends it. Set maxVisits on one of them, and onExhausted to say where the run goes after.",
			listed(loop))})
	}
	return out
}

// closure is every node reachable from start in one step or more.
func closure(follows map[string][]string, start string) map[string]bool {
	reached := map[string]bool{}
	queue := append([]string(nil), follows[start]...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if reached[id] {
			continue
		}
		reached[id] = true
		queue = append(queue, follows[id]...)
	}
	return reached
}

// listed joins names as a sentence does: a, b and c.
func listed(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
