//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A key sends spans as the account it was made for: they join a run that account reads, and its own trace otherwise,
// stamped with the key; another account's key naming the run lands in that account's own trace.
func TestAnIngestKeySendsSpansAsItsAccount(t *testing.T) {
	h := newHarness(t)
	admin := h.browserFor(t, h.addPerson(t, "admin"))
	starter, stranger := h.addPerson(t, "member"), h.addPerson(t, "member")
	suffix := uuid.NewString()[:8]
	key := makeKey(t, h, admin, map[string]any{"name": "Billing " + suffix, "accountEmail": h.emailOf(t, starter)})
	other := makeKey(t, h, admin, map[string]any{"name": "Elsewhere " + suffix, "accountEmail": h.emailOf(t, stranger)})
	if account, _ := key.key["account"].(map[string]any); account["id"] != starter.String() {
		t.Fatalf("the key sends as %v, want the starter", key.key["account"])
	}
	r := h.runWithTrace(t, starter)
	traceID := r.traceID.String()

	tool := appSpan(traceID, "lookup", map[string]string{"openinference.span.kind": "TOOL", "rotor.ingest_key": "forged"})
	status, _, joined := sendSpans(t, otlpCall{token: key.secret, contentType: mediaJSON, body: jsonOf(t, tool)})
	if status != http.StatusOK {
		t.Fatalf("the key's spans = %d %q", status, joined)
	}
	kept, ok := h.keptSpanOf(t, traceID, tool.spanID)
	if !ok || kept.runID == nil || *kept.runID != r.id || kept.senderID == nil || *kept.senderID != starter ||
		kept.attributes["rotor.sender"] != starter.String() || kept.attributes["rotor.ingest_key"] != key.id {
		t.Fatalf("the key's span was kept as %+v, %v; want the run's, sent as the starter with the key", kept, ok)
	}
	if traces, spans := h.spansReadBy(t, starter, r.id); traces != 1 || spans != 1 {
		t.Errorf("the starter reads %d traces and %d spans of the run, want the key's one span", traces, spans)
	}

	own := appSpan(randomTraceID(), "LangGraph", nil)
	sendSpans(t, otlpCall{token: key.secret, contentType: mediaProtobuf, body: protobufOf(t, own)})
	if mine, ok := h.keptSpanOf(t, own.traceID, own.spanID); !ok || mine.runID != nil || mine.ownerID == nil || *mine.ownerID != starter {
		t.Errorf("a span of no run was kept as %+v, %v; want the starter's own trace", mine, ok)
	}

	forged := appSpan(traceID, "lookup", nil)
	_, _, refused := sendSpans(t, otlpCall{token: other.secret, contentType: mediaJSON, body: jsonOf(t, forged)})
	if string(refused) != string(joined) {
		t.Errorf("another account's key naming the run is answered %q, the starter's %q; want the same", refused, joined)
	}
	if theirs, ok := h.keptSpanOf(t, traceID, forged.spanID); !ok || theirs.runID != nil || theirs.ownerID == nil ||
		*theirs.ownerID != stranger || theirs.attributes["rotor.ingest_key"] != other.id {
		t.Errorf("the other key's span was kept as %+v, %v; want the stranger's own trace", theirs, ok)
	}
	if traces, spans := h.spansReadBy(t, starter, r.id); spans != 1 {
		t.Errorf("the run holds %d spans in %d traces after another account's key named it, want the one", spans, traces)
	}

	// Its use is noted, at most once a minute, and the list says when without the key.
	var used *time.Time
	if err := h.super.QueryRow(context.Background(), `SELECT last_used_at FROM ingest_key WHERE id = $1`, key.id).Scan(&used); err != nil ||
		used == nil || time.Since(*used) > time.Minute {
		t.Errorf("the key's last use reads %v, %v", used, err)
	}
	_, list := admin.api(http.MethodGet, "/settings/ingest-keys", nil)
	for _, each := range list["keys"].([]any) {
		listed := each.(map[string]any)
		if listed["id"] == key.id && listed["lastUsedAt"] == nil {
			t.Errorf("the list says the key was never used: %v", listed)
		}
	}
}

// A key is refused once revoked, rotated away or expired, and while its account is switched off; a rotated key's new
// secret sends, and an expired key is not rotated.
func TestARevokedRotatedOrExpiredIngestKeyIsRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.browserFor(t, h.addPerson(t, "admin"))
	sender := h.addPerson(t, "member")
	suffix := uuid.NewString()[:8]
	key := makeKey(t, h, admin, map[string]any{"name": "Rotating " + suffix, "accountEmail": h.emailOf(t, sender)})
	if status, body := sendWith(t, key.secret); status != http.StatusOK {
		t.Fatalf("a fresh key = %d %q", status, body)
	}

	status, rotated := admin.api(http.MethodPost, "/settings/ingest-keys/"+key.id+"/rotate", nil)
	fresh, _ := rotated["secret"].(string)
	if status != http.StatusOK || fresh == "" || fresh == key.secret {
		t.Fatalf("rotating = %d %v", status, rotated)
	}
	if status, body := sendWith(t, key.secret); status != http.StatusUnauthorized || !strings.Contains(string(body), "revoked or rotated") {
		t.Errorf("the rotated-away secret = %d %q, want 401", status, body)
	}
	if status, body := sendWith(t, fresh); status != http.StatusOK {
		t.Errorf("the new secret = %d %q", status, body)
	}

	if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, sender); err != nil {
		t.Fatal(err)
	}
	if status, body := sendWith(t, fresh); status != http.StatusForbidden || !strings.Contains(string(body), "account_switched_off") {
		t.Errorf("a key of an account switched off = %d %q, want 403", status, body)
	}
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = true WHERE id = $1`, sender); err != nil {
		t.Fatal(err)
	}

	if status, _ := admin.api(http.MethodDelete, "/settings/ingest-keys/"+key.id, nil); status != http.StatusNoContent {
		t.Fatalf("revoking = %d", status)
	}
	if status, body := sendWith(t, fresh); status != http.StatusUnauthorized || !strings.Contains(string(body), "invalid_token") {
		t.Errorf("a revoked key = %d %q, want 401", status, body)
	}
	if status, _ := admin.api(http.MethodDelete, "/settings/ingest-keys/"+key.id, nil); status != http.StatusNotFound {
		t.Errorf("revoking twice = %d, want 404", status)
	}

	expiring := makeKey(t, h, admin, map[string]any{"name": "Expiring " + suffix, "accountEmail": h.emailOf(t, sender),
		"expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if _, err := h.super.Exec(ctx, `UPDATE ingest_key SET created_at = now() - interval '2 hours',
		expires_at = now() - interval '1 hour' WHERE id = $1`, expiring.id); err != nil {
		t.Fatal(err)
	}
	if status, body := sendWith(t, expiring.secret); status != http.StatusUnauthorized || !strings.Contains(string(body), "has expired") {
		t.Errorf("an expired key = %d %q, want 401", status, body)
	}
	if status, answer := admin.api(http.MethodPost, "/settings/ingest-keys/"+expiring.id+"/rotate", nil); status != http.StatusConflict ||
		errorCode(answer) != "ingest_key_expired" {
		t.Errorf("rotating an expired key = %d %v", status, answer)
	}
}

// A key is taken at the OTLP endpoint alone: at MCP, at a person's route and at an administrator's it answers 401
// saying where it is taken, and does nothing there.
func TestAnIngestKeyIsRefusedAtEveryOtherRoute(t *testing.T) {
	h := newHarness(t)
	admin := h.browserFor(t, h.addPerson(t, "admin"))
	key := makeKey(t, h, admin, map[string]any{"name": "Narrow " + uuid.NewString()[:8]})
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`},
		{http.MethodGet, "/api/v1/auth/me", ""},
		{http.MethodGet, "/api/v1/settings/ingest-keys", ""},
		{http.MethodPost, "/api/v1/settings/ingest-keys", `{"name":"Made by a key"}`},
		{http.MethodGet, "/api/v1/runs", ""},
	} {
		status, body := callWith(t, c.method, c.path, key.secret, c.body)
		if status != http.StatusUnauthorized || !strings.Contains(body, "sends spans to /api/v1/otlp/v1/traces alone") ||
			strings.Contains(body, key.secret) {
			t.Errorf("%s %s with a key = %d %q, want 401", c.method, c.path, status, body)
		}
	}
	if status, body := sendWith(t, key.secret); status != http.StatusOK {
		t.Errorf("the same key at the endpoint = %d %q", status, body)
	}
}
