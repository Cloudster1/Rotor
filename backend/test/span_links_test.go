//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// The spans ownTrace stores, which a link names.
const ownTraceRoot = "1111111111111111"

// A span's links are kept without their text and answered with where each leads: the run's trace and the reader's own
// trace by their ids, and a stranger's trace exactly as a trace Rotor never held (#262).
func TestSpanLinksAreKeptAndLeadOnlyWhereTheReaderMayRead(t *testing.T) {
	h := newHarness(t)
	h.keepText(t, false)
	sender, stranger := h.addPerson(t, "member"), h.addPerson(t, "member")
	token := h.appToken(t, sender, false)
	exp := stockExporter(t, "Bearer "+token)

	run := h.runWithTrace(t, sender)
	inRun := grpcSpanOf(run.traceID.String(), "tool call", nil)
	if err := exportSpans(t, exp, inRun); err != nil {
		t.Fatalf("a span into the run's trace: %v", err)
	}
	mine, theirs, nowhere := h.ownTrace(t, sender), h.ownTrace(t, stranger), randomTraceID()

	batch := grpcSpanOf(randomTraceID(), "handle batch", nil)
	attrs := map[string]string{"llm.system": "openai", "input.value": "the message a person wrote"}
	batch.links = []sdktrace.Link{linkTo(t, run.traceID.String(), inRun.spanID, attrs), linkTo(t, mine, ownTraceRoot, nil),
		linkTo(t, theirs, ownTraceRoot, nil), linkTo(t, nowhere, ownTraceRoot, nil)}
	if err := exportSpans(t, exp, batch); err != nil {
		t.Fatalf("the span with links: %v", err)
	}

	var stored []map[string]any
	var raw []byte
	if err := h.super.QueryRow(context.Background(), `SELECT s.links FROM trace_span s JOIN trace t ON t.id = s.trace
		WHERE t.trace_id = $1 AND s.span_id = $2`, batch.traceID, batch.spanID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &stored); err != nil || len(stored) != 4 {
		t.Fatalf("stored links %s: %v", raw, err)
	}
	if kept, _ := stored[0]["attributes"].(map[string]any); kept["llm.system"] != "openai" || kept["input.value"] != nil {
		t.Errorf("a link keeps the attributes %v; want its attributes without text alone", kept)
	}

	reader := h.browserFor(t, sender)
	for _, path := range []string{"/traces/" + batch.traceID, "/traces/" + batch.traceID + "/spans/" + batch.spanID} {
		links := linksOf(t, traceAnswer(t, reader, path))
		if len(links) != 4 {
			t.Fatalf("%s answers the links %v", path, links)
		}
		want := []struct{ runID, sender any }{{run.id.String(), nil}, {nil, sender.String()}, {nil, nil}, {nil, nil}}
		for i, w := range want {
			if links[i]["runId"] != w.runID || links[i]["sender"] != w.sender {
				t.Errorf("%s: link %d = %v, want run %v and sender %v", path, i, links[i], w.runID, w.sender)
			}
		}
		// A stranger's trace and one Rotor never held are answered alike, but for their ids.
		theirLink, noLink := links[2], links[3]
		delete(theirLink, "traceId")
		delete(noLink, "traceId")
		if a, b := encodedJSON(t, theirLink), encodedJSON(t, noLink); a != b {
			t.Errorf("%s tells a stranger's trace %s from none %s", path, a, b)
		}
	}
	if status, _ := reader.api(http.MethodGet, "/runs/"+run.id.String()+"/trace", nil); status != http.StatusOK {
		t.Errorf("the run the link leads to = %d", status)
	}
}

// linksOf is the links of the first root of a trace's answer, or of a span's.
func linksOf(t *testing.T, answer map[string]any) []map[string]any {
	t.Helper()
	span, _ := answer["span"].(map[string]any)
	if span == nil {
		roots := rootsOf(answer)
		if len(roots) == 0 {
			t.Fatalf("no span in %v", answer)
		}
		span, _ = roots[0].(map[string]any)
	}
	list, _ := span["links"].([]any)
	out := make([]map[string]any, 0, len(list))
	for _, l := range list {
		link, _ := l.(map[string]any)
		out = append(out, link)
	}
	return out
}

func encodedJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
