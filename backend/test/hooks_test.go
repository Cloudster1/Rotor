//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/hooks"
)

// deliver posts a body to Rotor's hook for a tool as the tool would, signed with secret at the time given;
// change may alter the headers after signing.
func deliver(t *testing.T, toolID, word, secret string, body []byte, at time.Time, version string, change func(http.Header)) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, apiURL(t)+"/api/v1/hooks/"+toolID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-"+word+"-Timestamp", strconv.FormatInt(at.Unix(), 10))
	req.Header.Set("X-"+word+"-Signature-Timestamped-256", hooks.SignTimestamped(body, secret, at))
	req.Header.Set("X-"+word+"-Signature-256", hooks.Sign(body, secret))
	req.Header.Set("X-"+word+"-Delivery", uuid.NewString())
	req.Header.Set("X-"+word+"-Webhook-Version", version)
	if change != nil {
		change(req.Header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// envelope is a version 1 delivery of a topic with a payload.
func envelope(id, topic, payload string) []byte {
	return []byte(fmt.Sprintf(`{"version":1,"id":%q,"source":"exciter","topic":%q,"occurredAt":"2026-10-19T08:00:00Z","payload":%s}`, id, topic, payload))
}

// eventTrigger adds an enabled event trigger behind the policies' back, for the hook to match.
func (h *harness) eventTrigger(t *testing.T, toolID string, topics, projects []string, conditions string, enabled bool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if projects == nil {
		projects = []string{}
	}
	owner := h.addPerson(t, "member")
	var workflowID, id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO workflow (name, owner_id) VALUES ('hooked', $1) RETURNING id`, owner).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, workflowID) })
	if err := h.super.QueryRow(ctx, `INSERT INTO trigger (workflow_id, owner_id, name, kind, tool_id, topics, projects, conditions, enabled)
		VALUES ($1, $2, 'on event', 'event', $3, $4, $5, $6, $7) RETURNING id`,
		workflowID, owner, toolID, topics, projects, conditions, enabled).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// fires lists the event topics of the firings queued for a trigger.
func (h *harness) fires(t *testing.T, triggerID uuid.UUID) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `SELECT inputs->'event'->>'topic' FROM trigger_fire WHERE trigger_id = $1 ORDER BY created_at`, triggerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var topic string
		if err := rows.Scan(&topic); err != nil {
			t.Fatal(err)
		}
		out = append(out, topic)
	}
	return out
}

func TestAToolsSignedEventsQueueTheTriggersTheyMatch(t *testing.T) {
	h := newHarness(t)
	// The test reads what deliveries queue; the worker would fire, and turn off, these bare triggers meanwhile.
	stopWorker(t)
	ctx := context.Background()
	toolID := addTool(t, newToolStub(t, map[string]any{"product": "exciter"}))
	alice, bob := signedIn(t, "alice"), signedIn(t, "bob")
	now := time.Now()
	draft := `{"requirement":{"key":"RTR-1","status":"draft","project":{"key":"RTR"}}}`

	if status, _ := bob.api(http.MethodGet, "/tools/"+toolID+"/hook", nil); status != http.StatusForbidden {
		t.Errorf("a member reads the hook with %d", status)
	}
	status, hook := alice.api(http.MethodGet, "/tools/"+toolID+"/hook", nil)
	if status != http.StatusOK || hook["secretSet"] != false || hook["product"] != "Exciter" || !strings.HasSuffix(hook["url"].(string), "/api/v1/hooks/"+toolID) {
		t.Fatalf("the hook reads %d %v", status, hook)
	}
	if status, _ := deliver(t, toolID, "Exciter", "exciter_whs_none", envelope("e0", "requirement.created", draft), now, "1", nil); status != http.StatusNotFound {
		t.Errorf("a delivery before a secret = %d, want 404", status)
	}
	if status, _ := alice.api(http.MethodPut, "/tools/"+toolID+"/hook", map[string]any{"secret": "armature_whs_abc"}); status != http.StatusUnprocessableEntity {
		t.Errorf("another tool's secret = %d, want 422", status)
	}
	secret := "exciter_whs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if status, hook := alice.api(http.MethodPut, "/tools/"+toolID+"/hook", map[string]any{"secret": secret}); status != http.StatusOK || hook["secretSet"] != true {
		t.Fatalf("setting the secret = %d %v", status, hook)
	}

	matching := h.eventTrigger(t, toolID, []string{"requirement.created"}, []string{"RTR"},
		`[{"path":"payload.requirement.status","op":"equals","value":"draft"}]`, true)
	otherTopic := h.eventTrigger(t, toolID, []string{"review.decided"}, nil, `[]`, true)
	otherProject := h.eventTrigger(t, toolID, []string{"requirement.created"}, []string{"XYZ"}, `[]`, true)
	off := h.eventTrigger(t, toolID, []string{"requirement.created"}, nil, `[]`, false)

	id := uuid.NewString()
	if status, answer := deliver(t, toolID, "Exciter", secret, envelope(id, "requirement.created", draft), now, "1", nil); status != http.StatusAccepted {
		t.Fatalf("a signed delivery = %d %v", status, answer)
	}
	if got := h.fires(t, matching); len(got) != 1 || got[0] != "requirement.created" {
		t.Errorf("the matching trigger queued %v", got)
	}
	for name, other := range map[string]uuid.UUID{"another topic": otherTopic, "another project": otherProject, "an off trigger": off} {
		if got := h.fires(t, other); len(got) != 0 {
			t.Errorf("%s queued %v", name, got)
		}
	}
	var payload string
	if err := h.super.QueryRow(ctx, `SELECT inputs->'event'->'payload'->'requirement'->>'key' FROM trigger_fire WHERE trigger_id = $1`, matching).Scan(&payload); err != nil || payload != "RTR-1" {
		t.Errorf("the firing's event payload holds %q (%v)", payload, err)
	}

	// The tool retries, signed afresh: the event is the same, so nothing more is queued.
	if status, _ := deliver(t, toolID, "Exciter", secret, envelope(id, "requirement.created", draft), now.Add(time.Minute), "1", nil); status != http.StatusOK {
		t.Errorf("a repeated event = %d, want 200", status)
	}
	ce := []byte(`{"specversion":"1.0","id":"` + uuid.NewString() + `","source":"https://exciter","type":"dev.exciter.requirement.created",` +
		`"time":"2026-10-19T08:00:00Z","datacontenttype":"application/json","dataversion":1,"data":` + draft + `}`)
	if status, answer := deliver(t, toolID, "Exciter", secret, ce, now, "2", func(h http.Header) { h.Set("Content-Type", "application/cloudevents+json") }); status != http.StatusAccepted {
		t.Errorf("a CloudEvent = %d %v", status, answer)
	}
	if got := h.fires(t, matching); len(got) != 2 {
		t.Errorf("after a repeat and a CloudEvent the trigger has %d firings, want 2", len(got))
	}

	body := envelope(uuid.NewString(), "requirement.created", draft)
	for _, c := range []struct {
		name   string
		body   []byte
		secret string
		at     time.Time
		ver    string
		change func(http.Header)
		want   int
	}{
		{"a replay older than the window", body, secret, now.Add(-10 * time.Minute), "1", nil, http.StatusUnauthorized},
		{"a wrong secret", body, "exciter_whs_wrong", now, "1", nil, http.StatusUnauthorized},
		{"no timestamped signature", body, secret, now, "1", func(h http.Header) { h.Del("X-Exciter-Signature-Timestamped-256") }, http.StatusUnauthorized},
		{"another product's headers", body, secret, now, "1", func(h http.Header) {
			h.Set("X-Stator-Timestamp", h.Get("X-Exciter-Timestamp"))
			h.Set("X-Stator-Signature-Timestamped-256", h.Get("X-Exciter-Signature-Timestamped-256"))
			h.Del("X-Exciter-Signature-Timestamped-256")
		}, http.StatusUnauthorized},
		{"version 3", []byte(`{"version":3,"id":"x","topic":"requirement.created"}`), secret, now, "3", nil, http.StatusUnprocessableEntity},
		{"a ping", envelope(uuid.NewString(), "ping", `{}`), secret, now, "1", nil, http.StatusOK},
		{"a body too large", []byte(`{"pad":"` + strings.Repeat("x", 300<<10) + `"}`), secret, now, "1", nil, http.StatusRequestEntityTooLarge},
	} {
		if status, answer := deliver(t, toolID, "Exciter", c.secret, c.body, c.at, c.ver, c.change); status != c.want {
			t.Errorf("%s = %d %v, want %d", c.name, status, answer, c.want)
		}
	}
	if got := h.fires(t, matching); len(got) != 2 {
		t.Errorf("refused deliveries queued firings: %d", len(got))
	}

	// The secret is in OpenBao alone: not in the tool's row, not in the audit log.
	var leaked int
	if err := h.super.QueryRow(ctx, `SELECT (SELECT count(*) FROM tool t WHERE row_to_json(t)::text LIKE '%' || $1 || '%')
		+ (SELECT count(*) FROM audit_log a WHERE row_to_json(a)::text LIKE '%' || $1 || '%')
		+ (SELECT count(*) FROM hook_event e WHERE row_to_json(e)::text LIKE '%' || $1 || '%')`, secret).Scan(&leaked); err != nil || leaked != 0 {
		t.Errorf("the secret is in %d rows (%v)", leaked, err)
	}
	if !h.audited(t, "hook.secret_changed", toolID) {
		t.Error("setting the secret is not in the audit log")
	}
	if status, hook := alice.api(http.MethodGet, "/tools/"+toolID+"/hook", nil); status != http.StatusOK || hook["lastReceivedAt"] == nil {
		t.Errorf("the hook after deliveries reads %d %v", status, hook)
	}

	if status, hook := alice.api(http.MethodDelete, "/tools/"+toolID+"/hook", nil); status != http.StatusOK || hook["secretSet"] != false {
		t.Errorf("removing the secret = %d %v", status, hook)
	}
	if status, _ := deliver(t, toolID, "Exciter", secret, envelope(uuid.NewString(), "requirement.created", draft), now, "1", nil); status != http.StatusNotFound {
		t.Errorf("a delivery after the secret is gone = %d, want 404", status)
	}
}

func TestArmatureAndStatorSignWithTheirOwnHeaders(t *testing.T) {
	h := newHarness(t)
	stopWorker(t)
	alice := signedIn(t, "alice")
	now := time.Now()
	for _, c := range []struct{ product, word, topic, payload, project string }{
		{"armature", "Armature", "issue.created", armatureIssueCreated, "CP"},
		{"stator", "Stator", "page.published", `{"page":{"space":{"key":"DOC"}}}`, "DOC"},
	} {
		toolID := addTool(t, newToolStub(t, map[string]any{"product": c.product}))
		secret := c.product + "_whs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if status, hook := alice.api(http.MethodPut, "/tools/"+toolID+"/hook", map[string]any{"secret": secret}); status != http.StatusOK || hook["product"] != c.word {
			t.Fatalf("setting %s's secret = %d %v", c.product, status, hook)
		}
		trigger := h.eventTrigger(t, toolID, []string{c.topic}, []string{c.project}, `[]`, true)
		body := []byte(fmt.Sprintf(`{"version":1,"id":%q,"topic":%q,"orgId":"o","occurredAt":"2026-10-19T08:00:00Z","payload":%s}`, uuid.NewString(), c.topic, c.payload))
		if status, answer := deliver(t, toolID, c.word, secret, body, now, "1", nil); status != http.StatusAccepted {
			t.Errorf("%s's delivery = %d %v", c.product, status, answer)
		}
		if got := h.fires(t, trigger); len(got) != 1 {
			t.Errorf("%s's trigger queued %v", c.product, got)
		}
		if status, _ := deliver(t, toolID, "Exciter", secret, body, now, "1", nil); status != http.StatusUnauthorized {
			t.Errorf("%s signed with Exciter's headers = %d, want 401", c.product, status)
		}
		ce := []byte(`{"specversion":"1.0","id":"` + uuid.NewString() + `","source":"x","type":"dev.` + c.product + `.` + c.topic + `","time":"2026-10-19T08:00:00Z","data":{}}`)
		if status, _ := deliver(t, toolID, c.word, secret, ce, now, "2", nil); status != http.StatusUnprocessableEntity {
			t.Errorf("a CloudEvent from %s = %d, want 422 until it sends them", c.product, status)
		}
	}
}

// Payloads as Armature's docs/webhooks.md describes them: the issue's key sits at the top of the payload.
const (
	armatureIssueCreated = `{"issueId":"0b6e2f7a-3c41-4d8e-9f52-1a7c9e4b2d60","key":"CP-12","summary":"Login fails",` +
		`"actorId":"5d3a8c19-7e2b-4f60-a1d4-8b9c0e6f2a37"}`
	armatureCommentAdded = `{"issueId":"0b6e2f7a-3c41-4d8e-9f52-1a7c9e4b2d60","key":"CP-12","commentId":"c2f1e0d9-8b7a-4c6d-9e5f-4a3b2c1d0e9f",` +
		`"actorId":"5d3a8c19-7e2b-4f60-a1d4-8b9c0e6f2a37","internal":false,"mentions":null}`
)

// A comment on an Armature issue fires the triggers on that issue's project, and only those (#125).
func TestAnArmatureCommentFiresTheTriggersOnItsIssuesProject(t *testing.T) {
	h := newHarness(t)
	stopWorker(t)
	alice := signedIn(t, "alice")
	toolID := addTool(t, newToolStub(t, map[string]any{"product": "armature"}))
	secret := "armature_whs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if status, hook := alice.api(http.MethodPut, "/tools/"+toolID+"/hook", map[string]any{"secret": secret}); status != http.StatusOK {
		t.Fatalf("setting Armature's secret = %d %v", status, hook)
	}
	topics := []string{"comment.added"}
	matching := h.eventTrigger(t, toolID, topics, []string{"CP"}, `[]`, true)
	public := h.eventTrigger(t, toolID, topics, []string{"CP"}, `[{"path":"payload.internal","op":"equals","value":false}]`, true)
	elsewhere := h.eventTrigger(t, toolID, topics, []string{"OPS"}, `[]`, true)
	anyProject := h.eventTrigger(t, toolID, topics, nil, `[]`, true)

	id := uuid.NewString()
	body := []byte(fmt.Sprintf(`{"version":1,"id":%q,"topic":"comment.added","orgId":"0199a000-0000-7000-8000-000000000001",`+
		`"occurredAt":"2026-10-19T08:00:00Z","payload":%s}`, id, armatureCommentAdded))
	if status, answer := deliver(t, toolID, "Armature", secret, body, time.Now(), "1", nil); status != http.StatusAccepted {
		t.Fatalf("the comment's delivery = %d %v", status, answer)
	}
	for name, c := range map[string]struct {
		trigger uuid.UUID
		fires   int
	}{"its project": {matching, 1}, "a condition on it": {public, 1}, "another project": {elsewhere, 0}, "every project": {anyProject, 1}} {
		if got := h.fires(t, c.trigger); len(got) != c.fires {
			t.Errorf("a trigger on %s queued %v, want %d", name, got, c.fires)
		}
	}
	var key string
	if err := h.super.QueryRow(context.Background(), `SELECT inputs->'event'->'payload'->>'key' FROM trigger_fire
		WHERE trigger_id = $1 AND fire_key = $2`, matching, "event:"+id).Scan(&key); err != nil || key != "CP-12" {
		t.Errorf("the firing carries the issue %q (%v), want CP-12", key, err)
	}
}
