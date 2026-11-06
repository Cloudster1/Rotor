package httpapi

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces"
)

// How get_run_trace shortens a trace for a model; docs/api-contract-m18.md says why each part goes first.
var modelAttributes = []string{"rotor.node", "rotor.node.type", "rotor.binding", "mcp.tool", "tool.name", "rotor.interrupted"}

const (
	exceptionEvent = "exception"
	failedStatus   = "error"
	// encoderNewline is the byte respondJSON adds after a body, which the tool result's cap counts too.
	encoderNewline = 1
)

// valuedKinds are the spans whose inputs and outputs a model reads first after the failed ones'.
var valuedKinds = map[string]bool{"LLM": true, "TOOL": true}

// pick is a span on its way into a shortened tree.
type pick struct {
	node     *traces.Node
	parent   *pick
	children []*pick
	fields   spanFields
	cost     int
	kept     bool
}

// forModel shortens a trace for get_run_trace: the attributes and events that say where a run went, failed spans and
// their ancestors first, then the rest by start, and inputs and outputs while the budget lasts.
func forModel(t traces.Trace, roots []*traces.Node) traceTree {
	head := traceHead(t)
	used := jsonSize(traceResponse{Trace: head})
	var all []*pick
	var top []*pick
	for _, r := range roots {
		top = append(top, flatten(r, nil, &all))
	}
	slices.SortStableFunc(all, func(a, b *pick) int {
		return cmp.Or(a.node.StartedAt.Compare(b.node.StartedAt), cmp.Compare(a.node.SpanID, b.node.SpanID))
	})
	var failed []*pick
	for _, p := range all {
		if p.node.Status == failedStatus {
			failed = append(failed, p)
		}
	}
	kept := 0
	for _, p := range slices.Concat(failed, all) {
		var chain []*pick
		cost := 0
		for at := p; at != nil && !at.kept; at = at.parent {
			chain, cost = append(chain, at), cost+at.cost
		}
		if len(chain) == 0 || kept+len(chain) > config.MCPTraceMaxSpans || used+cost > config.MCPTraceSpanBudgetBytes {
			continue
		}
		for _, at := range chain {
			at.kept = true
		}
		kept, used = kept+len(chain), used+cost
	}
	valued := slices.Clone(failed)
	for _, p := range slices.Backward(all) {
		if valuedKinds[p.node.Kind] && p.node.Status != failedStatus {
			valued = append(valued, p)
		}
	}
	for _, p := range valued {
		if !p.kept {
			continue
		}
		in := valueOf(p.node.Span, inputKey, inputMimeKey, config.MCPTraceValueChars)
		out := valueOf(p.node.Span, outputKey, outputMimeKey, config.MCPTraceValueChars)
		add := valueSize(in) + valueSize(out)
		if used+add > config.MCPTraceBudgetBytes {
			break
		}
		p.fields.Input, p.fields.Output, used = in, out, used+add
	}
	for _, p := range top {
		if p.kept {
			head.Roots = append(head.Roots, p.assemble())
		}
	}
	return head
}

// flatten makes a pick of each span under n, its fields shortened and without values, and what it costs in the answer.
func flatten(n *traces.Node, parent *pick, all *[]*pick) *pick {
	p := &pick{node: n, parent: parent, fields: toSpanFields(n.Span, wholeValue)}
	p.fields.Input, p.fields.Output = nil, nil
	attrs := map[string]string{}
	for _, k := range modelAttributes {
		if v, ok := n.Attributes[k]; ok {
			attrs[k] = v
		}
	}
	p.fields.Attributes = attrs
	events := []spanEvent{}
	for _, e := range p.fields.Events {
		if e.Name == exceptionEvent {
			events = append(events, cutEvent(e, config.MCPTraceValueChars))
		}
	}
	p.fields.Events = events
	// The most it can say it omitted stands in for the count, which is known only once the tree is chosen.
	p.cost = jsonSize(traceSpan{spanFields: p.fields, Children: []traceSpan{}, Omitted: len(n.Children)}) + len(",")
	*all = append(*all, p)
	for _, c := range n.Children {
		p.children = append(p.children, flatten(c, p, all))
	}
	return p
}

func (p *pick) assemble() traceSpan {
	out := traceSpan{spanFields: p.fields, Children: []traceSpan{}}
	for _, c := range p.children {
		if c.kept {
			out.Children = append(out.Children, c.assemble())
		} else {
			out.Omitted++
		}
	}
	return out
}

// fitSpan cuts the values of one span, all to the same length, so get_run_trace_span fits a tool's answer.
func fitSpan(in traceSpanResponse) traceSpanResponse {
	limit := config.MCPMaxToolResultBytes - encoderNewline
	if jsonSize(in) <= limit {
		return in
	}
	best := cutSpan(in, 0)
	low, high := 1, config.MaxSpanContentChars
	for low <= high {
		mid := (low + high) / 2
		if c := cutSpan(in, mid); jsonSize(c) <= limit {
			best, low = c, mid+1
		} else {
			high = mid - 1
		}
	}
	return best
}

func cutSpan(in traceSpanResponse, chars int) traceSpanResponse {
	out := in
	out.Span.Input, out.Span.Output = cutValue(in.Span.Input, chars), cutValue(in.Span.Output, chars)
	out.Span.Content = make(map[string]string, len(in.Span.Content))
	for k, v := range in.Span.Content {
		out.Span.Content[k], _ = cutChars(v, chars)
	}
	out.Span.Events = make([]spanEvent, 0, len(in.Span.Events))
	for _, e := range in.Span.Events {
		out.Span.Events = append(out.Span.Events, cutEvent(e, chars))
	}
	return out
}

func cutValue(v *spanValue, chars int) *spanValue {
	if v == nil {
		return nil
	}
	value, cut := cutChars(v.Value, chars)
	return &spanValue{Value: value, MimeType: v.MimeType, Cut: v.Cut || cut}
}

func cutEvent(e spanEvent, chars int) spanEvent {
	attrs := map[string]string{}
	maps.Copy(attrs, e.Attributes)
	for k, v := range attrs {
		attrs[k], _ = cutChars(v, chars)
	}
	return spanEvent{Name: e.Name, At: e.At, Attributes: attrs}
}

// valueSize is what filling an input or output adds to the answer in place of its null.
func valueSize(v *spanValue) int {
	if v == nil {
		return 0
	}
	return jsonSize(v) - len("null")
}

func jsonSize(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}
