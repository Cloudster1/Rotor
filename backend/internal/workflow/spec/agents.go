package spec

import (
	"fmt"
	"strings"
)

// agentSense holds an agent's own step cap to the workflow's, and keeps templates out of its role: what a run read
// never becomes an instruction (#133).
func agentSense(s *Spec, n Node, base string) []Problem {
	var out []Problem
	if n.MaxSteps > 0 && s.Limits.MaxSteps > 0 && n.MaxSteps > s.Limits.MaxSteps {
		out = append(out, Problem{Path: base + "/maxSteps", Code: CodeStepsOverLimit,
			Message: fmt.Sprintf("%s may take at most the workflow's %d steps, not %d; lower its maxSteps or raise limits.maxSteps.", n.ID, s.Limits.MaxSteps, n.MaxSteps)})
	}
	if strings.Contains(n.Role, "{{") {
		out = append(out, Problem{Path: base + "/role", Code: CodeRoleTemplate,
			Message: fmt.Sprintf("The role of %s holds a template, and a role never does; put what the run read into the prompt.", n.ID)})
	}
	return out
}

// AgentModels is the model each agent node names of its own, by node id; the others use the workflow's.
func (s *Spec) AgentModels() map[string]Model {
	out := map[string]Model{}
	for _, n := range s.Nodes {
		if n.Type == TypeAgent && n.Model != nil {
			out[n.ID] = *n.Model
		}
	}
	return out
}

// UsesWorkflowModel says whether an agent runs on the workflow's model, having none of its own.
func (s *Spec) UsesWorkflowModel() bool {
	for _, n := range s.Nodes {
		if n.Type == TypeAgent && n.Model == nil {
			return true
		}
	}
	return false
}

// CheckModels refuses an agent's own model that allowed says no administrator allows; the workflow's own model is
// resolved when a run starts, as before.
func (s *Spec) CheckModels(allowed func(Model) bool) []Problem {
	var out []Problem
	for i, n := range s.Nodes {
		if n.Type != TypeAgent || n.Model == nil || allowed(*n.Model) {
			continue
		}
		out = append(out, Problem{Path: fmt.Sprintf("/nodes/%d/model", i), Code: CodeModelNotAllowed,
			Message: fmt.Sprintf("No administrator allows the model %s for %s; choose one of the models Rotor lists, or leave model out to use the workflow's.", n.Model.Describe(), n.ID)})
	}
	if s.places != nil {
		return s.places.place(out)
	}
	return out
}

// Describe names a model as a person reads it.
func (m Model) Describe() string {
	if m.Provider == "" {
		return m.Name
	}
	return m.Name + " of " + m.Provider
}
