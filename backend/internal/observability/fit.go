package observability

import (
	"maps"
	"slices"

	"github.com/cloudster1/rotor/backend/internal/config"
)

func attrBytes(attrs map[string]string) int {
	n := 0
	for k, v := range attrs {
		n += len(k) + len(v)
	}
	return n
}

func spanBytes(s *RunnerSpan) int {
	n := len(s.Name) + attrBytes(s.Attributes)
	for _, e := range s.Events {
		n += len(e.Name) + attrBytes(e.Attributes)
	}
	return n
}

// fit holds a span to config.MaxSpanBytes: it drops its text, longest first, then its oldest events, then its longest
// other attributes but Rotor's, and says so in rotor.content_cut.
func fit(s *RunnerSpan) {
	size := spanBytes(s)
	if size <= config.MaxSpanBytes {
		return
	}
	size += len(ContentCut) + len("true")
	size = dropLongest(s.Attributes, textKeys, size)
	for size > config.MaxSpanBytes && len(s.Events) > 0 {
		size -= len(s.Events[0].Name) + attrBytes(s.Events[0].Attributes)
		s.Events = s.Events[1:]
	}
	dropLongest(s.Attributes, plainKeys, size)
	s.Attributes[ContentCut] = "true"
}

// dropLongest drops attributes of one rank, longest first, until size fits; it answers the size left.
func dropLongest(attrs map[string]string, of int, size int) int {
	keys := slices.Collect(maps.Keys(attrs))
	slices.SortFunc(keys, func(a, b string) int { return len(attrs[b]) - len(attrs[a]) })
	for _, k := range keys {
		if size <= config.MaxSpanBytes {
			break
		}
		if rank(k) == of {
			size -= len(k) + len(attrs[k])
			delete(attrs, k)
		}
	}
	return size
}

// WithoutText drops every attribute that carries text from spans, for a place that may not keep it.
func WithoutText(spans []RunnerSpan) []RunnerSpan {
	out := make([]RunnerSpan, len(spans))
	for i, s := range spans {
		s.Attributes = plain(s.Attributes)
		events := make([]SpanEvent, len(s.Events))
		for j, e := range s.Events {
			e.Attributes = plain(e.Attributes)
			events[j] = e
		}
		s.Events = events
		out[i] = s
	}
	return out
}

func plain(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		if !IsText(k) {
			out[k] = v
		}
	}
	return out
}
