//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// A finished run's trace reads as a tree through the API and MCP (#244): the runner's spans nested as the run made
// them, the run carrying its trace id, and a member who may not see the run answered as for a run that does not exist.
func TestAFinishedRunsTraceReadsAsATreeForWhoeverReadsTheRun(t *testing.T) {
	h := newHarness(t)
	keepAssistantsOn(t, h)
	key, product := "llm-key-"+uuid.NewString(), "read-"+uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key, "llmScript": []map[string]any{
		{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."}}})
	bob, _, _ := connected(t, stub)
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)
	run := startRun(t, bob, createWorkflow(t, bob, agentWorkflow(product, provider["name"].(string))), map[string]any{}, 201)
	runID := run["id"].(string)
	if done := waitRun(t, bob, runID, "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the run ended %v: %v", done["status"], done["reason"])
	}
	traceID, _ := traceAnswer(t, bob, "/runs/"+runID)["traceId"].(string)
	if len(traceID) != 32 {
		t.Fatalf("the run carries the trace id %q", traceID)
	}

	// The runner's last spans come right before its Done and are stored beside the run ending.
	var answer map[string]any
	var tool []string
	llm := map[string]any{}
	for deadline := time.Now().Add(traceWrite); ; time.Sleep(time.Second) {
		answer = traceAnswer(t, bob, "/runs/"+runID+"/trace")
		tool = pathTo(rootsOf(answer), "mcp whoami")
		eachSpan(rootsOf(answer), func(s map[string]any) {
			if s["kind"] == "LLM" {
				llm = s
			}
		})
		if len(tool) > 0 && len(llm) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run's trace reads %v", answer)
		}
	}
	tr := answer["trace"].(map[string]any)
	if tr["traceId"] != traceID || tr["runId"] != runID || tr["state"] != "kept" || tr["sender"] != nil || tr["expiresAt"] == nil ||
		tr["links"] == nil || len(tr["links"].([]any)) != 0 {
		t.Errorf("the trace reads %v", tr)
	}
	if tool[0] != "run" || !slices.Contains(tool, "node ask") {
		t.Errorf("the MCP call nests as %v, want it under the run and its node", tool)
	}
	if llm["model"] == "" || llm["durationMs"] == nil {
		t.Errorf("the model call reads %v", llm)
	}
	eachSpan(rootsOf(answer), func(s map[string]any) {
		attrs := s["attributes"].(map[string]any)
		if _, text := attrs["input.value"]; text || attrs["rotor.run"] != runID || s["source"] != "runner" {
			t.Errorf("span %v has the attributes %v", s["name"], attrs)
		}
	})
	listed := traceAnswer(t, bob, "/runs?limit=5")["runs"].([]any)
	if !slices.ContainsFunc(listed, func(r any) bool { return r.(map[string]any)["traceId"] == traceID }) {
		t.Error("the run's trace id is missing from the list of runs")
	}
	spanID := llm["spanId"].(string)
	span := traceAnswer(t, bob, "/runs/"+runID+"/trace/spans/"+spanID)["span"].(map[string]any)
	if span["spanId"] != spanID || span["content"] == nil || span["children"] != nil {
		t.Errorf("one span reads %v", span)
	}

	// Nobody else learns the run exists: its trace and its spans answer as a run's that does not exist.
	other := h.browserFor(t, h.addPerson(t, "member"))
	missing := "/runs/" + uuid.NewString() + "/trace"
	if got, want := refusedAs(other, "/runs/"+runID+"/trace"), refusedAs(other, missing); got != want || got[0] != http.StatusNotFound {
		t.Errorf("a member reads another's trace as %v, a missing run's as %v", got, want)
	}
	if got, want := refusedAs(other, "/runs/"+runID+"/trace/spans/"+spanID), refusedAs(other, missing+"/spans/"+spanID); got != want {
		t.Errorf("a member reads another's span as %v, a missing run's as %v", got, want)
	}
	if got := refusedAs(bob, "/traces/"+traceID); got[0] != http.StatusNotFound {
		t.Errorf("a run's trace reads as an own trace: %v", got)
	}

	// An assistant reads the same tree shortened, and one acting for the member reads nothing of it.
	_, access := connectHand(t, h, bob, map[string]any{"allow": true})
	shortened := structured(t, callTool(t, access, "get_run_trace", map[string]any{"runID": runID}))
	if path := pathTo(rootsOf(shortened), "mcp whoami"); !slices.Equal(path, tool) {
		t.Errorf("get_run_trace nests the MCP call as %v, the API as %v", path, tool)
	}
	eachSpan(rootsOf(shortened), func(s map[string]any) {
		if _, ok := s["attributes"].(map[string]any)["rotor.run"]; ok {
			t.Errorf("get_run_trace keeps every attribute of %v", s["name"])
		}
	})
	whole := structured(t, callTool(t, access, "get_run_trace_span", map[string]any{"runID": runID, "spanID": spanID}))
	if whole["span"].(map[string]any)["spanId"] != spanID {
		t.Errorf("get_run_trace_span reads %v", whole)
	}
	_, theirs := connectHand(t, h, other, map[string]any{"allow": true})
	got := callTool(t, theirs, "get_run_trace", map[string]any{"runID": runID})
	want := callTool(t, theirs, "get_run_trace", map[string]any{"runID": uuid.NewString()})
	if got["isError"] != true || toolText(got) != toolText(want) {
		t.Errorf("another member's assistant reads %v, for a missing run %v", got, want)
	}
}

// toolText is what a tool call said in words.
func toolText(called map[string]any) string {
	content, _ := called["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	return text
}

// A trace says what Rotor holds of it (#244): none before its first span, expired once swept with its counts kept,
// and cut once it reached its cap.
func TestATraceSaysWhetherItIsKeptExpiredOrCut(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	person := h.addPerson(t, "member")
	b := h.browserFor(t, person)

	none := h.runWithTrace(t, person)
	tr := traceAnswer(t, b, "/runs/"+none.id.String()+"/trace")["trace"].(map[string]any)
	if tr["state"] != "none" || tr["traceId"] != none.traceID.String() || len(tr["roots"].([]any)) != 0 || tr["expiresAt"] != nil {
		t.Errorf("a run without spans has the trace %v", tr)
	}

	expired := h.runWithTrace(t, person)
	spans := runnerSpans(expired, 3)
	if err := h.traceStore(t).Keep(ctx, expired.id, expired.dataKey, observability.AllTracing, spans); err != nil {
		t.Fatal(err)
	}
	kept, _ := h.keptTraceOf(t, expired.id)
	if _, err := h.super.Exec(ctx, `DELETE FROM trace_span WHERE trace = $1`, kept.id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE trace SET expired_at = now(), last_span_at = now() - make_interval(days => $2)
		WHERE id = $1`, kept.id, config.MaxTraceRetentionDays+1); err != nil {
		t.Fatal(err)
	}
	tr = traceAnswer(t, b, "/runs/"+expired.id.String()+"/trace")["trace"].(map[string]any)
	expires, _ := time.Parse(time.RFC3339Nano, tr["expiresAt"].(string))
	if tr["state"] != "expired" || len(tr["roots"].([]any)) != 0 || tr["spans"] != float64(3) || !expires.Before(time.Now()) {
		t.Errorf("an expired trace reads %v", tr)
	}
	if got := refusedAs(b, "/runs/"+expired.id.String()+"/trace/spans/"+spans[0].SpanID.String()); got[0] != http.StatusNotFound ||
		!strings.Contains(got[2].(string), "span") {
		t.Errorf("an expired span reads %v", got)
	}

	cut := h.runWithTrace(t, person)
	if err := h.traceStore(t).Keep(ctx, cut.id, cut.dataKey, observability.AllTracing, runnerSpans(cut, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE trace SET cut_at = now(), dropped = 7 WHERE run_id = $1`, cut.id); err != nil {
		t.Fatal(err)
	}
	tr = traceAnswer(t, b, "/runs/"+cut.id.String()+"/trace")["trace"].(map[string]any)
	if tr["state"] != "kept" || tr["cut"] != true || tr["dropped"] != float64(7) || len(tr["roots"].([]any)) != 2 {
		t.Errorf("a cut trace reads %v", tr)
	}
}

// One span reads whole (#244): its whole input and every text attribute while Rotor kept text when it was stored,
// none of it otherwise, and the tree previews it.
func TestOneSpanReadsWholeWithItsTextOnlyWhereTextWasKept(t *testing.T) {
	h := newHarness(t)
	keepAssistantsOn(t, h)
	ctx := context.Background()
	person := h.addPerson(t, "member")
	b := h.browserFor(t, person)
	r := h.runWithTrace(t, person)
	long := strings.Repeat("a story ", 100)
	with := stepSpan(r, trace.SpanID{}, "node ask", "AGENT", long, time.Now())
	with.Attributes["input.mime_type"] = "text/plain"
	h.keepContent(t, true)
	if err := h.traceStore(t).Keep(ctx, r.id, r.dataKey, observability.AllTracing, []observability.RunnerSpan{with}); err != nil {
		t.Fatal(err)
	}
	h.keepContent(t, false)
	without := stepSpan(r, with.SpanID, "ChatOpenAI", "LLM", long, time.Now().Add(time.Millisecond))
	if err := h.traceStore(t).Keep(ctx, r.id, r.dataKey, observability.AllTracing, []observability.RunnerSpan{without}); err != nil {
		t.Fatal(err)
	}

	base := "/runs/" + r.id.String() + "/trace"
	tree := traceAnswer(t, b, base)
	if tree["trace"].(map[string]any)["content"] != true {
		t.Error("the trace does not say it kept text")
	}
	root := rootsOf(tree)[0].(map[string]any)
	in := root["input"].(map[string]any)
	if len(in["value"].(string)) != config.TracePreviewChars || in["cut"] != true || in["mimeType"] != "text/plain" {
		t.Errorf("the tree previews the input as %v", in)
	}
	if child := root["children"].([]any)[0].(map[string]any); child["input"] != nil || child["output"] != nil {
		t.Errorf("a span stored without text previews %v", child["input"])
	}

	span := traceAnswer(t, b, base+"/spans/"+with.SpanID.String())["span"].(map[string]any)
	content := span["content"].(map[string]any)
	if span["input"].(map[string]any)["value"] != long || span["input"].(map[string]any)["cut"] != false ||
		content["input.value"] != long || content["output.value"] != long {
		t.Errorf("the span whole reads %v", span)
	}
	if _, plain := span["attributes"].(map[string]any)["input.value"]; plain {
		t.Error("the span's text is among its attributes")
	}
	bare := traceAnswer(t, b, base+"/spans/"+without.SpanID.String())["span"].(map[string]any)
	if bare["input"] != nil || bare["output"] != nil || len(bare["content"].(map[string]any)) != 0 {
		t.Errorf("a span stored without text reads %v", bare)
	}

	_, access := connectHand(t, h, b, map[string]any{"allow": true})
	whole := structured(t, callTool(t, access, "get_run_trace_span", map[string]any{"runID": r.id.String(), "spanID": with.SpanID.String()}))
	if whole["span"].(map[string]any)["input"].(map[string]any)["value"] != long {
		t.Errorf("get_run_trace_span reads %v", whole)
	}
}

// A sender's own traces are read by the sender and administrators alone (#244), and a member asking for another's
// learns nothing of it.
func TestASendersOwnTracesAreReadByTheSenderAndAdministratorsAlone(t *testing.T) {
	h := newHarness(t)
	sender := h.addPerson(t, "member")
	traceID := h.ownTrace(t, sender)
	mine := h.browserFor(t, sender)
	of := "?sender=" + sender.String()

	page := traceAnswer(t, mine, "/traces")
	listed := page["traces"].([]any)
	if page["total"] != float64(1) || len(listed) != 1 {
		t.Fatalf("the sender lists %v", page)
	}
	first := listed[0].(map[string]any)
	if first["traceId"] != traceID || first["rootName"] != "chat" || first["service"] != "my-app" || first["failed"] != true ||
		first["spans"] != float64(2) || first["sender"].(map[string]any)["id"] != sender.String() {
		t.Errorf("the sender's trace is listed as %v", first)
	}
	tr := traceAnswer(t, mine, "/traces/"+traceID)["trace"].(map[string]any)
	if tr["runId"] != nil || tr["sender"].(map[string]any)["name"] != "The sender" || tr["state"] != "kept" {
		t.Errorf("the sender reads the trace as %v", tr)
	}
	if path := pathTo(tr["roots"].([]any), "ChatOpenAI"); !slices.Equal(path, []string{"chat", "ChatOpenAI"}) {
		t.Errorf("the trace nests as %v", path)
	}
	span := traceAnswer(t, mine, "/traces/"+traceID+"/spans/2222222222222222")["span"].(map[string]any)
	if span["status"] != "error" || span["statusMessage"] != "rate limited" || span["sender"].(map[string]any)["id"] != sender.String() {
		t.Errorf("the sender reads the span as %v", span)
	}

	other := h.browserFor(t, h.addPerson(t, "member"))
	missing := refusedAs(other, "/traces/"+randomTraceID()+of)
	for _, path := range []string{"/traces/" + traceID + of, "/traces/" + traceID, "/traces/" + traceID + "/spans/2222222222222222" + of} {
		if got := refusedAs(other, path); got != missing || got[0] != http.StatusNotFound {
			t.Errorf("another member reads %s as %v, a missing trace as %v", path, got, missing)
		}
	}
	if page := traceAnswer(t, other, "/traces"+of); page["total"] != float64(0) {
		t.Errorf("another member lists the sender's traces: %v", page)
	}

	admin := h.browserFor(t, h.addPerson(t, "admin"))
	if tr := traceAnswer(t, admin, "/traces/"+traceID+of)["trace"].(map[string]any); tr["traceId"] != traceID {
		t.Errorf("an administrator reads %v", tr)
	}
	traceAnswer(t, admin, "/traces/"+traceID+"/spans/1111111111111111"+of)
	if page := traceAnswer(t, admin, "/traces"+of); page["total"] != float64(1) {
		t.Errorf("an administrator lists %v", page)
	}
	if got := refusedAs(admin, "/traces/"+traceID); got[0] != http.StatusNotFound {
		t.Errorf("an administrator's own trace of that id reads %v", got)
	}
}
