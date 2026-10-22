package spec

import (
	"fmt"
	"sort"
)

// CodeUnattended marks a node that may write without an approval before it, which no trigger may start (#34).
const CodeUnattended = "unattended_write"

// UnattendedWrites finds the nodes a run can reach without passing an approval that may write through any binding,
// whatever its approval field says, except through calls the binding names unattended; the trigger checks that its
// owner agreed to those. known is false when a write binding's tool did not say which calls only read.
func (s *Spec) UnattendedWrites(c Catalog) (problems []Problem, known bool) {
	infos, known := map[string]ToolInfo{}, true
	for name, b := range s.Tools {
		info, ok := c.Tool(b.Tool)
		if ok {
			infos[name] = info
		}
		if b.Writes() && (!ok || info.Calls == nil) {
			known = false
		}
	}
	index := map[string]int{}
	for i, n := range s.Nodes {
		if _, dup := index[n.ID]; !dup {
			index[n.ID] = i
		}
	}
	unapproved := reach(s, index, s.Entry, func(n Node) bool { return n.Type == TypeApproval })
	for i, n := range s.Nodes {
		if n.Type == TypeApproval || !unapproved[n.ID] {
			continue
		}
		for _, name := range s.writesThrough(n, infos, func(Binding) bool { return true }, true) {
			problems = append(problems, Problem{Path: fmt.Sprintf("/nodes/%d", i), Code: CodeUnattended,
				Message: fmt.Sprintf("%s may write through %s with nobody watching; put an approval node on every way to it before a trigger starts this workflow.", n.ID, name)})
		}
	}
	return problems, known
}

// UnattendedCalls are the calls the workflow lets a triggered run make with no approval, as sorted
// <binding>.<call> pairs, which the trigger's owner must agree to.
func (s *Spec) UnattendedCalls() []string {
	var out []string
	for name, b := range s.Tools {
		for _, call := range b.Unattended {
			out = append(out, name+"."+call)
		}
	}
	sort.Strings(out)
	return out
}
