package spec

import (
	"strings"
	"testing"
)

// review fans out from test to two reviewers whose edges both lead into gather.
const review = `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: review}
limits: {maxSteps: 20, timeout: 1m}
entry: test
nodes:
  - {id: test, type: approval, message: 'Test?'}
  - {id: review-code, type: approval, message: 'Code?'}
  - {id: review-security, type: approval, message: 'Security?'}
  - {id: gather, type: approval, message: 'Merge?'JOIN}
edges:
  - {from: test, to: review-code}
  - {from: test, to: review-security}
  - {from: review-code, to: gather}
  - {from: review-security, to: gather}
`

func parseReview(t *testing.T, join string) *Spec {
	t.Helper()
	s, problems := Parse([]byte(strings.Replace(review, "JOIN", join, 1)))
	if s == nil || len(problems) > 0 {
		t.Fatalf("the workflow does not parse: %+v", problems)
	}
	return s
}

func TestANodeSeveralBranchesMeetAtWithoutAJoinIsWarnedAbout(t *testing.T) {
	got := parseReview(t, "").Warnings(nil)
	if len(got) != 1 || got[0].Path != "/nodes/3" || got[0].Code != CodeJoinMissing || got[0].Line == 0 {
		t.Fatalf("warnings = %+v", got)
	}
	if !strings.HasPrefix(got[0].Message, "gather runs once for each of review-code and review-security") {
		t.Errorf("the warning says %q", got[0].Message)
	}
}

func TestAJoinIsAllowedAndSilencesTheWarning(t *testing.T) {
	s := parseReview(t, ", join: all")
	if got := s.Warnings(nil); len(got) > 0 {
		t.Errorf("a join is warned about: %+v", got)
	}
	if g := s.Graph(); !g.Nodes[3].Join || g.Nodes[2].Join {
		t.Errorf("the graph's nodes = %+v", g.Nodes)
	}
}

func TestAnEdgeBackIntoALoopIsNotABranch(t *testing.T) {
	s := parseReview(t, "")
	// gather goes back to review-code, bounded, and review-security no longer leads to gather.
	s.Nodes[1].MaxVisits = 3
	s.Edges[3] = Edge{From: "gather", To: "review-code"}
	if got := s.Warnings(nil); len(got) > 0 {
		t.Errorf("a loop's way back is warned about: %+v", got)
	}
}
