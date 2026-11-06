//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sealing "github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/traces"
	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// A LangChain app sends its spans as protobuf and as OTLP's JSON, gzip or not, and they land in a trace of its sender's
// own, as the sender with what Rotor says of it, read back through SQL by the sender and by no other member.
func TestAnAppsSpansAreTakenAsProtobufAndJSONGzipOrNot(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.keepText(t, false)
	sender, stranger := h.addPerson(t, "member"), h.addPerson(t, "member")
	token := h.appToken(t, sender, false)

	for name, c := range map[string]struct {
		contentType string
		gzip        bool
	}{
		"protobuf": {mediaProtobuf, false}, "gzipped protobuf": {mediaProtobuf, true},
		"JSON": {mediaJSON, false}, "gzipped JSON": {mediaJSON, true},
	} {
		traceID := randomTraceID()
		root := appSpan(traceID, "LangGraph", map[string]string{"openinference.span.kind": "CHAIN", "rotor.run": uuid.NewString(),
			"rotor.sender": uuid.NewString(), "input.value": "the question"})
		model := appSpan(traceID, "ChatOpenAI", map[string]string{"openinference.span.kind": "LLM", "llm.model_name": "gpt-5"})
		model.parent = root.spanID
		body := protobufOf(t, root, model)
		if c.contentType == mediaJSON {
			body = jsonOf(t, root, model)
		}
		encoding := ""
		if c.gzip {
			body, encoding = gzipOf(t, body), "gzip"
		}
		status, header, answer := sendSpans(t, otlpCall{token: token, contentType: c.contentType, encoding: encoding, body: body})
		if status != http.StatusOK || header.Get("Content-Type") != c.contentType {
			t.Fatalf("%s = %d %q %q", name, status, header.Get("Content-Type"), answer)
		}
		if c.contentType == mediaJSON {
			if n, why := partial(t, answer); n != 0 {
				t.Errorf("%s: %d spans were not taken: %s", name, n, why)
			}
		}

		kept, ok := h.keptSpanOf(t, traceID, model.spanID)
		if !ok {
			t.Fatalf("%s: the model's span was not kept", name)
		}
		if kept.runID != nil || kept.ownerID == nil || *kept.ownerID != sender || kept.senderID == nil || *kept.senderID != sender ||
			kept.senderName != "Test person" || kept.source != "otlp" || kept.service != appService || kept.kind != "LLM" ||
			kept.status != "ok" || kept.parent != root.spanID || kept.name != "ChatOpenAI" {
			t.Errorf("%s: kept %+v", name, kept)
		}
		rootKept, _ := h.keptSpanOf(t, traceID, root.spanID)
		if rootKept.attributes["rotor.sender"] != sender.String() || rootKept.attributes["rotor.run"] != "" ||
			rootKept.attributes["input.value"] != "" || rootKept.sealed != nil {
			t.Errorf("%s: the root's attributes are %v, text sealed %v; want Rotor's sender, no forged keys and no text",
				name, rootKept.attributes, rootKept.sealed != nil)
		}

		// The sender reads its own trace through SQL; another member does not.
		for who, want := range map[uuid.UUID]int{sender: 2, stranger: 0} {
			var n int
			if err := asApp(ctx, h.super, who, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM trace_span s JOIN trace t ON t.id = s.trace WHERE t.trace_id = $1`, traceID).Scan(&n)
			}); err != nil {
				t.Fatal(err)
			}
			if n != want {
				t.Errorf("%s: %s reads %d of the spans, want %d", name, who, n, want)
			}
		}
	}
}

// A span naming a run joins the run's trace when its sender reads the run, its text sealed with the run's key; a span
// of someone who may not read the run lands in their own trace, answered exactly alike, and the run never sees it.
func TestASpanJoinsARunOnlyWhenItsSenderReadsTheRun(t *testing.T) {
	h := newHarness(t)
	h.keepText(t, true)
	starter, stranger := h.addPerson(t, "member"), h.addPerson(t, "member")
	r := h.runWithTrace(t, starter)
	traceID := r.traceID.String()

	tool := appSpan(traceID, "whoami", map[string]string{"openinference.span.kind": "TOOL", "input.value": "who am I"})
	_, _, joined := sendSpans(t, otlpCall{token: h.appToken(t, starter, false), contentType: mediaJSON, body: jsonOf(t, tool)})
	forged := appSpan(traceID, "whoami", map[string]string{"openinference.span.kind": "TOOL", "input.value": "let me in"})
	status, _, refused := sendSpans(t, otlpCall{token: h.appToken(t, stranger, false), contentType: mediaJSON, body: jsonOf(t, forged)})
	if status != http.StatusOK || string(joined) != string(refused) {
		t.Fatalf("a stranger naming the run is answered %d %q, the starter %q; want the same", status, refused, joined)
	}

	kept, ok := h.keptSpanOf(t, traceID, tool.spanID)
	if !ok || kept.runID == nil || *kept.runID != r.id || kept.attributes["rotor.run"] != r.id.String() ||
		kept.attributes["rotor.workflow"] != r.workflow.String() || kept.sealed == nil {
		t.Fatalf("the starter's span was kept as %+v, %v", kept, ok)
	}
	text, err := h.runBox(t, r.id).Open(sealing.ColumnTraceContent, sealing.TraceSpanRow(kept.trace, tool.spanID), kept.sealed)
	var content map[string]string
	if err != nil || json.Unmarshal(text, &content) != nil || content["input.value"] != "who am I" {
		t.Errorf("the run's key opens %q, %v", text, err)
	}
	if traces, spans := h.spansReadBy(t, starter, r.id); traces != 1 || spans != 1 {
		t.Errorf("the starter reads %d traces and %d spans of the run, want the one span", traces, spans)
	}

	own, ok := h.keptSpanOf(t, traceID, forged.spanID)
	if !ok || own.runID != nil || own.ownerID == nil || *own.ownerID != stranger || own.trace == kept.trace ||
		own.attributes["rotor.run"] != "" {
		t.Errorf("the stranger's span was kept as %+v, %v; want a trace of the stranger's own", own, ok)
	}
	if traces, spans := h.spansReadBy(t, stranger, r.id); traces != 0 || spans != 0 {
		t.Errorf("the stranger reads %d traces and %d spans of the run", traces, spans)
	}
}

// An owned trace made without text gets a key of its own the first time it seals some, and keeps it.
func TestASendersOwnTraceSealsItsTextWithAKeyOfItsOwn(t *testing.T) {
	h := newHarness(t)
	h.keepText(t, true)
	sender := h.addPerson(t, "member")
	token := h.appToken(t, sender, false)
	traceID := randomTraceID()
	plain := appSpan(traceID, "LangGraph", map[string]string{"openinference.span.kind": "CHAIN"})
	sendSpans(t, otlpCall{token: token, contentType: mediaProtobuf, body: protobufOf(t, plain)})
	first, _ := h.keptSpanOf(t, traceID, plain.spanID)
	if first.dataKey != nil || first.sealed != nil {
		t.Fatalf("a trace without text was given a key %v", first.dataKey)
	}

	worded := appSpan(traceID, "ChatOpenAI", map[string]string{"openinference.span.kind": "LLM", "output.value": "the answer"})
	sendSpans(t, otlpCall{token: token, contentType: mediaProtobuf, body: protobufOf(t, worded)})
	kept, ok := h.keptSpanOf(t, traceID, worded.spanID)
	if !ok || kept.dataKey == nil || kept.sealed == nil || kept.trace != first.trace {
		t.Fatalf("the worded span was kept as %+v", kept)
	}
	box, err := runKeys(t).For(context.Background(), kept.trace, *kept.dataKey)
	if err != nil {
		t.Fatal(err)
	}
	text, err := box.Open(sealing.ColumnTraceContent, sealing.TraceSpanRow(kept.trace, worded.spanID), kept.sealed)
	if err != nil || !strings.Contains(string(text), "the answer") {
		t.Errorf("the trace's key opens %q, %v", text, err)
	}
}

// Spans that fail a check, or that the cap's trigger skips, are named in partial_success with their count.
func TestSpansNotTakenAreNamedInPartialSuccess(t *testing.T) {
	h := newHarness(t)
	sender := h.addPerson(t, "member")
	token := h.appToken(t, sender, false)
	traceID := randomTraceID()
	good := appSpan(traceID, "LangGraph", nil)
	nameless := appSpan(traceID, "", nil)
	twice := good
	status, _, body := sendSpans(t, otlpCall{token: token, contentType: mediaJSON, body: jsonOf(t, good, nameless, twice)})
	n, why := partial(t, body)
	if status != http.StatusOK || n != 2 || !strings.Contains(why, otlp.ReasonName) || !strings.Contains(why, otlp.ReasonTwice) {
		t.Errorf("a request with two bad spans = %d, %d not taken: %q", status, n, why)
	}

	// A sender whose own traces hold all it may keep has the trigger skip the next span, whoever inserts it.
	ctx := context.Background()
	if _, err := h.super.Exec(ctx, `UPDATE trace SET spans = 100000 WHERE owner_id = $1`, sender); err != nil {
		t.Fatal(err)
	}
	status, _, body = sendSpans(t, otlpCall{token: token, contentType: mediaJSON, body: jsonOf(t, appSpan(randomTraceID(), "more", nil))})
	if n, why := partial(t, body); status != http.StatusOK || n != 1 || !strings.Contains(why, traces.ReasonCapped) {
		t.Errorf("a span past the sender's cap = %d, %d not taken: %q", status, n, why)
	}
	var dropped int
	if err := h.super.QueryRow(ctx, `SELECT coalesce(sum(dropped), 0) FROM trace WHERE owner_id = $1`, sender).Scan(&dropped); err != nil || dropped != 1 {
		t.Errorf("the sender's traces count %d dropped spans, %v", dropped, err)
	}
}
