package spec

import (
	"fmt"
	"slices"
)

// Problems with the skills an agent names (#221). The library's answers come from whoever saves or runs the workflow.
const (
	CodeSkillTwice          = "skill_twice"
	CodeSkillUnknown        = "skill_unknown"
	CodeSkillVersionUnknown = "skill_version_unknown"
	CodeSkillArchived       = "skill_archived"
	// CodeSkillUnreadable refuses a run's start, not a spec: its person may not read a skill the version names.
	CodeSkillUnreadable = "skill_unreadable"
)

// SkillState is a skill as the person checking reads it: the versions it has and whether it is archived.
type SkillState struct {
	Versions []int
	Archived bool
}

// skillSense refuses a skill named twice on one node, which would give the agent the same skill twice.
func skillSense(n Node, base string) []Problem {
	var out []Problem
	seen := map[string]bool{}
	for j, ref := range n.Skills {
		if seen[ref.Name] {
			out = append(out, Problem{Path: fmt.Sprintf("%s/skills/%d", base, j), Code: CodeSkillTwice,
				Message: fmt.Sprintf("%s names the skill %s twice; keep one of them.", n.ID, ref.Name)})
		}
		seen[ref.Name] = true
	}
	return out
}

// SkillNames are the skills the agents name, each once, in order.
func (s *Spec) SkillNames() []string {
	var out []string
	for _, n := range s.Nodes {
		for _, ref := range n.Skills {
			if !slices.Contains(out, ref.Name) {
				out = append(out, ref.Name)
			}
		}
	}
	return out
}

// CheckSkills refuses a pin the library does not hold as found answers it; a skill missing from found is one the
// person may not read or none at all, and both read alike so a name gives nothing away.
func (s *Spec) CheckSkills(found map[string]SkillState) []Problem {
	var out []Problem
	for i, n := range s.Nodes {
		for j, ref := range n.Skills {
			path := fmt.Sprintf("/nodes/%d/skills/%d", i, j)
			state, ok := found[ref.Name]
			switch {
			case !ok:
				out = append(out, Problem{Path: path, Code: CodeSkillUnknown,
					Message: fmt.Sprintf("There is no skill called %s that you may read; check the name, or ask the skill's owner to share it with you.", ref.Name)})
			case !slices.Contains(state.Versions, ref.Version):
				out = append(out, Problem{Path: path + "/version", Code: CodeSkillVersionUnknown,
					Message: fmt.Sprintf("The skill %s has no version %d; pin one of the versions the skill lists.", ref.Name, ref.Version)})
			case state.Archived:
				out = append(out, Problem{Path: path, Code: CodeSkillArchived,
					Message: fmt.Sprintf("The skill %s is archived; choose another skill, or ask its administrator to restore it.", ref.Name)})
			}
		}
	}
	if s.places != nil {
		return s.places.place(out)
	}
	return out
}

// UnreadableSkill is the first skill an agent names that is missing from readable, with the node naming it.
func (s *Spec) UnreadableSkill(readable map[string]SkillState) (node string, ref SkillRef, ok bool) {
	for _, n := range s.Nodes {
		for _, r := range n.Skills {
			if _, found := readable[r.Name]; !found {
				return n.ID, r, true
			}
		}
	}
	return "", SkillRef{}, false
}
