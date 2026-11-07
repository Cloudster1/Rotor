//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

// seeded is a span stored behind the policies' back, as the store keeps one, for a search to find.
type seeded struct {
	name, kind, status, service, message string
	attrs                                map[string]string
	at                                   time.Time
}

// runTraceRow stores r's trace row, empty, as the store makes it with the run's first span.
func (h *harness) runTraceRow(t *testing.T, r tracedByHand) uuid.UUID {
	t.Helper()
	var row uuid.UUID
	if err := h.super.QueryRow(context.Background(), `INSERT INTO trace (trace_id, run_id) VALUES ($1, $2) RETURNING id`,
		r.traceID.String(), r.id).Scan(&row); err != nil {
		t.Fatal(err)
	}
	return row
}

// ownTraceRow stores an empty trace of sender's own; it goes with the sender.
func (h *harness) ownTraceRow(t *testing.T, sender uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	traceID := randomTraceID()
	var row uuid.UUID
	if err := h.super.QueryRow(context.Background(), `INSERT INTO trace (trace_id, owner_id) VALUES ($1, $2) RETURNING id`,
		traceID, sender).Scan(&row); err != nil {
		t.Fatal(err)
	}
	return row, traceID
}

// seedSpans stores spans in the trace row and answers their ids; an owned trace's are the sender's, a run's the runner's.
func (h *harness) seedSpans(t *testing.T, row uuid.UUID, sender *uuid.UUID, spans ...seeded) []string {
	t.Helper()
	ids := make([]string, len(spans))
	source, senderName := "runner", (*string)(nil)
	if sender != nil {
		name := "The sender"
		source, senderName = "otlp", &name
	}
	for i, s := range spans {
		var id trace.SpanID
		_, _ = rand.Read(id[:])
		ids[i] = id.String()
		attrs, _ := json.Marshal(s.attrs)
		if s.attrs == nil {
			attrs = []byte("{}")
		}
		service, status, kind := s.service, s.status, s.kind
		if service == "" {
			service = "rotor-runner"
		}
		if status == "" {
			status = "ok"
		}
		if kind == "" {
			kind = "CHAIN"
		}
		if _, err := h.super.Exec(context.Background(), `INSERT INTO trace_span (trace, span_id, name, kind, service, source,
				sender_id, sender_name, started_at, ended_at, status, status_message, attributes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::timestamptz, $9::timestamptz + interval '250 milliseconds', $10, $11, $12)`,
			row, ids[i], s.name, kind, service, source, sender, senderName, s.at, status, s.message, string(attrs)); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

// searchAs is what b finds searching spans with q, failing unless the search answered.
func searchAs(t *testing.T, b *browser, q url.Values) map[string]any {
	t.Helper()
	status, body := b.api(http.MethodGet, "/spans?"+q.Encode(), nil)
	if status != http.StatusOK {
		t.Fatalf("GET /spans?%s: %d %v", q.Encode(), status, body)
	}
	return body
}

// hitsOf are the spans a search answered.
func hitsOf(answer map[string]any) []map[string]any {
	raw, _ := answer["spans"].([]any)
	out := make([]map[string]any, len(raw))
	for i, h := range raw {
		out[i] = h.(map[string]any)
	}
	return out
}

// spanIDsOf are the span ids of a search's hits, in its order.
func spanIDsOf(answer map[string]any) []string {
	var out []string
	for _, h := range hitsOf(answer) {
		out = append(out, h["spanId"].(string))
	}
	return out
}
