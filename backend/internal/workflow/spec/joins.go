package spec

import "fmt"

// Parallel branches (#135): join all runs a node once every branch into it has ended.
const (
	// JoinAll is the one join there is.
	JoinAll = "all"
	// CodeJoinMissing warns of a node several edges lead into with no join, which runs once per arriving branch.
	CodeJoinMissing = "join_missing"
)

// missingJoins finds the nodes several edges lead into that set no join. An edge that closes a loop through the node
// is a way back, not a branch, so it is not counted.
func (s *Spec) missingJoins(index map[string]int) []Problem {
	var out []Problem
	for i, n := range s.Nodes {
		if n.Join != "" || index[n.ID] != i {
			continue
		}
		around := reach(s, index, n.ID, nil)
		seen := map[string]bool{}
		var from []string
		for _, e := range s.Edges {
			if e.To == n.ID && !seen[e.From] && !around[e.From] {
				seen[e.From] = true
				from = append(from, e.From)
			}
		}
		if len(from) < 2 {
			continue
		}
		out = append(out, Problem{Path: fmt.Sprintf("/nodes/%d", i), Code: CodeJoinMissing, Message: fmt.Sprintf(
			"%s runs once for each of %s that reaches it, since it sets no join. Set join: all to run it once, after all of them.",
			n.ID, listed(from))})
	}
	return out
}
