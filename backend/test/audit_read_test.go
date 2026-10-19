//go:build integration

package test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// seedAudit writes entries about one target straight into the log, a second apart, oldest first.
func seedAudit(t *testing.T, h *harness, target string, targetTypes ...string) {
	t.Helper()
	for i, kind := range targetTypes {
		if _, err := h.super.Exec(context.Background(), `INSERT INTO audit_log (action, target_type, target_id, data, created_at)
			VALUES ('run.started', $1, $2, '{"n": 1}', now() - make_interval(secs => $3))`, kind, target, len(targetTypes)-i); err != nil {
			t.Fatal(err)
		}
	}
}

// export downloads the log as b, answering the status, the content type and the body.
func export(t *testing.T, b *browser, query string) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, b.app+"/api/v1/audit/export?"+query, nil)
	resp, body := b.do(req)
	return resp.StatusCode, resp.Header.Get("Content-Type"), body
}

func TestAnAdministratorReadsTheLogInWindowsAndExportsIt(t *testing.T) {
	h := newHarness(t)
	alice, bob := signedIn(t, "alice"), signedIn(t, "bob")
	target := uuid.NewString()
	seedAudit(t, h, target, "run", "run", "=cmd")

	if status, _ := bob.api(http.MethodGet, "/audit", nil); status != http.StatusForbidden {
		t.Errorf("a member reads the log with %d, want 403", status)
	}
	if status, _ := alice.api(http.MethodGet, "/audit?action=no.such", nil); status != http.StatusUnprocessableEntity {
		t.Errorf("an unknown action = %d, want 422", status)
	}
	_, first := alice.api(http.MethodGet, "/audit?target="+target+"&limit=2", nil)
	entries, _ := first["entries"].([]any)
	next, _ := first["next"].(string)
	if len(entries) != 2 || next == "" || entries[0].(map[string]any)["targetType"] != "=cmd" {
		t.Fatalf("the first window = %v", first)
	}
	_, second := alice.api(http.MethodGet, "/audit?target="+target+"&limit=2&cursor="+next, nil)
	if rest, _ := second["entries"].([]any); len(rest) != 1 || second["next"] != nil {
		t.Errorf("the second window = %v", second)
	}

	status, kind, body := export(t, alice, "target="+target)
	lines, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if status != http.StatusOK || !strings.HasPrefix(kind, "text/csv") || err != nil || len(lines) != 4 || lines[0][0] != "time" {
		t.Fatalf("the CSV export = %d %s %v: %q", status, kind, err, body)
	}
	if lines[1][4] != "'=cmd" {
		t.Errorf("a cell a spreadsheet would read as a formula is %q", lines[1][4])
	}
	status, kind, body = export(t, alice, "format=json&target="+target)
	var records []map[string]any
	if status != http.StatusOK || kind != "application/json" || json.Unmarshal([]byte(body), &records) != nil || len(records) != 3 {
		t.Fatalf("the JSON export = %d %s: %q", status, kind, body)
	}
	if status, _, _ := export(t, bob, "target="+target); status != http.StatusForbidden {
		t.Errorf("a member exports the log with %d", status)
	}
	var noted int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log a JOIN app_user u ON u.id = a.actor_user_id
		WHERE a.action = 'audit.exported' AND a.data->>'targetId' = $1 AND u.email LIKE 'alice%'`, target).Scan(&noted); err != nil || noted != 2 {
		t.Errorf("the log records %d exports by alice (%v), want 2", noted, err)
	}
}

func TestAnExportTooLargeIsRefusedRatherThanCut(t *testing.T) {
	h := newHarness(t)
	alice := signedIn(t, "alice")
	target := uuid.NewString()
	if _, err := h.super.Exec(context.Background(), `INSERT INTO audit_log (action, target_type, target_id)
		SELECT 'run.started', 'run', $1 FROM generate_series(1, 50001)`, target); err != nil {
		t.Fatal(err)
	}
	status, _, body := export(t, alice, "target="+target)
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "export_too_large") {
		t.Errorf("an export of 50001 entries = %d %s", status, body)
	}
}

func TestTheAuditLogCannotBeEmptied(t *testing.T) {
	h := newHarness(t)
	ctx := db.WithUser(context.Background(), h.addPerson(t, "admin"))
	for role, run := range map[string]func(context.Context, func(context.Context, db.DBTX) error) error{
		"rotor_app": h.cluster.Write, "rotor_admin": h.cluster.WriteAdmin} {
		permissionDenied(t, "TRUNCATE as "+role, run(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `TRUNCATE audit_log`)
			return err
		}))
	}
}

func TestARunsSandboxAndEndAreAudited(t *testing.T) {
	h := newHarness(t)
	product := "audited-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	run := startRun(t, bob, createWorkflow(t, bob, greeter(uniqueName("audited"), product)), map[string]any{"greeting": "Hi"}, http.StatusCreated)
	runID := run["id"].(string)
	waitRun(t, bob, runID, "succeeded", "failed")
	deadline := time.Now().Add(sandboxWait)
	for !h.audited(t, "sandbox.destroyed", runID) && time.Now().Before(deadline) {
		time.Sleep(time.Second)
	}
	for _, action := range []string{"sandbox.spawned", "sandbox.destroyed", "run.ended"} {
		if !h.audited(t, action, runID) {
			t.Errorf("the log has no %s for the run", action)
		}
	}
	var keys string
	if err := h.super.QueryRow(context.Background(), `SELECT string_agg(k, ',' ORDER BY k) FROM audit_log, jsonb_object_keys(data) k
		WHERE action = 'sandbox.spawned' AND target_id = $1`, runID).Scan(&keys); err != nil || keys != "image,sandboxId" {
		t.Errorf("a spawned sandbox's entry holds %q (%v); it must never hold its environment", keys, err)
	}
}
