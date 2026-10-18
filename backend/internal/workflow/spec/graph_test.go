package spec

import (
	"reflect"
	"testing"
)

func TestTheGraphDrawsEdgesAndEachRoutersCases(t *testing.T) {
	s, problems := Parse([]byte(`apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: triage}
limits: {maxSteps: 5, timeout: 1m}
entry: ask
nodes:
  - {id: ask, type: approval, message: 'Go?'}
  - {id: route, type: router, on: nodes.ask.approved, cases: {'true': agreed, 'false': refused}, default: refused}
  - {id: agreed, type: approval, message: 'Sure?'}
  - {id: refused, type: approval, message: 'Why?'}
edges:
  - {from: ask, to: route}
`))
	if s == nil || len(problems) > 0 {
		t.Fatalf("the workflow does not parse: %+v", problems)
	}
	g := s.Graph()
	if g.Entry != "ask" || len(g.Nodes) != 4 || g.Nodes[1] != (GraphNode{ID: "route", Type: TypeRouter}) {
		t.Errorf("the nodes = %+v", g)
	}
	want := []GraphEdge{{From: "ask", To: "route"}, {From: "route", To: "refused", Label: "false"}, {From: "route", To: "agreed", Label: "true"},
		{From: "route", To: "refused", Label: "default"}}
	if !reflect.DeepEqual(g.Edges, want) {
		t.Errorf("the edges = %+v, want %+v", g.Edges, want)
	}
}
