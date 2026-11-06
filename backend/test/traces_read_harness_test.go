//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/observability"
)

// traceAnswer is what b reads of a run's trace, or of a span of it.
func traceAnswer(t *testing.T, b *browser, path string) map[string]any {
	t.Helper()
	status, body := b.api(http.MethodGet, path, nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s: %d %v", path, status, body)
	}
	return body
}

// refusedAs is the status, code and sentence of a refusal, which tell a missing run from one the caller may not see.
func refusedAs(b *browser, path string) [3]any {
	status, body := b.api(http.MethodGet, path, nil)
	envelope, _ := body["error"].(map[string]any)
	return [3]any{status, envelope["code"], envelope["message"]}
}

// pathTo names the spans from a root down to the first span named name, nil when the tree holds none.
func pathTo(spans []any, name string) []string {
	for _, raw := range spans {
		s := raw.(map[string]any)
		if s["name"] == name {
			return []string{name}
		}
		children, _ := s["children"].([]any)
		if below := pathTo(children, name); below != nil {
			return append([]string{s["name"].(string)}, below...)
		}
	}
	return nil
}

// eachSpan visits every span of a tree answer.
func eachSpan(spans []any, visit func(map[string]any)) {
	for _, raw := range spans {
		s := raw.(map[string]any)
		visit(s)
		children, _ := s["children"].([]any)
		eachSpan(children, visit)
	}
}

// rootsOf are the roots of a tree answer.
func rootsOf(answer map[string]any) []any {
	roots, _ := answer["trace"].(map[string]any)["roots"].([]any)
	return roots
}

// stepSpan is a span of r's trace under parent, with an input and an output of the given text.
func stepSpan(r tracedByHand, parent trace.SpanID, name, kind, text string, at time.Time) observability.RunnerSpan {
	var id trace.SpanID
	_, _ = rand.Read(id[:])
	return observability.RunnerSpan{TraceID: r.traceID, SpanID: id, ParentID: parent, Name: name, Start: at, End: at.Add(time.Millisecond),
		Attributes: map[string]string{"openinference.span.kind": kind, "rotor.run": r.id.String(), "metadata": "{}",
			"input.value": text, "output.value": text}}
}

// ownTrace stores a sender's own trace of two spans behind the policies' back, as the OTLP endpoint keeps one.
func (h *harness) ownTrace(t *testing.T, sender uuid.UUID) string {
	t.Helper()
	ctx := context.Background()
	traceID := randomTraceID()
	var row uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO trace (trace_id, owner_id) VALUES ($1, $2) RETURNING id`, traceID, sender).Scan(&row); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM trace WHERE id = $1`, row) })
	if _, err := h.super.Exec(ctx, `INSERT INTO trace_span (trace, span_id, parent_span_id, name, kind, service, source, sender_id,
			sender_name, started_at, ended_at, status, status_message) VALUES
		($1, '1111111111111111', '', 'chat', 'CHAIN', 'my-app', 'otlp', $2, 'The sender', now(), now() + interval '2 seconds', 'ok', ''),
		($1, '2222222222222222', '1111111111111111', 'ChatOpenAI', 'LLM', 'my-app', 'otlp', $2, 'The sender',
			now() + interval '1 second', now() + interval '2 seconds', 'error', 'rate limited')`, row, sender); err != nil {
		t.Fatal(err)
	}
	return traceID
}
