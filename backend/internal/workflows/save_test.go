package workflows

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/skills"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// library answers as the skill library does for one person: the skills they read, by name.
type library struct {
	held  map[string]skills.Pinnable
	asked [][]string
	err   error
}

func (l *library) ByNames(_ context.Context, names []string) (map[string]skills.Pinnable, error) {
	l.asked = append(l.asked, names)
	out := map[string]skills.Pinnable{}
	for _, name := range names {
		if p, ok := l.held[name]; ok {
			out[name] = p
		}
	}
	return out, l.err
}

const analyst = `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: analyst}
limits: {maxSteps: 5, timeout: 1m}
entry: a
nodes:
  - id: a
    type: agent
    prompt: Turn the story into requirements.
    skills:
      - {name: requirements-engineering, version: 2}
      - {name: test-engineering, version: 1}
`

func TestASaveChecksEachSkillAsTheSaverReadsIt(t *testing.T) {
	readable := map[string]skills.Pinnable{
		"requirements-engineering": {Versions: []int{1, 2}},
		"test-engineering":         {Versions: []int{1}},
	}
	lib := &library{held: readable}
	s := &Service{Skills: lib}
	if problems := s.Validate(context.Background(), analyst); len(problems) != 0 {
		t.Fatalf("skills the saver reads are refused: %+v", problems)
	}
	if len(lib.asked) != 1 || !slices.Equal(lib.asked[0], []string{"requirements-engineering", "test-engineering"}) {
		t.Errorf("the library was asked %v", lib.asked)
	}
	for _, c := range []struct {
		name string
		held map[string]skills.Pinnable
		path string
		code string
	}{
		{"unreadable or missing", map[string]skills.Pinnable{"test-engineering": {Versions: []int{1}}}, "/nodes/0/skills/0", spec.CodeSkillUnknown},
		{"no such version", map[string]skills.Pinnable{"requirements-engineering": {Versions: []int{1}}, "test-engineering": {Versions: []int{1}}},
			"/nodes/0/skills/0/version", spec.CodeSkillVersionUnknown},
		{"archived", map[string]skills.Pinnable{"requirements-engineering": {Versions: []int{2}, Archived: true}, "test-engineering": {Versions: []int{1}}},
			"/nodes/0/skills/0", spec.CodeSkillArchived},
	} {
		s := &Service{Skills: &library{held: c.held}}
		problems := s.Validate(context.Background(), analyst)
		if len(problems) != 1 || problems[0].Path != c.path || problems[0].Code != c.code {
			t.Errorf("%s: %+v", c.name, problems)
		}
	}
}

func TestAWorkflowWithoutSkillsAsksNoLibrary(t *testing.T) {
	lib := &library{}
	s := &Service{Skills: lib}
	source := `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: plain}
limits: {maxSteps: 5, timeout: 1m}
entry: a
nodes:
  - {id: a, type: agent, prompt: hi}
`
	if problems := s.Validate(context.Background(), source); len(problems) != 0 || len(lib.asked) != 0 {
		t.Errorf("problems %+v, asked %v", problems, lib.asked)
	}
}

func TestALibraryThatCannotAnswerLeavesTheSkillsToTheStart(t *testing.T) {
	s := &Service{Skills: &library{err: errors.New("the database is away")}}
	if problems := s.Validate(context.Background(), analyst); len(problems) != 0 {
		t.Errorf("problems %+v", problems)
	}
}
