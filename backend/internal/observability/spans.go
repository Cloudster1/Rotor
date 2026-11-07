package observability

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/redact"
)

//go:embed trace-content.json
var traceContentFile []byte

// traceContent is api/trace-content.json as read: the names of the attributes with text, without, and never kept.
var traceContent = func() struct{ Text, Plain, Never []string } {
	var c struct{ Text, Plain, Never []string }
	if err := json.Unmarshal(traceContentFile, &c); err != nil || len(c.Text) == 0 || len(c.Plain) == 0 {
		panic(fmt.Sprintf("observability: trace-content.json does not read: %v", err))
	}
	return c
}()

// SpanEvent is one event of a span, such as an exception, its attributes as strings.
type SpanEvent struct {
	Name       string
	Time       time.Time
	Attributes map[string]string
}

// SpanLink is another span a span names as related, such as each message of a batch it handled (#262).
type SpanLink struct {
	TraceID    trace.TraceID
	SpanID     trace.SpanID
	Attributes map[string]string
}

const (
	// ContentCut says a span was cut down to config.MaxSpanBytes.
	ContentCut = "rotor.content_cut"
	// stackKey is an exception's stack, kept with or without text to show where a run failed.
	stackKey                       = "exception.stacktrace"
	rotorKeys, plainKeys, textKeys = 0, 1, 2
)

func under(key, name string) bool { return key == name || strings.HasPrefix(key, name+".") }

// IsText says whether an attribute carries text a person, a tool or a model wrote, as api/trace-content.json says.
func IsText(key string) bool {
	for _, name := range traceContent.Text {
		if under(key, name) {
			return true
		}
	}
	return !slices.ContainsFunc(traceContent.Plain, func(name string) bool { return under(key, name) })
}

// never says whether an attribute is an image or an embedding vector, which no span keeps.
func never(key string) bool {
	padded := "." + key + "."
	return slices.ContainsFunc(traceContent.Never, func(name string) bool { return strings.Contains(padded, "."+name+".") })
}

func rank(key string) int {
	switch {
	case under(key, "rotor"):
		return rotorKeys
	case IsText(key):
		return textKeys
	}
	return plainKeys
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// held is one attribute redacted and cut to its limit, or false for one never kept.
func held(key, value string) (string, string, bool) {
	if never(key) {
		return "", "", false
	}
	limit := config.MaxSpanAttrChars
	if key == stackKey {
		limit = config.MaxSpanStackChars
	} else if IsText(key) {
		limit = config.MaxSpanContentChars
	}
	return cut(redact.Text(key), config.MaxSpanNameChars), cut(redact.Text(value), limit), true
}

// Hold redacts a span again and holds it to the limits of docs/api-contract-m18.md, whoever sent it.
func Hold(s RunnerSpan) RunnerSpan {
	s.Name = cut(redact.Text(s.Name), config.MaxSpanNameChars)
	s.Message = cut(redact.Text(s.Message), config.MaxSpanAttrChars)
	attrs := make(map[string]string, min(len(s.Attributes), config.MaxSpanAttributes))
	keys := slices.SortedFunc(maps.Keys(s.Attributes), func(a, b string) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	for _, k := range keys {
		if len(attrs) == config.MaxSpanAttributes {
			break
		}
		if key, value, ok := held(k, s.Attributes[k]); ok {
			attrs[key] = value
		}
	}
	s.Attributes = attrs
	events := s.Events[max(0, len(s.Events)-config.MaxSpanEvents):]
	s.Events = make([]SpanEvent, 0, len(events))
	for _, e := range events {
		out := SpanEvent{Name: cut(redact.Text(e.Name), config.MaxSpanNameChars), Time: e.Time, Attributes: make(map[string]string, len(e.Attributes))}
		for k, v := range e.Attributes {
			if key, value, ok := held(k, v); ok {
				out.Attributes[key] = value
			}
		}
		s.Events = append(s.Events, out)
	}
	s.Links = heldLinks(s.Links)
	fit(&s)
	return s
}

// heldLinks are the first links with a valid trace id and span id, each with its attributes without text alone, since
// none of them says where the link leads.
func heldLinks(links []SpanLink) []SpanLink {
	out := make([]SpanLink, 0, min(len(links), config.MaxSpanLinks))
	for _, l := range links {
		if len(out) == config.MaxSpanLinks {
			break
		}
		if !l.TraceID.IsValid() || !l.SpanID.IsValid() {
			continue
		}
		attrs := make(map[string]string, min(len(l.Attributes), config.MaxSpanLinkAttributes))
		for _, k := range slices.Sorted(maps.Keys(l.Attributes)) {
			if len(attrs) == config.MaxSpanLinkAttributes {
				break
			}
			if IsText(k) {
				continue
			}
			if key, value, ok := held(k, l.Attributes[k]); ok {
				attrs[key] = value
			}
		}
		out = append(out, SpanLink{TraceID: l.TraceID, SpanID: l.SpanID, Attributes: attrs})
	}
	return out
}
