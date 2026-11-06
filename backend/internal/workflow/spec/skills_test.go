package spec

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func withSkills(t *testing.T) *Spec {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(fixtures, "valid", "agent-with-skills.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s, problems := Parse(source)
	if s == nil || len(problems) > 0 {
		t.Fatalf("the fixture does not read: %+v", problems)
	}
	return s
}

func TestAnAgentKeepsItsSkillsPinned(t *testing.T) {
	s := withSkills(t)
	want := []SkillRef{{Name: "requirements-engineering", Version: 2}, {Name: "test-engineering", Version: 1}}
	if !slices.Equal(s.Nodes[0].Skills, want) {
		t.Errorf("the analyst's skills are %+v", s.Nodes[0].Skills)
	}
	if got := s.SkillNames(); !slices.Equal(got, []string{"requirements-engineering", "test-engineering"}) {
		t.Errorf("the skills named are %v", got)
	}
}

func TestASkillNamedTwiceIsRefusedAtItsSecondMention(t *testing.T) {
	source := []byte(`apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: twice}
limits: {maxSteps: 5, timeout: 1m}
entry: a
nodes:
  - id: a
    type: agent
    prompt: hi
    skills:
      - {name: one, version: 1}
      - {name: two, version: 1}
      - {name: one, version: 2}
      - {name: two, version: 1}
`)
	_, problems := Parse(source)
	var paths []string
	for _, p := range problems {
		if p.Code == CodeSkillTwice {
			paths = append(paths, p.Path)
		}
	}
	if !slices.Equal(paths, []string{"/nodes/0/skills/2", "/nodes/0/skills/3"}) || len(problems) != len(paths) {
		t.Fatalf("the problems are %+v", problems)
	}
}

func TestOnlyAnAgentTakesSkills(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(fixtures, "invalid", "code-with-skills.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, problems := Parse(source)
	if len(problems) != 1 || problems[0].Code != CodeSchema || problems[0].Path != "/nodes/0" {
		t.Fatalf("a code node's skills are refused as %+v", problems)
	}
}

func TestEachPinIsCheckedAgainstTheLibrary(t *testing.T) {
	s := withSkills(t)
	held := map[string]SkillState{
		"requirements-engineering": {Versions: []int{1, 2}},
		"test-engineering":         {Versions: []int{1}},
	}
	if problems := s.CheckSkills(held); len(problems) != 0 {
		t.Fatalf("skills the saver reads are refused: %+v", problems)
	}
	for _, c := range []struct {
		name  string
		found map[string]SkillState
		path  string
		code  string
	}{
		{"unknown", map[string]SkillState{"test-engineering": {Versions: []int{1}}}, "/nodes/0/skills/0", CodeSkillUnknown},
		{"no such version", map[string]SkillState{"requirements-engineering": {Versions: []int{1, 3}}, "test-engineering": {Versions: []int{1}}},
			"/nodes/0/skills/0/version", CodeSkillVersionUnknown},
		{"archived", map[string]SkillState{"requirements-engineering": {Versions: []int{2}}, "test-engineering": {Versions: []int{1}, Archived: true}},
			"/nodes/0/skills/1", CodeSkillArchived},
	} {
		problems := s.CheckSkills(c.found)
		if len(problems) != 1 || problems[0].Path != c.path || problems[0].Code != c.code || problems[0].Line == 0 || problems[0].Message == "" {
			t.Errorf("%s: %+v", c.name, problems)
		}
	}
}

func TestTheFirstSkillARunsPersonMayNotReadIsNamed(t *testing.T) {
	s := withSkills(t)
	if _, _, ok := s.UnreadableSkill(map[string]SkillState{"requirements-engineering": {}, "test-engineering": {Archived: true}}); ok {
		t.Error("an archived skill its person reads stops a run")
	}
	node, ref, ok := s.UnreadableSkill(map[string]SkillState{"requirements-engineering": {}})
	if !ok || node != "analyse" || ref.Name != "test-engineering" || ref.Version != 1 {
		t.Errorf("the unreadable skill is %s %+v %v", node, ref, ok)
	}
}
