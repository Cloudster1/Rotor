package spec

import "fmt"

// CodeUntrustedWrite warns of a node that may write after reading what anyone could have written in a tool, with
// no approval in between (#40); saving still works.
const CodeUntrustedWrite = "untrusted_then_write"

// Warnings finds what may be unwise but is allowed: writes text read from a tool could steer, loops nothing bounds
// (#134) and branches meeting with no join (#135). c may be nil: every call of a write binding is then a write.
func (s *Spec) Warnings(c Catalog) []Problem {
	infos := map[string]ToolInfo{}
	if c != nil {
		for name, b := range s.Tools {
			if info, ok := c.Tool(b.Tool); ok {
				infos[name] = info
			}
		}
	}
	index := map[string]int{}
	for i, n := range s.Nodes {
		if _, dup := index[n.ID]; !dup {
			index[n.ID] = i
		}
	}
	isApproval := func(n Node) bool { return n.Type == TypeApproval }
	var readers []string
	for _, n := range s.Nodes {
		if reads(n) {
			readers = append(readers, n.ID)
		}
	}
	var out []Problem
	for i, n := range s.Nodes {
		through := s.writesThrough(n, infos, func(Binding) bool { return true }, false)
		if len(through) == 0 {
			continue
		}
		path := fmt.Sprintf("/nodes/%d", i)
		if n.Type == TypeAgent || n.Type == TypeCode {
			out = append(out, Problem{Path: path, Code: CodeUntrustedWrite, Message: fmt.Sprintf(
				"%s reads from its tools and may write through %s in the same step, so text it reads could steer the write. Read in one node, put an approval node after it, and write in the next.",
				n.ID, through[0])})
			continue
		}
		for _, r := range readers {
			if r != n.ID && reach(s, index, r, isApproval)[n.ID] {
				out = append(out, Problem{Path: path, Code: CodeUntrustedWrite, Message: fmt.Sprintf(
					"%s may write through %s after %s read from a tool, with no approval in between, so text it read could steer the write. Put an approval node between them.",
					n.ID, through[0], r)})
				break
			}
		}
	}
	out = append(out, s.unboundedLoops(index)...)
	out = append(out, s.missingJoins(index)...)
	if s.places != nil {
		return s.places.place(out)
	}
	return out
}

// reads says whether a node takes in what a tool answers.
func reads(n Node) bool {
	switch n.Type {
	case TypeTool:
		return n.Tool != ""
	case TypeAgent, TypeCode:
		return len(n.Tools) > 0
	}
	return false
}
