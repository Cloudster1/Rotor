package spec

import (
	"fmt"
	"strings"
)

// notBuilt refuses the fields M13 agreed until the issue that builds each merges, so no run takes a field it would
// ignore; each issue deletes its lines here (docs/api-contract-m13.md).
func notBuilt(s *Spec) []Problem {
	var out []Problem
	add := func(path, field string, issue int) {
		out = append(out, Problem{Path: path, Code: CodeNotBuilt,
			Message: fmt.Sprintf("%s is agreed but not built yet (#%d); leave it out until Rotor offers it.", field, issue)})
	}
	for _, name := range sortedKeys(s.Tools) {
		if s.Tools[name].Repository != nil {
			add(fmt.Sprintf("/tools/%s/repository", escape(name)), "A repository binding", 137)
		}
	}
	for i, n := range s.Nodes {
		base := fmt.Sprintf("/nodes/%d", i)
		for _, f := range []struct {
			set   bool
			field string
			issue int
		}{
			{n.Model != nil, "model", 133}, {n.Role != "", "role", 133}, {n.MaxSteps != 0, "maxSteps", 133},
			{n.MaxVisits != 0, "maxVisits", 134}, {n.OnExhausted != "", "onExhausted", 134}, {n.Join != "", "join", 135},
			{n.Workspace != nil, "workspace", 136},
		} {
			if f.set {
				add(base+"/"+f.field, "A node's "+f.field, f.issue)
			}
		}
		if n.Type == TypeTest {
			add(base+"/type", "The test node", 138)
		}
		if n.Type == TypeRouter && strings.HasPrefix(n.On, "visits.") {
			add(base+"/on", "Reading visits", 134)
		}
	}
	return out
}
