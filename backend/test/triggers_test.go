//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/triggers"
)

// yearly fires on the first of January, far enough off that the stack's own scheduler never finds it due; a test
// scheduler whose clock is set past it fires it at once.
const yearly = "0 0 1 1 *"

// asker asks the tool who the run acts as, then waits for an approval before it may write through a write binding.
func asker(tool string, approved bool) string {
	first, second := "review", "file"
	if !approved {
		first, second = "file", "review"
	}
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("asker") + `}
tools:
  read: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
  write: {tool: ` + tool + `, scopes: [mcp], access: write, allowTools: [create_requirement]}
limits: {maxSteps: 10, timeout: 5m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: read, call: whoami}
  - {id: review, type: approval, message: 'File it?'}
  - {id: file, type: tool, tool: write, call: create_requirement, arguments: {title: x}}
edges:
  - {from: ask, to: ` + first + `}
  - {from: ` + first + `, to: ` + second + `}
`
}

// fireAt runs a scheduler whose clock reads at until the trigger's firing is queued; the stack's worker holds the
// scheduler's lock for a moment each pass, so a pass may find nothing to do.
func fireAt(t *testing.T, h *harness, triggerID uuid.UUID, at time.Time) {
	t.Helper()
	s := triggers.NewScheduler(h.cluster, slog.New(slog.DiscardHandler))
	s.Now = func() time.Time { return at }
	deadline := time.Now().Add(sandboxWait)
	for time.Now().Before(deadline) {
		if err := s.Schedule(context.Background()); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM trigger_fire WHERE trigger_id = $1 AND due_at <= $2`,
			triggerID, at).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no firing of trigger %s was queued", triggerID)
}

// lastFire waits until the trigger's last firing ended as one of outcomes and answers it.
func lastFire(t *testing.T, b *browser, triggerID string, outcomes ...string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(sandboxWait)
	var trigger map[string]any
	for time.Now().Before(deadline) {
		_, trigger = b.api(http.MethodGet, "/triggers/"+triggerID, nil)
		if fire, _ := trigger["lastFire"].(map[string]any); fire != nil {
			for _, want := range outcomes {
				if fire["outcome"] == want {
					return trigger
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("trigger %s last fired %v, want one of %v", triggerID, trigger["lastFire"], outcomes)
	return nil
}

func addTrigger(t *testing.T, b *browser, workflowID string, body map[string]any, want int) map[string]any {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/workflows/"+workflowID+"/triggers", body)
	if status != want {
		t.Fatalf("adding a trigger with %v = %d %v, want %d", body, status, answer, want)
	}
	return answer
}

func schedule(name, cron string, noOverlap bool) map[string]any {
	return map[string]any{"name": name, "kind": "schedule", "schedule": map[string]any{"cron": cron, "timeZone": "UTC", "noOverlap": noOverlap}}
}

func nextRun(t *testing.T, trigger map[string]any) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, trigger["nextRunAt"].(string))
	if err != nil {
		t.Fatalf("the trigger's next run reads %v", trigger["nextRunAt"])
	}
	return at
}

func TestAScheduleFiresOnceAsItsOwnerAndOverlapsNothing(t *testing.T) {
	h := newHarness(t)
	product := "sched-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, bobID, _ := connected(t, stub)
	workflowID := createWorkflow(t, bob, asker(product, true))
	trigger := addTrigger(t, bob, workflowID, schedule("yearly", yearly, true), http.StatusCreated)
	id := uuid.MustParse(trigger["id"].(string))
	due := nextRun(t, trigger)
	if !due.After(time.Now()) || trigger["enabled"] != true {
		t.Fatalf("the new trigger reads %v", trigger)
	}

	fireAt(t, h, id, due.Add(time.Second))
	fired := lastFire(t, bob, id.String(), "started", "refused", "failed")
	fire := fired["lastFire"].(map[string]any)
	if fire["outcome"] != "started" {
		t.Fatalf("the firing ended %v", fire)
	}
	runID := fire["runId"].(string)
	run := waitRun(t, bob, runID, "waiting", "failed")
	if run["status"] != "waiting" || run["startedBy"].(map[string]any)["id"] != bobID.String() ||
		run["trigger"].(map[string]any)["id"] != id.String() {
		t.Fatalf("the triggered run reads %v", run)
	}
	if again := nextRun(t, fired); !again.After(due) {
		t.Errorf("the next run did not move on: %v after %v", again, due)
	}

	// The same time again starts nothing more; the next time, with the last run waiting, is skipped.
	fireAt(t, h, id, due.Add(time.Second))
	later := nextRun(t, fired)
	fireAt(t, h, id, later.Add(time.Second))
	skipped := lastFire(t, bob, id.String(), "skipped")
	if skipped["lastFire"].(map[string]any)["runId"] != runID {
		t.Errorf("a skipped firing changed the last run: %v", skipped["lastFire"])
	}
	var runs int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM run WHERE trigger_id = $1`, id).Scan(&runs); err != nil || runs != 1 {
		t.Errorf("the trigger started %d runs (%v), want 1", runs, err)
	}
	if !h.audited(t, "trigger.created", id.String()) {
		t.Error("the audit log does not record the trigger made")
	}
}

func TestATriggerIsCheckedAsItsOwnerAndTurnedOffWhenConsentEnds(t *testing.T) {
	h := newHarness(t)
	product := "consent-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, toolID := connected(t, stub)
	carol := signedIn(t, "carol")
	workflowID := createWorkflow(t, bob, asker(product, true))

	// Refusals say what is wrong, field by field; a workflow carol does not see is not there for her.
	addTrigger(t, carol, workflowID, schedule("theirs", yearly, false), http.StatusNotFound)
	for _, bad := range []map[string]any{schedule("x", "* * * * *", false), schedule("x", "TZ=UTC 0 8 * * 1", false),
		{"name": "x", "kind": "schedule", "schedule": map[string]any{"cron": yearly, "timeZone": "Mars/Base"}}} {
		if answer := addTrigger(t, bob, workflowID, bad, http.StatusUnprocessableEntity); len(answer["error"].(map[string]any)["fields"].(map[string]any)) == 0 {
			t.Errorf("%v was refused without naming a field: %v", bad, answer)
		}
	}
	unapproved := createWorkflow(t, bob, asker(product, false))
	if answer := addTrigger(t, bob, unapproved, schedule("x", yearly, false), http.StatusUnprocessableEntity); errorCode(answer) != "unattended_write" {
		t.Errorf("a trigger on a workflow that writes unapproved = %v", answer)
	}
	trigger := addTrigger(t, bob, workflowID, schedule("weekly", "0 8 * * 1", false), http.StatusCreated)
	id := trigger["id"].(string)
	if status, _ := carol.api(http.MethodGet, "/triggers/"+id, nil); status != http.StatusNotFound {
		t.Errorf("someone else reads the trigger with %d", status)
	}

	// Disconnecting turns it off at the next sweep, with a reason the owner reads, and it stays off until mended.
	if status, answer := bob.api(http.MethodDelete, "/tools/"+toolID.String()+"/connection", nil); status != http.StatusNoContent {
		t.Fatalf("disconnecting = %d %v", status, answer)
	}
	sweeper := triggers.NewScheduler(h.cluster, slog.New(slog.DiscardHandler))
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, off := bob.api(http.MethodGet, "/triggers/"+id, nil)
	disabled, _ := off["disabled"].(map[string]any)
	if off["enabled"] != false || disabled["reason"] != "connection_lost" || disabled["message"] == "" {
		t.Fatalf("after disconnecting the trigger reads %v", off)
	}
	if status, answer := bob.api(http.MethodPost, "/triggers/"+id+"/enable", nil); status != http.StatusConflict {
		t.Errorf("turning it on without the connection = %d %v, want 409", status, answer)
	}
	var actor *uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT actor_user_id FROM audit_log WHERE action = 'trigger.disabled' AND target_id = $1`,
		id).Scan(&actor); err != nil || actor != nil {
		t.Errorf("the audit log records the turning off by %v (%v), want Rotor itself", actor, err)
	}
}

func TestANewVersionThatWritesUnapprovedTurnsTheTriggerOffWhenItFires(t *testing.T) {
	h := newHarness(t)
	product := "version-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	workflowID := createWorkflow(t, bob, asker(product, true))
	trigger := addTrigger(t, bob, workflowID, schedule("yearly", yearly, false), http.StatusCreated)
	id := uuid.MustParse(trigger["id"].(string))
	if status, answer := bob.api(http.MethodPut, "/workflows/"+workflowID, map[string]any{"specYAML": asker(product, false), "baseVersion": 1}); status != http.StatusOK {
		t.Fatalf("saving version 2 = %d %v", status, answer)
	}

	fireAt(t, h, id, nextRun(t, trigger).Add(time.Second))
	refused := lastFire(t, bob, id.String(), "refused", "started", "failed")
	disabled, _ := refused["disabled"].(map[string]any)
	if refused["lastFire"].(map[string]any)["outcome"] != "refused" || disabled["reason"] != "unattended_write" {
		t.Fatalf("the firing of the new version reads %v", refused)
	}
}

func TestASwitchedOffOwnersTriggerIsTurnedOff(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	person := h.addPerson(t, "member")
	var workflowID, triggerID uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO workflow (name, owner_id) VALUES ('switched', $1) RETURNING id`, person).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, workflowID) })
	if err := h.super.QueryRow(ctx, `INSERT INTO trigger (workflow_id, owner_id, name, kind, cron, time_zone)
		VALUES ($1, $2, 'mine', 'schedule', '0 8 * * 1', 'UTC') RETURNING id`, workflowID, person).Scan(&triggerID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, person); err != nil {
		t.Fatal(err)
	}
	if err := triggers.NewScheduler(h.cluster, slog.New(slog.DiscardHandler)).Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	var reason string
	if err := h.super.QueryRow(ctx, `SELECT enabled, disabled_reason FROM trigger WHERE id = $1`, triggerID).Scan(&enabled, &reason); err != nil ||
		enabled || reason != "owner_switched_off" {
		t.Errorf("the switched-off owner's trigger is enabled=%v reason=%q (%v)", enabled, reason, err)
	}
}
