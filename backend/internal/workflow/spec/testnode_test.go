package spec

import (
	"slices"
	"testing"
)

func TestATestStepRunsInItsImageAndNeverWritesThroughATool(t *testing.T) {
	s := parseFixture(t, "test-step.yaml")
	if images := s.WorkspaceImages(); !slices.Equal(images, []string{"golang-1.26"}) {
		t.Errorf("the workflow names the images %v", images)
	}
	test := s.Nodes[2]
	if test.Type != TypeTest || !slices.Equal(test.Reports, []string{"junit.xml"}) || test.Command[0] != "gotestsum" {
		t.Fatalf("the test node reads as %+v", test)
	}
	// Even with every binding asking for approval, the test step is no write that needs one.
	repo := s.Tools["repo"]
	repo.Approval = ApprovalBeforeWrite
	s.Tools["repo"] = repo
	if through := s.writesThrough(test, nil, func(Binding) bool { return true }, false); len(through) != 0 {
		t.Errorf("a test node writes through %v", through)
	}
	if got := codes(s.CheckAgainst(catalog{"armature": {Scopes: []string{"read", "issue.write"}}})); got["/nodes/2"] != "" {
		t.Errorf("the test node needs an approval: %v", got)
	}
	g := s.Graph()
	if g.Nodes[2] != (GraphNode{ID: "test", Type: TypeTest}) || !slices.Contains(g.Edges, GraphEdge{From: "test", To: "report"}) {
		t.Errorf("the run's graph draws %+v", g)
	}
}

func TestAReportPathStaysInsideTheWorkTree(t *testing.T) {
	for path, want := range map[string]bool{
		"junit.xml": true, "reports/junit.xml": true, "./reports/go.xml": true,
		"/tmp/junit.xml": false, "../junit.xml": false, "a/../../b.xml": false, ".": false, "./": false, "": false, "a\x00b": false,
	} {
		if ok := reportPathProblem(path) == ""; ok != want {
			t.Errorf("the report path %q is taken: %v, want %v", path, ok, want)
		}
	}
}
