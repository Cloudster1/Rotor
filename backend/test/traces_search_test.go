//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A search finds the spans of runs the person reads and of their own traces, and none of another member's (#261):
// through the API and straight through SQL as rotor_app, so the policies decide and not the route.
func TestSpanSearchFindsOnlyTheSpansThePersonReads(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.addPerson(t, "member"), h.addPerson(t, "member")
	theirs, others := h.runWithTrace(t, alice), h.runWithTrace(t, bob)
	marker := "search-" + uuid.NewString()[:8]
	now := time.Now()
	mine := h.seedSpans(t, h.runTraceRow(t, theirs), nil,
		seeded{name: marker + " mcp whoami", kind: "TOOL", status: "error", message: "the tool refused", at: now.Add(-time.Minute),
			attrs: map[string]string{"tool.name": "whoami", "rotor.node": "ask"}},
		seeded{name: marker + " ChatOpenAI", kind: "LLM", at: now.Add(-2 * time.Minute),
			attrs: map[string]string{"llm.model_name": "gpt-4o-2024-08-06"}},
		seeded{name: marker + " node ask", kind: "AGENT", service: marker + "-svc", at: now.Add(-3 * time.Minute)},
		seeded{name: marker + " long ago", kind: "CHAIN", at: now.AddDate(0, 0, -10)})
	ownRow, ownTraceID := h.ownTraceRow(t, alice)
	own := h.seedSpans(t, ownRow, &alice, seeded{name: marker + " my app", kind: "CHAIN", service: "my-app", at: now.Add(-4 * time.Minute)})
	hidden := h.seedSpans(t, h.runTraceRow(t, others), nil,
		seeded{name: marker + " mcp whoami", kind: "TOOL", status: "error", at: now.Add(-time.Minute),
			attrs: map[string]string{"tool.name": "whoami"}})

	a, b := h.browserFor(t, alice), h.browserFor(t, bob)
	found := searchAs(t, a, url.Values{"name": {marker}})
	if got, want := spanIDsOf(found), []string{mine[0], mine[1], mine[2], own[0]}; !slices.Equal(got, want) {
		t.Fatalf("alice finds %v, want her runs' and own spans of the last week, newest first: %v", got, want)
	}
	hits := hitsOf(found)
	if hits[0]["runId"] != theirs.id.String() || hits[0]["workflow"].(map[string]any)["id"] != theirs.workflow.String() ||
		hits[0]["owner"] != nil || hits[0]["traceId"] != theirs.traceID.String() || hits[0]["statusMessage"] != "the tool refused" {
		t.Errorf("a run's span reads %v", hits[0])
	}
	if hits[1]["model"] != "gpt-4o-2024-08-06" || hits[0]["durationMs"] != float64(250) {
		t.Errorf("the hits read %v and %v", hits[0], hits[1])
	}
	if hits[3]["runId"] != nil || hits[3]["traceId"] != ownTraceID || hits[3]["owner"].(map[string]any)["id"] != alice.String() {
		t.Errorf("an own trace's span reads %v", hits[3])
	}
	if found["next"] != nil {
		t.Errorf("a search of four hits pages on: %v", found["next"])
	}
	if got := spanIDsOf(searchAs(t, b, url.Values{"name": {marker}})); !slices.Equal(got, hidden) {
		t.Errorf("bob finds %v, want only his own run's %v", got, hidden)
	}

	// The policies decide below the route: as rotor_app, alice reads no span of bob's run, and bob none of hers.
	for person, want := range map[uuid.UUID]int{alice: len(mine) + len(own), bob: len(hidden)} {
		var count int
		err := asApp(context.Background(), h.super, person, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT count(*) FROM trace_span WHERE starts_with(name, $1)`, marker).Scan(&count)
		})
		if err != nil || count != want {
			t.Errorf("as rotor_app %v reads %d of the spans (%v), want %d", person, count, err, want)
		}
	}

	// Each filter narrows to what it names.
	filters := map[string]struct {
		q    url.Values
		want []string
	}{
		"kind":      {url.Values{"kind": {"tool"}}, mine[:1]},
		"status":    {url.Values{"status": {"error"}}, mine[:1]},
		"model":     {url.Values{"model": {"GPT-4O"}}, mine[1:2]},
		"service":   {url.Values{"service": {marker + "-svc"}}, mine[2:3]},
		"workflow":  {url.Values{"workflow": {theirs.workflow.String()}}, mine[:3]},
		"attribute": {url.Values{"attrKey": {"tool.name"}, "attrValue": {"whoami"}}, mine[:1]},
		"name":      {url.Values{"name": {"CHATOPENAI"}}, mine[1:2]},
		"window":    {url.Values{"since": {now.AddDate(0, 0, -11).Format(time.RFC3339)}}, []string{mine[0], mine[1], mine[2], own[0], mine[3]}},
		"until":     {url.Values{"until": {now.Add(-150 * time.Second).Format(time.RFC3339)}}, []string{mine[2], own[0]}},
	}
	for what, f := range filters {
		q := f.q
		if !q.Has("name") {
			q.Set("name", marker)
		}
		if got := spanIDsOf(searchAs(t, a, q)); !slices.Equal(got, f.want) {
			t.Errorf("%s: alice finds %v, want %v", what, got, f.want)
		}
	}
	got := spanIDsOf(searchAs(t, a, url.Values{"name": {marker}, "since": {now.AddDate(0, 0, -11).Format(time.RFC3339)},
		"until": {now.AddDate(0, 0, -9).Format(time.RFC3339)}}))
	if !slices.Equal(got, mine[3:]) {
		t.Errorf("a window around the old span finds %v", got)
	}
}

// The window is always bounded: never more than 30 days back, its end after its start, and text never searched.
func TestSpanSearchRefusesAnUnboundedWindowAndText(t *testing.T) {
	h := newHarness(t)
	b := h.browserFor(t, h.addPerson(t, "member"))
	now := time.Now()
	for field, q := range map[string]url.Values{
		"since":   {"since": {now.AddDate(0, 0, -31).Format(time.RFC3339)}},
		"until":   {"since": {now.Add(-time.Hour).Format(time.RFC3339)}, "until": {now.Add(-2 * time.Hour).Format(time.RFC3339)}},
		"attrKey": {"attrKey": {"input.value"}, "attrValue": {"the story"}},
		"kind":    {"kind": {"WIDGET"}},
	} {
		status, body := b.api(http.MethodGet, "/spans?"+q.Encode(), nil)
		envelope, _ := body["error"].(map[string]any)
		fields, _ := envelope["fields"].(map[string]any)
		if status != http.StatusUnprocessableEntity || envelope["code"] != "validation_failed" || fields[field] == nil {
			t.Errorf("%s: %v answers %d %v", field, q, status, body)
		}
	}
	if status, body := b.api(http.MethodGet, "/spans?cursor=made-up", nil); status != http.StatusBadRequest {
		t.Errorf("a made-up cursor answers %d %v", status, body)
	}
	answer := searchAs(t, b, url.Values{})
	since, _ := time.Parse(time.RFC3339, answer["since"].(string))
	until, _ := time.Parse(time.RFC3339, answer["until"].(string))
	if d := until.Sub(since); d != 7*24*time.Hour || answer["limit"] != float64(50) {
		t.Errorf("a search naming no window reads %v to %v, limit %v", since, until, answer["limit"])
	}
}

// Pages follow one another without a span twice or missed, ties broken alike, though spans arrive between them.
func TestSpanSearchPagesStablyNewestFirst(t *testing.T) {
	h := newHarness(t)
	person := h.addPerson(t, "member")
	r := h.runWithTrace(t, person)
	row := h.runTraceRow(t, r)
	marker := "page-" + uuid.NewString()[:8]
	now := time.Now().Truncate(time.Second)
	var spans []seeded
	for i := range 8 {
		spans = append(spans, seeded{name: marker, at: now.Add(-time.Duration(i) * time.Minute)})
	}
	// Three spans start at the same moment, so only the tie break orders them.
	for range 3 {
		spans = append(spans, seeded{name: marker, at: now.Add(-90 * time.Second)})
	}
	h.seedSpans(t, row, nil, spans...)
	b := h.browserFor(t, person)
	whole := spanIDsOf(searchAs(t, b, url.Values{"name": {marker}}))
	if len(whole) != len(spans) {
		t.Fatalf("one page finds %d of %d spans", len(whole), len(spans))
	}

	var paged []string
	q := url.Values{"name": {marker}, "limit": {"3"}}
	for page := 0; ; page++ {
		answer := searchAs(t, b, q)
		paged = append(paged, spanIDsOf(answer)...)
		if page == 0 {
			// A span arriving now, newer than every page, must not shift the pages after the first.
			h.seedSpans(t, row, nil, seeded{name: marker, at: now.Add(time.Second)})
		}
		next, _ := answer["next"].(string)
		if next == "" {
			break
		}
		if page > len(spans) {
			t.Fatal("the pages never end")
		}
		q.Set("cursor", next)
	}
	if !slices.Equal(paged, whole) {
		t.Errorf("pages of three find %v, one page %v", paged, whole)
	}
}
