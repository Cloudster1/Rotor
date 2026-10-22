package spec

import "fmt"

// notBuilt refuses the fields M13 agreed until the issue that builds each merges, so no run takes a field it would
// ignore; each issue deletes its lines here (docs/api-contract-m13.md).
func notBuilt(s *Spec) []Problem {
	var out []Problem
	add := func(path, field string, issue int) {
		out = append(out, Problem{Path: path, Code: CodeNotBuilt,
			Message: fmt.Sprintf("%s is agreed but not built yet (#%d); leave it out until Rotor offers it.", field, issue)})
	}
	for i, n := range s.Nodes {
		base := fmt.Sprintf("/nodes/%d", i)
		for _, f := range []struct {
			set   bool
			field string
			issue int
		}{} {
			if f.set {
				add(base+"/"+f.field, "A node's "+f.field, f.issue)
			}
		}
	}
	return out
}
