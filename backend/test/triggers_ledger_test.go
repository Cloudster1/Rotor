//go:build integration

package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// envelopeAt is a version 1 delivery of a topic that says it occurred at a given time.
func envelopeAt(id, topic string, at time.Time, payload string) []byte {
	return []byte(fmt.Sprintf(`{"version":1,"id":%q,"source":"exciter","topic":%q,"occurredAt":%q,"payload":%s}`,
		id, topic, at.UTC().Format(time.RFC3339), payload))
}

// firing waits until the firing of one event ended and answers how, why, and the run it started.
func firing(t *testing.T, h *harness, triggerID, eventID string) (outcome, reason string, run *uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(sandboxWait)
	for time.Now().Before(deadline) {
		err := h.super.QueryRow(context.Background(), `SELECT COALESCE(outcome, ''), reason, run_id FROM trigger_fire
			WHERE trigger_id = $1 AND fire_key = $2`, triggerID, "event:"+eventID).Scan(&outcome, &reason, &run)
		if err == nil && outcome != "" {
			return outcome, reason, run
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("the firing of event %s did not end", eventID)
	return "", "", nil
}

// An event about something a run of the same workflow wrote does not start it again; a person's event, or one long
// after, does (#127).
func TestAWorkflowDoesNotStartItselfOnItsOwnWrites(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	stub := newToolStub(t, map[string]any{"product": "exciter", "requirements": map[string]any{"RTR-1": []string{"bob"}}})
	bob, bobID, toolID := connected(t, stub)
	alice := signedIn(t, "alice")
	secret := "exciter_whs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if status, hook := alice.api(http.MethodPut, "/tools/"+toolID.String()+"/hook", map[string]any{"secret": secret}); status != http.StatusOK {
		t.Fatalf("setting the secret = %d %v", status, hook)
	}
	workflowID := createWorkflow(t, bob, echoEvent())
	trigger := addTrigger(t, bob, workflowID, map[string]any{"name": "On requirements", "kind": "event",
		"event": map[string]any{"toolId": toolID.String(), "topics": []string{"requirement.created"}, "projects": []string{}}}, http.StatusCreated)
	triggerID := trigger["id"].(string)
	revision := func(n int, at time.Time) string {
		id := uuid.NewString()
		body := envelopeAt(id, "requirement.created", at, fmt.Sprintf(`{"requirement":{"key":"RTR-1","revision":%d,"project":{"key":"RTR"}}}`, n))
		if status, answer := deliver(t, toolID.String(), "Exciter", secret, body, time.Now(), "1", nil); status != http.StatusAccepted {
			t.Fatalf("delivering revision %d = %d %v", n, status, answer)
		}
		return id
	}

	// A person's revision starts a run; that run then writes revision 2, as the ledger records it.
	outcome, _, first := firing(t, h, triggerID, revision(1, time.Now()))
	if outcome != "started" || first == nil {
		t.Fatalf("a person's revision ended %q", outcome)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO run_write (run_id, tool_id, call, subject) VALUES ($1, $2, 'update_requirement', 'requirement:RTR-1@2')`,
		*first, toolID); err != nil {
		t.Fatal(err)
	}
	outcome, reason, run := firing(t, h, triggerID, revision(2, time.Now()))
	if outcome != "skipped" || run != nil || !strings.Contains(reason, "own write") || !strings.Contains(reason, first.String()) {
		t.Errorf("the workflow's own revision ended %q (%q), run %v", outcome, reason, run)
	}
	_, shown := bob.api(http.MethodGet, "/triggers/"+triggerID, nil)
	if fire, _ := shown["lastFire"].(map[string]any); fire["outcome"] != "skipped" || !strings.Contains(fmt.Sprint(fire["reason"]), "own write") {
		t.Errorf("the owner reads the last firing as %v", shown["lastFire"])
	}

	// The same subject long after the write is somebody else's change, and starts a run.
	if outcome, _, _ := firing(t, h, triggerID, revision(2, time.Now().Add(time.Hour))); outcome != "started" {
		t.Errorf("an event an hour after the write ended %q", outcome)
	}

	// The ledger is the run's: its person reads it, nobody else does, and nobody writes it through SQL.
	carol := h.addPerson(t, "member")
	count := func(person uuid.UUID) int {
		var n int
		if err := h.cluster.Read(db.WithUser(ctx, person), func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM run_write WHERE run_id = $1`, *first).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if mine, theirs := count(bobID), count(carol); mine != 1 || theirs != 0 {
		t.Errorf("the ledger reads %d rows to its person and %d to another, want 1 and 0", mine, theirs)
	}
	err := h.cluster.Write(db.WithUser(ctx, bobID), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO run_write (run_id, tool_id, call, subject) VALUES ($1, $2, 'x', 'requirement:RTR-9@1')`, *first, toolID)
		if err == nil {
			return errors.New("roll back")
		}
		return err
	})
	permissionDenied(t, "writing the ledger", err)
}
