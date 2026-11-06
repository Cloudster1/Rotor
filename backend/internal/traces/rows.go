package traces

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

const (
	// kindKey is OpenInference's kind of a span, which the tree shows and filters by.
	kindKey = "openinference.span.kind"
	// unknownKind is the kind of a span that names none Rotor knows.
	unknownKind = "UNKNOWN"
	// sourceRunner marks the spans a run's runner sent.
	sourceRunner = "runner"
	statusUnset  = "unset"
	statusError  = "error"
)

// kinds are OpenInference's span kinds, as trace_span.kind takes them.
var kinds = map[string]bool{"CHAIN": true, "LLM": true, "TOOL": true, "AGENT": true, "RETRIEVER": true, "EMBEDDING": true,
	"RERANKER": true, "GUARDRAIL": true, "EVALUATOR": true, "PROMPT": true, unknownKind: true}

// spanRows are a batch of spans as the columns of trace_span, one array per column, for a single insert.
type spanRows struct {
	ids, parents, names, kinds, statuses, messages, attributes, events []string
	starts, ends                                                       []time.Time
	sealed                                                             [][]byte
}

// event is a span event as trace_span.events keeps it.
type event struct {
	Name       string            `json:"name"`
	Time       time.Time         `json:"time"`
	Attributes map[string]string `json:"attributes"`
}

// rowsOf turns checked spans into rows of the trace: attributes without text kept plain, the text sealed with box,
// or dropped when box is nil. A span id seen before in the batch, or a span ending before it starts, is left out.
func rowsOf(trace uuid.UUID, spans []observability.RunnerSpan, box *envelope.Box) (spanRows, error) {
	var out spanRows
	seen := make(map[string]bool, len(spans))
	for _, s := range spans {
		id := s.SpanID.String()
		if seen[id] || s.End.Before(s.Start) {
			continue
		}
		seen[id] = true
		plain, text := split(s.Attributes)
		attrs, err := json.Marshal(plain)
		if err != nil {
			return spanRows{}, err
		}
		events := make([]event, 0, len(s.Events))
		for _, e := range s.Events {
			// An event's text has no place of its own; exceptions carry none, as api/trace-content.json says.
			eventPlain, _ := split(e.Attributes)
			events = append(events, event{Name: clean(e.Name), Time: e.Time.UTC(), Attributes: eventPlain})
		}
		eventsJSON, err := json.Marshal(events)
		if err != nil {
			return spanRows{}, err
		}
		var sealed []byte
		if box != nil && len(text) > 0 {
			content, err := json.Marshal(text)
			if err != nil {
				return spanRows{}, err
			}
			sealed = box.Seal(envelope.ColumnTraceContent, envelope.TraceSpanRow(trace, id), content)
		}
		parent := ""
		if s.ParentID.IsValid() {
			parent = s.ParentID.String()
		}
		status := statusUnset
		if s.Failed {
			status = statusError
		}
		out.ids, out.parents, out.names = append(out.ids, id), append(out.parents, parent), append(out.names, name(s.Name))
		out.kinds, out.statuses = append(out.kinds, kind(plain[kindKey])), append(out.statuses, status)
		out.messages = append(out.messages, clean(s.Message))
		out.attributes, out.events = append(out.attributes, string(attrs)), append(out.events, string(eventsJSON))
		out.starts, out.ends, out.sealed = append(out.starts, s.Start.UTC()), append(out.ends, s.End.UTC()), append(out.sealed, sealed)
	}
	return out, nil
}

// split parts attributes into those without text and those with, cleaned for Postgres.
func split(attrs map[string]string) (plain, text map[string]string) {
	plain, text = map[string]string{}, map[string]string{}
	for k, v := range attrs {
		if observability.IsText(k) {
			text[clean(k)] = clean(v)
		} else {
			plain[clean(k)] = clean(v)
		}
	}
	return plain, text
}

// clean drops NUL, which Postgres keeps in neither text nor jsonb, so one odd value does not cost the whole batch.
func clean(s string) string { return strings.ReplaceAll(s, "\x00", "") }

// name is a span's name as trace_span keeps it, which a span without one would leave empty.
func name(s string) string {
	if s = clean(s); s == "" {
		return "span"
	}
	return s
}

func kind(k string) string {
	if kinds[k] {
		return k
	}
	return unknownKind
}
