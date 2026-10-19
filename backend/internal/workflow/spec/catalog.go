package spec

import (
	"fmt"
	"slices"
)

// Catalog is what Rotor knows of its tools when a workflow is saved, as the saving person sees them.
type Catalog interface {
	// Tool answers a tool by its name in Rotor; ok is false when Rotor has none of that name.
	Tool(name string) (info ToolInfo, ok bool)
}

// ToolInfo is a tool's scopes and MCP tools.
type ToolInfo struct {
	Scopes []string
	// Calls are the tool's MCP tools; nil when unknown, such as for a person not connected to it.
	Calls map[string]CallInfo
}

// CallInfo is what an MCP tool says of itself.
type CallInfo struct {
	ReadOnly    bool
	Destructive bool
}

// CheckAgainst finds what only the tools can tell: unknown tools, scopes and calls, writes through a read
// binding, and writes without the approval a binding asks for.
func (s *Spec) CheckAgainst(c Catalog) []Problem {
	var out []Problem
	add := func(path, code, format string, args ...any) {
		out = append(out, Problem{Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
	}
	infos := map[string]ToolInfo{}
	for _, name := range sortedKeys(s.Tools) {
		b, base := s.Tools[name], "/tools/"+escape(name)
		info, ok := c.Tool(b.Tool)
		if !ok {
			add(base+"/tool", CodeUnknownTool, "Rotor has no tool called %s; an administrator adds it in the settings.", b.Tool)
			continue
		}
		infos[name] = info
		for j, scope := range b.Scopes {
			if !slices.Contains(info.Scopes, scope) {
				add(fmt.Sprintf("%s/scopes/%d", base, j), CodeUnknownScope, "%s does not offer the scope %s; it offers %v.", b.Tool, scope, info.Scopes)
			}
		}
		if info.Calls == nil {
			continue
		}
		for j, call := range b.AllowTools {
			known, ok := info.Calls[call]
			switch {
			case !ok:
				add(fmt.Sprintf("%s/allowTools/%d", base, j), CodeUnknownCall, "%s has no MCP tool called %s.", b.Tool, call)
			case !b.Writes() && !known.ReadOnly:
				add(fmt.Sprintf("%s/allowTools/%d", base, j), CodeWritesOnRead, "%s may change things in %s, so the binding needs access: write.", call, b.Tool)
			}
		}
	}
	out = append(out, s.approvalProblems(infos)...)
	if s.places != nil {
		return s.places.place(out)
	}
	return out
}

// approvalProblems finds nodes that write through a beforeWrite binding where a run can reach them unapproved.
func (s *Spec) approvalProblems(infos map[string]ToolInfo) []Problem {
	index := map[string]int{}
	for i, n := range s.Nodes {
		if _, dup := index[n.ID]; !dup {
			index[n.ID] = i
		}
	}
	unapproved := reach(s, index, s.Entry, func(n Node) bool { return n.Type == TypeApproval })
	var out []Problem
	for i, n := range s.Nodes {
		if n.Type == TypeApproval || !unapproved[n.ID] {
			continue
		}
		for _, name := range s.writesThrough(n, infos, func(b Binding) bool { return b.Approval == ApprovalBeforeWrite }) {
			out = append(out, Problem{Path: fmt.Sprintf("/nodes/%d", i), Code: CodeNeedsApproval,
				Message: fmt.Sprintf("%s writes through %s, which asks for approval first; put an approval node on every way to it.", n.ID, name)})
		}
	}
	return out
}

// writesThrough names the write bindings that asks holds for which a node may write through: a call the tool does
// not mark read-only counts as a write.
func (s *Spec) writesThrough(n Node, infos map[string]ToolInfo, asks func(Binding) bool) []string {
	writes := func(name string, calls []string) bool {
		b, ok := s.Tools[name]
		if !ok || !asks(b) || !b.Writes() {
			return false
		}
		info := infos[name]
		for _, call := range calls {
			if c, known := info.Calls[call]; !known || !c.ReadOnly {
				return true
			}
		}
		return false
	}
	var out []string
	switch n.Type {
	case TypeTool:
		if writes(n.Tool, []string{n.Call}) {
			out = append(out, n.Tool)
		}
	case TypeAgent, TypeCode:
		for _, name := range n.Tools {
			if writes(name, s.Tools[name].AllowTools) {
				out = append(out, name)
			}
		}
	}
	return out
}
