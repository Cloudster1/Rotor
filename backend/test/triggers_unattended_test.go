//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// unattended writes before its approval through calls its write binding names unattended (#128).
func unattended(tool string, calls ...string) string {
	list := "[" + calls[0]
	for _, c := range calls[1:] {
		list += ", " + c
	}
	list += "]"
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("unattended") + `}
tools:
  read: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
  write: {tool: ` + tool + `, scopes: [mcp], access: write, allowTools: ` + list + `, unattended: ` + list + `}
limits: {maxSteps: 10, timeout: 5m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: read, call: whoami}
  - {id: file, type: tool, tool: write, call: create_requirement, arguments: {title: x}}
  - {id: review, type: approval, message: 'Go on?'}
edges:
  - {from: ask, to: file}
  - {from: file, to: review}
`
}

func withUnattended(body map[string]any, calls ...string) map[string]any {
	body["allowUnattended"] = calls
	return body
}

func TestANamedWriteNeedsTheOwnersAgreementAndAWiderVersionTurnsTheTriggerOff(t *testing.T) {
	h := newHarness(t)
	product := "unattended-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	workflowID := createWorkflow(t, bob, unattended(product, "create_requirement"))

	_, wf := bob.api(http.MethodGet, "/workflows/"+workflowID, nil)
	if calls, _ := wf["version"].(map[string]any)["unattendedCalls"].([]any); !slices.Equal(calls, []any{"write.create_requirement"}) {
		t.Errorf("the workflow names unattended calls %v", wf["version"])
	}
	refused := addTrigger(t, bob, workflowID, schedule("unagreed", yearly, false), http.StatusUnprocessableEntity)
	if errorCode(refused) != "unattended_not_allowed" || refused["error"].(map[string]any)["fields"].(map[string]any)["allowUnattended"] == nil {
		t.Errorf("a trigger whose owner did not agree = %v", refused)
	}
	trigger := addTrigger(t, bob, workflowID, withUnattended(schedule("agreed", yearly, false), "write.create_requirement"), http.StatusCreated)
	if got, _ := trigger["allowUnattended"].([]any); !slices.Equal(got, []any{"write.create_requirement"}) {
		t.Errorf("the trigger reads allowUnattended %v", trigger["allowUnattended"])
	}

	// A version naming one more unattended call turns the trigger off when it fires, until the owner agrees again.
	id := uuid.MustParse(trigger["id"].(string))
	if status, answer := bob.api(http.MethodPut, "/workflows/"+workflowID,
		map[string]any{"specYAML": unattended(product, "create_requirement", "update_requirement"), "baseVersion": 1}); status != http.StatusOK {
		t.Fatalf("saving version 2 = %d %v", status, answer)
	}
	fireAt(t, h, id, nextRun(t, trigger).Add(time.Second))
	off := lastFire(t, bob, id.String(), "refused", "started", "failed")
	if disabled, _ := off["disabled"].(map[string]any); disabled["reason"] != "unattended_widened" {
		t.Fatalf("the firing of the wider version reads %v", off)
	}
	if status, answer := bob.api(http.MethodPost, "/triggers/"+id.String()+"/enable", nil); status != http.StatusUnprocessableEntity ||
		errorCode(answer) != "unattended_not_allowed" {
		t.Errorf("turning it on without agreeing again = %d %v", status, answer)
	}
	body := withUnattended(schedule("agreed", yearly, false), "write.create_requirement", "write.update_requirement")
	if status, answer := bob.api(http.MethodPut, "/triggers/"+id.String(), body); status != http.StatusOK {
		t.Fatalf("agreeing to the wider version = %d %v", status, answer)
	}
	if status, answer := bob.api(http.MethodPost, "/triggers/"+id.String()+"/enable", nil); status != http.StatusOK || answer["enabled"] != true {
		t.Errorf("turning it on once agreed = %d %v", status, answer)
	}
}

func TestAdministratorsForbidUnattendedWritesForEveryone(t *testing.T) {
	h := newHarness(t)
	product := "forbid-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	alice := signedIn(t, "alice")
	t.Cleanup(func() { alice.api(http.MethodPut, "/settings/unattended-writes", map[string]any{"allowed": true}) })
	workflowID := createWorkflow(t, bob, unattended(product, "create_requirement"))
	trigger := addTrigger(t, bob, workflowID, withUnattended(schedule("agreed", yearly, false), "write.create_requirement"), http.StatusCreated)
	plain := addTrigger(t, bob, createWorkflow(t, bob, asker(product, true)), schedule("plain", yearly, false), http.StatusCreated)

	if status, _ := bob.api(http.MethodPut, "/settings/unattended-writes", map[string]any{"allowed": false}); status != http.StatusForbidden {
		t.Errorf("a member forbids unattended writes with %d", status)
	}
	status, setting := alice.api(http.MethodPut, "/settings/unattended-writes", map[string]any{"allowed": false})
	if status != http.StatusOK || setting["allowed"] != false {
		t.Fatalf("forbidding = %d %v", status, setting)
	}
	_, off := bob.api(http.MethodGet, "/triggers/"+trigger["id"].(string), nil)
	if disabled, _ := off["disabled"].(map[string]any); off["enabled"] != false || disabled["reason"] != "unattended_forbidden" {
		t.Errorf("after forbidding, the agreed trigger reads %v", off)
	}
	if _, other := bob.api(http.MethodGet, "/triggers/"+plain["id"].(string), nil); other["enabled"] != true {
		t.Errorf("a trigger without unattended calls was turned off too: %v", other)
	}
	if answer := addTrigger(t, bob, workflowID, withUnattended(schedule("again", yearly, false), "write.create_requirement"),
		http.StatusUnprocessableEntity); errorCode(answer) != "unattended_forbidden" {
		t.Errorf("a new trigger while forbidden = %v", answer)
	}
	var switched int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = 'unattended.switched'
		AND actor_user_id IS NOT NULL AND (data ->> 'allowed')::boolean = false`).Scan(&switched); err != nil || switched == 0 {
		t.Errorf("the audit log records the switch %d times (%v)", switched, err)
	}

	// The database holds the switch too: a member cannot change it straight through SQL.
	member := h.addPerson(t, "member")
	_ = h.cluster.Write(db.WithUser(context.Background(), member), func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE unattended_setting SET allowed = true`)
		if err == nil && tag.RowsAffected() > 0 {
			t.Error("a member changed the setting through SQL")
		}
		return errors.New("roll back")
	})
}
