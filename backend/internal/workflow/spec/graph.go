package spec

import "sort"

// GraphNode is a node as a run's graph draws it.
type GraphNode struct {
	ID   string `json:"id"`
	Type string `json:"type"`
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

// defaultLabel names a router's way when no case matches.
const defaultLabel = "default"

// Graph is what a run's page draws: the nodes in the spec's order, its edges, then each router's cases.
func (s *Spec) Graph() Graph {
	g := Graph{Entry: s.Entry, Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	for _, n := range s.Nodes {
		g.Nodes = append(g.Nodes, GraphNode{ID: n.ID, Type: n.Type})
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
	return g
}
