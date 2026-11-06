//go:build integration

package test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces"
)

// An assistant searches spans as the person it acts for (#261): a page stops within the tool's budget and pages on
// from where it stopped, and one acting for another member finds nothing of the person's runs.
func TestSpanSearchAnswersAnAssistantWithinItsBudget(t *testing.T) {
	h := newHarness(t)
	keepAssistantsOn(t, h)
	person := h.addPerson(t, "member")
	r := h.runWithTrace(t, person)
	marker := "mcp-" + uuid.NewString()[:8]
	now := time.Now()
	var spans []seeded
	for i := range 60 {
		name := marker + strings.Repeat("n", config.MaxSpanNameChars-len(marker))
		spans = append(spans, seeded{name: name, kind: "TOOL", status: "error", message: strings.Repeat("m", config.MaxSpanAttrChars),
			at: now.Add(-time.Duration(i) * time.Second)})
	}
	ids := h.seedSpans(t, h.runTraceRow(t, r), nil, spans...)

	_, access := connectHand(t, h, h.browserFor(t, person), map[string]any{"allow": true})
	var found []string
	args := map[string]any{"name": marker, "status": "error", "limit": config.MCPSpanSearchMaxPage}
	for page := 0; ; page++ {
		began := time.Now()
		called := callTool(t, access, "search_spans", args)
		if took := time.Since(began); took > config.SpanSearchTimeout {
			t.Errorf("search_spans took %v", took)
		}
		if size := len(toolText(called)); size > config.MCPMaxToolResultBytes {
			t.Fatalf("search_spans answers %d bytes, over %d", size, config.MCPMaxToolResultBytes)
		}
		answer := structured(t, called)
		found = append(found, spanIDsOf(answer)...)
		if page == 0 && len(found) >= config.MCPSpanSearchMaxPage {
			t.Errorf("the first page holds %d spans of about a KiB each, past its budget", len(found))
		}
		next, _ := answer["next"].(string)
		if next == "" || page > len(ids) {
			break
		}
		args["cursor"] = next
	}
	if !slices.Equal(found, ids) {
		t.Errorf("the pages find %d spans, want all %d newest first", len(found), len(ids))
	}

	_, theirs := connectHand(t, h, h.browserFor(t, h.addPerson(t, "member")), map[string]any{"allow": true})
	if got := spanIDsOf(structured(t, callTool(t, theirs, "search_spans", map[string]any{"name": marker}))); len(got) != 0 {
		t.Errorf("another member's assistant finds %v", got)
	}
}

// The search walks an index newest first and stops at a page, or for one workflow reads its runs' traces, so it stays
// bounded on a busy installation: explained as rotor_app over tens of thousands of spans.
func TestSpanSearchWalksAnIndex(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	person := h.addPerson(t, "member")
	const runs, perRun = 6, 5000
	var workflow uuid.UUID
	for range runs {
		r := h.runWithTrace(t, person)
		workflow = r.workflow
		row := h.runTraceRow(t, r)
		if _, err := h.super.Exec(ctx, `INSERT INTO trace_span (trace, span_id, name, kind, service, source, started_at, ended_at,
				status, attributes)
			SELECT $1, lpad(to_hex(n), 16, '0'), 'span ' || n, (ARRAY['CHAIN', 'LLM', 'TOOL', 'AGENT'])[1 + n % 4], 'rotor-runner',
				'runner', now() - n * interval '1 second', now() - n * interval '1 second', CASE WHEN n % 50 = 0 THEN 'error' ELSE 'ok' END,
				jsonb_build_object('tool.name', 'tool ' || n % 10)
			FROM generate_series(1, $2) AS n`, row, perRun); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.super.Exec(ctx, `ANALYZE trace_span`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	since, until := traces.Window(nil, nil, now)
	for what, c := range map[string]struct {
		index string
		f     traces.SpanFilter
	}{
		"everything":   {"trace_span_started on trace_span", traces.SpanFilter{}},
		"a kind":       {"trace_span_kind_started on trace_span", traces.SpanFilter{Kind: "TOOL"}},
		"failed spans": {"trace_span_failed_started on trace_span", traces.SpanFilter{Status: "error"}},
		"a workflow":   {"run_workflow_idx on run", traces.SpanFilter{Workflow: &workflow}},
		"a name":       {"trace_span_started on trace_span", traces.SpanFilter{Name: "span 4"}},
		"an attribute": {"trace_span_started on trace_span", traces.SpanFilter{AttrKey: "tool.name", AttrValue: "tool 3"}},
	} {
		f, want := c.f, c.index
		f.Since, f.Until, f.Limit = since, until, config.SpanSearchPageSize
		query, args := traces.SearchQuery(f)
		var plan []string
		err := asApp(ctx, h.super, person, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY ON) "+query, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					return err
				}
				plan = append(plan, line)
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Join(plan, "\n")
		t.Logf("%s, %s:\n%s", what, want, text)
		if !strings.Contains(text, "using "+want) || strings.Contains(text, "Seq Scan on trace_span") {
			t.Errorf("the search %+v does not walk %s:\n%s", f, want, text)
		}
	}
}
