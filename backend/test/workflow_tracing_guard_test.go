//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Only a workflow's administrators, an administrator of Rotor among them, change its tracing: the api refuses anyone
// else, and so does the database, straight through SQL as rotor_app.
func TestOnlyAWorkflowsAdministratorsChangeItsTracing(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice, bob, carol, dave := newBrowser(t), newBrowser(t), newBrowser(t), newBrowser(t)
	alice.signIn("alice")
	bob.signIn("bob")
	carol.signIn("carol")
	dave.signIn("dave")
	workflowID := createWorkflow(t, bob, approvalOnly(uniqueName("tracing-guard"), "Go on?"))
	h.grantPerson(t, "editor", uuid.MustParse(workflowID), uuid.MustParse(userID(t, carol)))
	path := "/workflows/" + workflowID + "/settings/tracing"
	none := map[string]any{"content": "none", "destinations": nil, "sampleRatio": 1}

	status, answer := carol.api(http.MethodGet, path, nil)
	tracing, _ := answer["tracing"].(map[string]any)
	if status != http.StatusOK || answer["canChange"] != false || tracing["content"] != "inherit" || tracing["sampleRatio"] != float64(1) ||
		tracing["destinations"] != nil || tracing["changedBy"] != nil {
		t.Errorf("an editor reading the defaults = %d %v", status, answer)
	}
	if status, answer := carol.api(http.MethodPut, path, none); status != http.StatusForbidden || errorCode(answer) != "forbidden" {
		t.Errorf("an editor changing the workflow's tracing = %d %v", status, answer)
	}
	if status, _ := dave.api(http.MethodGet, path, nil); status != http.StatusNotFound {
		t.Errorf("someone without a grant reading the workflow's tracing = %d, want 404", status)
	}
	if status, _ := dave.api(http.MethodPut, path, none); status != http.StatusNotFound {
		t.Errorf("someone without a grant changing the workflow's tracing = %d, want 404", status)
	}

	refusals := []struct {
		what, field string
		body        map[string]any
	}{
		{"more text than the installation", "content", map[string]any{"content": "keep", "sampleRatio": 1}},
		{"a share past 1", "sampleRatio", map[string]any{"content": "inherit", "sampleRatio": 1.5}},
		{"a destination that is none", "destinations", map[string]any{"content": "inherit", "sampleRatio": 1,
			"destinations": []string{uuid.NewString()}}},
	}
	for _, r := range refusals {
		status, answer := bob.api(http.MethodPut, path, r.body)
		fields, _ := answer["error"].(map[string]any)["fields"].(map[string]any)
		if status != http.StatusUnprocessableEntity || errorCode(answer) != "validation_failed" || fields[r.field] == nil {
			t.Errorf("%s = %d %v", r.what, status, answer)
		}
	}
	if got := setTracing(t, bob, workflowID, none); got["canChange"] != true {
		t.Errorf("the workflow's owner reads %v", got)
	}
	// An administrator of Rotor holds no grant on the workflow, and changes it all the same.
	if got := setTracing(t, alice, workflowID, map[string]any{"content": "inherit", "destinations": []string{},
		"sampleRatio": 0.5}); stringify(got["tracing"].(map[string]any)["destinations"]) != "[]" {
		t.Errorf("an administrator of Rotor choosing no destination reads %v", got)
	}

	owner, editor, stranger := h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	workflow := h.ownedWorkflow(t, owner)
	h.grantPerson(t, "editor", workflow, editor)
	add := `INSERT INTO workflow_tracing (workflow_id, content) VALUES ($1, 'none')`
	err := asApp(ctx, h.super, editor, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, add, workflow)
		return err
	})
	refusedBy(t, "an editor choosing the workflow's tracing through SQL", err, "42501", "")
	err = asApp(ctx, h.super, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO workflow_tracing (workflow_id, content) VALUES ($1, 'keep')`, workflow)
		return err
	})
	refusedBy(t, "more text than the installation through SQL", err, "23514", "")
	if err := asApp(ctx, h.super, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, add, workflow)
		return err
	}); err != nil {
		t.Fatalf("the workflow's owner choosing its tracing through SQL = %v", err)
	}
	// Each asApp rolls back, so the row the others try to change is made behind the policies' back.
	if _, err := h.super.Exec(ctx, add, workflow); err != nil {
		t.Fatal(err)
	}
	err = asApp(ctx, h.super, editor, func(tx pgx.Tx) error {
		var seen int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM workflow_tracing WHERE workflow_id = $1`, workflow).Scan(&seen); err != nil || seen != 1 {
			t.Errorf("an editor reads %d of the workflow's tracing through SQL (%v)", seen, err)
		}
		if tag, err := tx.Exec(ctx, `UPDATE workflow_tracing SET content = 'inherit' WHERE workflow_id = $1`, workflow); err != nil ||
			tag.RowsAffected() != 0 {
			t.Errorf("an editor changed %d of the workflow's tracing through SQL (%v)", tag.RowsAffected(), err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM workflow_tracing WHERE workflow_id = $1`, workflow)
		if tag.RowsAffected() != 0 {
			t.Errorf("an editor removed the workflow's tracing through SQL")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := asApp(ctx, h.super, stranger, func(tx pgx.Tx) error {
		var seen int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM workflow_tracing WHERE workflow_id = $1`, workflow).Scan(&seen)
		if seen != 0 {
			t.Errorf("someone without a grant reads the workflow's tracing through SQL")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
