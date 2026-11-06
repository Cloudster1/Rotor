package traces

import (
	"cmp"
	"slices"
	"strconv"
)

// Node is a span in a trace's tree, its children in start order.
type Node struct {
	Span
	Children []*Node
}

// Tree nests spans under their parents. A span whose parent the trace does not hold is a root, and so is the earliest
// span of a loop of parents a sender made up, so every span shows exactly once; siblings are in start order.
func Tree(spans []Span) []*Node {
	nodes := make([]*Node, len(spans))
	byID := make(map[string]*Node, len(spans))
	for i := range spans {
		nodes[i] = &Node{Span: spans[i]}
		if _, taken := byID[spans[i].SpanID]; !taken {
			byID[spans[i].SpanID] = nodes[i]
		}
	}
	slices.SortStableFunc(nodes, func(a, b *Node) int {
		return cmp.Or(a.StartedAt.Compare(b.StartedAt), cmp.Compare(a.SpanID, b.SpanID))
	})
	var roots []*Node
	for _, n := range nodes {
		parent, held := byID[n.ParentSpanID]
		if !held || n.ParentSpanID == "" || parent == n {
			roots = append(roots, n)
			continue
		}
		parent.Children = append(parent.Children, n)
	}
	// A loop of parents is reached from no root; its earliest span becomes one, and the edge back to it is cut.
	seen := make(map[*Node]bool, len(nodes))
	for _, r := range roots {
		mark(r, seen)
	}
	for _, n := range nodes {
		if seen[n] {
			continue
		}
		if parent := byID[n.ParentSpanID]; parent != nil {
			parent.Children = slices.DeleteFunc(parent.Children, func(c *Node) bool { return c == n })
		}
		roots = append(roots, n)
		mark(n, seen)
	}
	slices.SortStableFunc(roots, func(a, b *Node) int {
		return cmp.Or(a.StartedAt.Compare(b.StartedAt), cmp.Compare(a.SpanID, b.SpanID))
	})
	return roots
}

func mark(n *Node, seen map[*Node]bool) {
	stack := []*Node{n}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[top] {
			continue
		}
		seen[top] = true
		stack = append(stack, top.Children...)
	}
}

// Model is the model a span called: OpenInference's name for it, else the gen_ai copy the runner makes.
func Model(attrs map[string]string) string {
	return cmp.Or(attrs["llm.model_name"], attrs["gen_ai.request.model"])
}

// Tokens are a model call's token counts, from OpenInference's keys else the gen_ai ones; ok is false for a span
// that counted none.
func Tokens(attrs map[string]string) (prompt, completion, total int, ok bool) {
	prompt, okPrompt := count(attrs, "llm.token_count.prompt", "gen_ai.usage.input_tokens")
	completion, okCompletion := count(attrs, "llm.token_count.completion", "gen_ai.usage.output_tokens")
	total, okTotal := count(attrs, "llm.token_count.total")
	if !okTotal {
		total = prompt + completion
	}
	return prompt, completion, total, okPrompt || okCompletion || okTotal
}

func count(attrs map[string]string, keys ...string) (int, bool) {
	for _, k := range keys {
		if n, err := strconv.Atoi(attrs[k]); err == nil {
			return n, true
		}
	}
	return 0, false
}
