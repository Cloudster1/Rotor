package spec

import (
	"fmt"
	"slices"
	"strings"
)

// Problem codes of workspaces (#136): a workspace on no repository binding, or one that cannot push what it
// writes, and an image no administrator allows.
const (
	CodeWorkspaceBinding = "workspace_binding"
	CodeImageNotAllowed  = "image_not_allowed"
)

// workspaceSense refuses a workspace whose repository is not a repository binding of the workflow, one that writes
// through a binding that may not push, and a branch read from a node that is not there.
func workspaceSense(s *Spec, known func(string) bool) []Problem {
	var out []Problem
	for i, n := range s.Nodes {
		w := n.Workspace
		if w == nil {
			continue
		}
		base := fmt.Sprintf("/nodes/%d/workspace", i)
		b, ok := s.Tools[w.Repository]
		switch {
		case !ok || b.Repository == nil:
			out = append(out, Problem{Path: base + "/repository", Code: CodeWorkspaceBinding,
				Message: fmt.Sprintf("%s is not a repository binding of the workflow; name a tool binding with a repository, which the step checks out.", w.Repository)})
		case w.Writes() && !b.Writes():
			out = append(out, Problem{Path: base + "/access", Code: CodeWorkspaceBinding,
				Message: fmt.Sprintf("%s pushes what it writes, so its repository binding %s needs access: write.", n.ID, w.Repository)})
		}
		for _, m := range templateRef.FindAllStringSubmatch(w.Branch, -1) {
			if ref := stateNode(m[1]); ref != "" && !known(ref) {
				out = append(out, Problem{Path: base + "/branch", Code: CodeUnknownNode,
					Message: fmt.Sprintf("The branch reads the output of %s, which is not a node.", ref)})
			} else if !strings.HasPrefix(m[1], "inputs.") && !strings.HasPrefix(m[1], "nodes.") {
				out = append(out, Problem{Path: base + "/branch", Code: CodeWorkspaceBinding,
					Message: fmt.Sprintf("The branch may read the run's inputs or a node's output, not %s.", m[1])})
			}
		}
	}
	return out
}

// Writes says whether the step commits and pushes what changed.
func (w Workspace) Writes() bool { return w.Access == AccessWrite }

// WorkspaceImages are the toolchain images the workflow's steps name, each once, in order.
func (s *Spec) WorkspaceImages() []string {
	var out []string
	for _, n := range s.Nodes {
		if n.Workspace != nil && !slices.Contains(out, n.Workspace.Image) {
			out = append(out, n.Workspace.Image)
		}
	}
	slices.Sort(out)
	return out
}

// CheckImages refuses a workspace image that allowed says no administrator allows; a run's start checks again.
func (s *Spec) CheckImages(allowed func(string) bool) []Problem {
	var out []Problem
	for i, n := range s.Nodes {
		if n.Workspace == nil || allowed(n.Workspace.Image) {
			continue
		}
		out = append(out, Problem{Path: fmt.Sprintf("/nodes/%d/workspace/image", i), Code: CodeImageNotAllowed,
			Message: fmt.Sprintf("No administrator allows the toolchain image %s; choose one of the images Rotor's settings list.", n.Workspace.Image)})
	}
	if s.places != nil {
		return s.places.place(out)
	}
	return out
}
