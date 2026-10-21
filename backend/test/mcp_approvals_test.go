//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// An assistant reads the approval its person may decide and is told to decide it in Rotor; deciding it is no tool,
// the token sent straight to the decision is refused, and the database refuses it too (#47).
func TestAnAssistantReadsAnApprovalButNeverDecidesIt(t *testing.T) {
	h := newHarness(t)
	product := "assist-approve-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	carol := signedIn(t, "carol")
	run := startRun(t, bob, createWorkflow(t, bob, reviewed(product, "quality", "")), map[string]any{"title": "Login"}, http.StatusCreated)
	runID := run["id"].(string)
	approvalID := approvalOf(t, bob, runID)

	_, access := connectHand(t, h, carol, map[string]any{"allow": true})
	_, _, listed := mcpAs(t, access, "tools/list", nil)
	for name := range toolNames(listed) {
		if strings.Contains(name, "decide") || strings.Contains(name, "approve") || strings.Contains(name, "reject") {
			t.Errorf("an assistant is offered %s", name)
		}
	}
	read := callTool(t, access, "get_approval", map[string]any{"approvalID": approvalID})
	if read["isError"] == true || read["structuredContent"].(map[string]any)["canDecide"] != true {
		t.Errorf("carol's assistant reads the approval as %v", read)
	}
	waiting := callTool(t, access, "list_approvals", map[string]any{"waitingOnMe": "true"})
	if !strings.Contains(stringify(waiting["structuredContent"]), approvalID) {
		t.Errorf("carol's assistant does not find the approval waiting on her: %v", waiting)
	}
	if status := bearerAt(t, access, http.MethodPost, "/approvals/"+approvalID+"/decision"); status != http.StatusUnauthorized {
		t.Errorf("the token sent straight to the decision answers %d", status)
	}

	// Straight through SQL, as either role, a transaction an assistant acts in decides nothing.
	carolID := personOf(t, carol)
	var grant uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM oauth_grant WHERE user_id = $1`, carolID).Scan(&grant); err != nil {
		t.Fatal(err)
	}
	through := db.WithVia(db.WithUser(context.Background(), carolID), grant)
	err := h.cluster.WriteAdmin(through, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE approval SET status = 'approved', decided_at = now() WHERE id = $1`, approvalID)
		return err
	})
	insufficientPrivilege(t, "approving through an assistant", err)

	// The person decides in Rotor, and the run goes on to its end, so no later test's worker takes it up.
	decide(t, carol, approvalID, map[string]any{"decision": "approve"}, http.StatusOK)
	waitRun(t, bob, runID, "succeeded", "failed", "cancelled")
	waitTornDown(t, h, uuid.MustParse(runID))
}
