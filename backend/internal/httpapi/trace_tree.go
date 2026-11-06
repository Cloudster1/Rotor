package httpapi

import (
	"maps"
	"unicode/utf8"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces"
)

// The text attributes a span's input and output are, and the plain ones that say their media type.
const (
	inputKey      = "input.value"
	outputKey     = "output.value"
	inputMimeKey  = "input.mime_type"
	outputMimeKey = "output.mime_type"
)

// wholeValue keeps an input or output uncut, as the span routes answer it.
const wholeValue = -1

// toTraceTree is a trace as the API answers it, each input and output cut to chars.
func toTraceTree(t traces.Trace, roots []*traces.Node, chars int) traceTree {
	out := traceHead(t)
	for _, n := range roots {
		out.Roots = append(out.Roots, toTraceSpan(n, chars))
	}
	return out
}

// traceHead is the trace without its spans.
func traceHead(t traces.Trace) traceTree {
	out := traceTree{TraceID: t.TraceID, State: traceState(t.State), Content: t.Content, Cut: t.Cut, Dropped: t.Dropped,
		Spans: t.Spans, Bytes: t.Bytes, StartedAt: t.StartedAt, EndedAt: t.EndedAt, ExpiresAt: t.ExpiresAt,
		// A run's trace route adds the destinations' links; a sender's own trace went to none.
		Links: []traceLink{}, Roots: []traceSpan{}}
	if t.RunID != nil {
		id := t.RunID.String()
		out.RunID = &id
	}
	if t.Owner != nil {
		out.Sender = &personRef{ID: t.Owner.ID.String(), Name: t.Owner.Name}
	}
	return out
}

func toTraceSpan(n *traces.Node, chars int) traceSpan {
	out := traceSpan{spanFields: toSpanFields(n.Span, chars), Children: make([]traceSpan, 0, len(n.Children))}
	for _, c := range n.Children {
		out.Children = append(out.Children, toTraceSpan(c, chars))
	}
	return out
}

func toSpanDetail(sp traces.Span) traceSpanDetail {
	content := map[string]string{}
	maps.Copy(content, sp.Text)
	return traceSpanDetail{spanFields: toSpanFields(sp, wholeValue), Content: content}
}

// toSpanFields is what a span says of itself, its input and output cut to chars and null where no text was kept.
func toSpanFields(sp traces.Span, chars int) spanFields {
	out := spanFields{SpanID: sp.SpanID, ParentSpanID: sp.ParentSpanID, Name: sp.Name, Kind: spanKind(sp.Kind),
		Service: sp.Service, Source: spanSource(sp.Source), StartedAt: sp.StartedAt, EndedAt: sp.EndedAt,
		DurationMs: sp.EndedAt.Sub(sp.StartedAt).Milliseconds(), Status: spanStatus(sp.Status), StatusMessage: sp.StatusMessage,
		Model: traces.Model(sp.Attributes), Attributes: map[string]string{}, Events: make([]spanEvent, 0, len(sp.Events)),
		Input:  valueOf(sp, inputKey, inputMimeKey, chars),
		Output: valueOf(sp, outputKey, outputMimeKey, chars)}
	maps.Copy(out.Attributes, sp.Attributes)
	if sp.Sender != nil {
		out.Sender = &personRef{ID: sp.Sender.ID.String(), Name: sp.Sender.Name}
	}
	if prompt, completion, total, ok := traces.Tokens(sp.Attributes); ok {
		out.Tokens = &spanTokens{Prompt: prompt, Completion: completion, Total: total}
	}
	for _, e := range sp.Events {
		attrs := map[string]string{}
		maps.Copy(attrs, e.Attributes)
		out.Events = append(out.Events, spanEvent{Name: e.Name, At: e.Time, Attributes: attrs})
	}
	out.Links = make([]spanLink, 0, len(sp.Links))
	for _, l := range sp.Links {
		link := spanLink{TraceID: l.TraceID, SpanID: l.SpanID, Attributes: map[string]string{}}
		maps.Copy(link.Attributes, l.Attributes)
		if l.RunID != nil {
			id := l.RunID.String()
			link.RunID = &id
		} else if l.Owner != nil {
			id := l.Owner.String()
			link.Sender = &id
		}
		out.Links = append(out.Links, link)
	}
	return out
}

// valueOf is a span's input or output; cut also says the runner cut it to MaxSpanContentChars when it kept it.
func valueOf(sp traces.Span, key, mimeKey string, chars int) *spanValue {
	value, ok := sp.Text[key]
	if !ok {
		return nil
	}
	kept := utf8.RuneCountInString(value) >= config.MaxSpanContentChars
	value, cut := cutChars(value, chars)
	return &spanValue{Value: value, MimeType: sp.Attributes[mimeKey], Cut: cut || kept}
}
