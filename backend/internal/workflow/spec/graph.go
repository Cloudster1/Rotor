package spec

import "sort"

// GraphNode is a node as a run's graph draws it.
type GraphNode struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Join says the node waits for every branch into it (#135), so it is drawn below the longest.
	Join bool `json:"join,omitempty"`
}

// GraphEdge leads from one node to the next; a router's edges carry the case that takes them.
type GraphEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// Graph is a workflow's nodes and the ways between them, entry first.
type Graph struct {
	Entry string      `json:"entry"`
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// defaultLabel names a router's way when no case matches, exhaustedLabel a node's way once its visits are used up.
const (
	defaultLabel   = "default"
	exhaustedLabel = "onExhausted"
)

// Graph is what a run's page draws: the nodes in the spec's order, its edges, each router's cases, then each node's
// onExhausted (#134).
func (s *Spec) Graph() Graph {
	g := Graph{Entry: s.Entry, Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	for _, n := range s.Nodes {
		g.Nodes = append(g.Nodes, GraphNode{ID: n.ID, Type: n.Type, Join: n.Join == JoinAll})
	}
	for _, e := range s.Edges {
		g.Edges = append(g.Edges, GraphEdge{From: e.From, To: e.To})
	}
	for _, n := range s.Nodes {
		if n.Type != TypeRouter {
			continue
		}
		values := make([]string, 0, len(n.Cases))
		for value := range n.Cases {
			values = append(values, value)
		}
		sort.Strings(values)
		for _, value := range values {
			g.Edges = append(g.Edges, GraphEdge{From: n.ID, To: n.Cases[value], Label: value})
		}
		if n.Default != "" {
			g.Edges = append(g.Edges, GraphEdge{From: n.ID, To: n.Default, Label: defaultLabel})
		}
	}
	for _, n := range s.Nodes {
		if n.OnExhausted != "" {
			g.Edges = append(g.Edges, GraphEdge{From: n.ID, To: n.OnExhausted, Label: exhaustedLabel})
		}
	}
	return g
}
