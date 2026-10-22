package hooks

import (
	"encoding/json"
	"testing"
	"time"
)

// Each event names what it says was written as a run's ledger names it, from the tools' real payloads (#127).
func TestAnEventNamesWhatWasWrittenAsTheLedgerDoes(t *testing.T) {
	at := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		tool, topic, payload, want string
	}{
		{"armature", "comment.added", `{"issueId":"i","key":"CP-12","commentId":"c-1","actorId":"a","internal":false,"mentions":null}`, "comment:c-1"},
		{"armature", "issue.created", `{"issueId":"i","key":"CP-12","summary":"s","actorId":"a"}`, "issue:CP-12"},
		{"armature", "issue.updated", `{"issueId":"i","key":"CP-12","actorId":"a","changes":[]}`, "issue:CP-12"},
		{"armature", "issue.transitioned", `{"issue":{"key":"CP-12"}}`, "issue:CP-12"},
		{"exciter", "requirement.created", `{"requirement":{"key":"RTR-1","revision":3,"project":{"key":"RTR"}}}`, "requirement:RTR-1@3"},
		{"exciter", "requirement.revised", `{"requirement":{"key":"RTR-1","revision":4}}`, "requirement:RTR-1@4"},
		{"exciter", "comment.created", `{"comment":{"id":"e-9","threadId":"t"},"requirement":{"key":"RTR-1"}}`, "comment:e-9"},
		{"exciter", "requirement.created", `{"requirement":{"key":"RTR-1"}}`, ""},
		{"exciter", "review.decided", `{"requirement":{"key":"RTR-1","revision":3}}`, ""},
		{"stator", "page.published", `{"page":{"id":"p"}}`, ""},
		{"armature", "comment.added", `{"key":"CP-12"}`, ""},
	} {
		event := map[string]any{"tool": c.tool, "topic": c.topic, "id": "x", "occurredAt": at, "payload": json.RawMessage(c.payload)}
		subject, occurred, ok := Written(map[string]any{"event": event})
		if ok != (c.want != "") || subject != c.want {
			t.Errorf("%s %s names %q (%v), want %q", c.tool, c.topic, subject, ok, c.want)
		}
		if ok && !occurred.Equal(at) {
			t.Errorf("%s %s occurred at %v, want %v", c.tool, c.topic, occurred, at)
		}
	}
	if _, _, ok := Written(map[string]any{}); ok {
		t.Error("firing inputs without an event name something")
	}
}
