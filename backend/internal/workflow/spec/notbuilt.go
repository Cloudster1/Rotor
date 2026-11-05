package spec

import "fmt"

// notBuilt refuses the fields M17 agreed until the issue that builds each merges, so no run takes a field it would
// ignore; each issue deletes its lines here (docs/api-contract-m17.md).
func notBuilt(s *Spec) []Problem {
	var out []Problem
	for i, n := range s.Nodes {
		if len(n.Skills) > 0 {
			out = append(out, Problem{Path: fmt.Sprintf("/nodes/%d/skills", i), Code: CodeNotBuilt,
				Message: "A node's skills are agreed but not built yet (#221); leave them out until Rotor offers them."})
		}
	}
	return out
}
