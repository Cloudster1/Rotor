package spec

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestABoundedLoopIsNotWarnedAbout(t *testing.T) {
	s := parseFixture(t, "review-loop.yaml")
	if got := s.Warnings(exciter); len(got) > 0 {
		t.Errorf("a loop with maxVisits is warned about: %+v", got)
	}
}

func TestALoopWithNoMaxVisitsIsWarnedAboutOnceAtItsFirstNode(t *testing.T) {
	s := parseFixture(t, "review-loop.yaml")
	s.Nodes[0].MaxVisits = 0
	got := s.Warnings(exciter)
	if len(got) != 1 || got[0].Path != "/nodes/0" || got[0].Code != CodeUnboundedLoop || got[0].Line == 0 {
		t.Fatalf("an unbounded loop = %+v", got)
	}
	if !strings.HasPrefix(got[0].Message, "write, review and verdict can run again") {
		t.Errorf("the warning names %q", got[0].Message)
	}
}

func TestAWayPastAnExhaustedNodeThatLeadsBackIsStillALoop(t *testing.T) {
	s, problems := Parse([]byte(`apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: around}
limits: {maxSteps: 20, timeout: 1m}
entry: a
nodes:
  - {id: a, type: approval, message: 'A?', maxVisits: 2, onExhausted: b}
  - {id: b, type: approval, message: 'B?'}
  - {id: c, type: approval, message: 'C?', maxVisits: 2, onExhausted: d}
  - {id: d, type: approval, message: 'D?'}
edges:
  - {from: a, to: b}
  - {from: b, to: a}
  - {from: b, to: c}
  - {from: c, to: c}
`))
	if s == nil || len(problems) > 0 {
		t.Fatalf("the workflow does not parse: %+v", problems)
	}
	// Once a has no visit left, b leads to b itself; c going round itself ends at d.
	if got := codes(s.Warnings(nil)); len(got) != 1 || got["/nodes/1"] != CodeUnboundedLoop {
		t.Errorf("warnings = %v", got)
	}
}

func TestOnExhaustedMustNameANodeAndVisitsReadKnownNodes(t *testing.T) {
	_, problems := Parse([]byte(`apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: nowhere}
limits: {maxSteps: 20, timeout: 1m}
entry: a
nodes:
  - {id: a, type: approval, message: 'Try {{ visits.a }} of {{ visits.b }}?', maxVisits: 2, onExhausted: gone}
  - {id: r, type: router, on: visits.a, cases: {'1': a}, default: z}
  - {id: z, type: approval, message: 'Done after {{ visits.a }}?'}
edges:
  - {from: a, to: r}
`))
	got := codes(problems)
	want := map[string]string{"/nodes/0/onExhausted": CodeExhaustedNowhere, "/nodes/0/message": CodeUnknownNode}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems = %v, want %v", got, want)
	}
}

func TestTheGraphDrawsWhereANodeGoesPastItsLastVisit(t *testing.T) {
	g := parseFixture(t, "review-loop.yaml").Graph()
	last := g.Edges[len(g.Edges)-1]
	if last != (GraphEdge{From: "write", To: "escalate", Label: "onExhausted"}) {
		t.Errorf("the last edge = %+v", last)
	}
	// escalate is reached only past write's last visit; without that way nothing leads to it.
	source, _ := os.ReadFile(filepath.Join(fixtures, "valid", "review-loop.yaml"))
	_, problems := Parse([]byte(strings.Replace(string(source), "    onExhausted: escalate\n", "", 1)))
	if len(problems) != 1 || problems[0].Code != CodeUnreachable || problems[0].Path != "/nodes/3" {
		t.Errorf("problems = %+v", problems)
	}
}
