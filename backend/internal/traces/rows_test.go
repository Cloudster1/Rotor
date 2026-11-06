package traces

import (
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

func testBox(t *testing.T, run uuid.UUID) *envelope.Box {
	t.Helper()
	key := make([]byte, config.DataKeyBits/8)
	_, _ = rand.Read(key)
	box, err := envelope.NewBox(run, key)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func span(id byte, start time.Time, attrs map[string]string) observability.RunnerSpan {
	return observability.RunnerSpan{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{id}, ParentID: trace.SpanID{9},
		Name: "node ask", Start: start, End: start.Add(time.Second), Attributes: attrs}
}

// A span's text is sealed with the run's key for its own trace and span alone, and kept nowhere in the clear; the
// attributes without it stay plain for the tree.
func TestASpansTextIsSealedForItsTraceAndSpanAlone(t *testing.T) {
	run, trace := uuid.New(), uuid.New()
	box := testBox(t, run)
	now := time.Now()
	s := span(1, now, map[string]string{"input.value": "the story", "openinference.span.kind": "AGENT", "rotor.node": "ask"})
	s.Events = []observability.SpanEvent{{Name: "exception", Time: now, Attributes: map[string]string{"exception.message": "boom"}}}
	s.Failed = true
	rows, err := rowsOf(trace, []observability.RunnerSpan{s}, box)
	if err != nil || len(rows.ids) != 1 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	if rows.kinds[0] != "AGENT" || rows.statuses[0] != statusError || rows.parents[0] != s.ParentID.String() {
		t.Errorf("the row reads kind %q, status %q, parent %q", rows.kinds[0], rows.statuses[0], rows.parents[0])
	}
	var plain map[string]string
	if err := json.Unmarshal([]byte(rows.attributes[0]), &plain); err != nil || plain["rotor.node"] != "ask" {
		t.Errorf("the plain attributes are %s (%v)", rows.attributes[0], err)
	}
	if _, ok := plain["input.value"]; ok {
		t.Error("the input is kept in the clear")
	}
	var events []Event
	if err := json.Unmarshal([]byte(rows.events[0]), &events); err != nil || len(events) != 1 || events[0].Attributes["exception.message"] != "boom" {
		t.Errorf("the events are %s (%v)", rows.events[0], err)
	}

	id := s.SpanID.String()
	opened, err := box.Open(envelope.ColumnTraceContent, envelope.TraceSpanRow(trace, id), rows.sealed[0])
	var text map[string]string
	if err != nil || json.Unmarshal(opened, &text) != nil || text["input.value"] != "the story" {
		t.Fatalf("the sealed text opens as %q, %v", opened, err)
	}
	if _, err := box.Open(envelope.ColumnTraceContent, envelope.TraceSpanRow(uuid.New(), id), rows.sealed[0]); err == nil {
		t.Error("the text opens as another trace's")
	}
	if _, err := testBox(t, uuid.New()).Open(envelope.ColumnTraceContent, envelope.TraceSpanRow(trace, id), rows.sealed[0]); err == nil {
		t.Error("the text opens with another run's key")
	}
}

// Without a box Rotor keeps no text; a span the database would refuse, or one twice, does not cost the batch.
func TestRowsKeepNoTextWithoutABoxAndLeaveOutWhatPostgresRefuses(t *testing.T) {
	now := time.Now()
	backwards := span(3, now, nil)
	backwards.End = now.Add(-time.Second)
	unnamed := span(4, now, map[string]string{"openinference.span.kind": "SOMETHING", "tool.name": "who\x00ami"})
	unnamed.Name = "\x00"
	rows, err := rowsOf(uuid.New(), []observability.RunnerSpan{
		span(1, now, map[string]string{"output.value": "secret answer"}), span(1, now, nil), backwards, unnamed,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.ids) != 2 {
		t.Fatalf("kept %d spans, want the first of the twins and the unnamed one", len(rows.ids))
	}
	if rows.sealed[0] != nil || rows.attributes[0] != "{}" {
		t.Errorf("without a box the span keeps %q and %s", rows.sealed[0], rows.attributes[0])
	}
	if rows.names[1] == "" || rows.kinds[1] != unknownKind || rows.attributes[1] != `{"openinference.span.kind":"SOMETHING","tool.name":"whoami"}` {
		t.Errorf("the unnamed span is %q of kind %q with %s", rows.names[1], rows.kinds[1], rows.attributes[1])
	}
}
