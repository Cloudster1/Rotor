//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/connections"
)

// ApproveClients allows every pending client of an Armature tenant, as its administrator does (#45).
func (s *toolStub) ApproveClients() {
	s.controlCall(http.MethodPost, "/control/tenants/"+s.name+"/clients/approve", nil, http.StatusOK)
}

// armatureConnected adds an Armature tenant, which holds Rotor pending until allowed, then connects bob to it.
func armatureConnected(t *testing.T, h *harness) (*toolStub, uuid.UUID, uuid.UUID) {
	t.Helper()
	stub := newToolStub(t, map[string]any{"product": "armature"})
	alice := signedIn(t, "alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	tool := toolOf(t, answer)
	id, _ := tool["id"].(string)
	if status != http.StatusCreated || tool["status"] != "awaiting_approval" || tool["resource"] != stub.BaseURL+"/api/v1" ||
		tool["mcpURL"] != stub.BaseURL+"/api/v1/mcp" {
		t.Fatalf("adding Armature = %d %v", status, answer)
	}
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+id, nil) })

	stub.ApproveClients()
	bob := signedIn(t, "bob")
	if landed := bob.landing(t, bob.consent(t, id, "bob", "allow")); !strings.Contains(landed, "connected=") {
		t.Fatalf("connecting to Armature landed on %q", landed)
	}
	if _, answer := alice.api(http.MethodGet, "/tools/"+id, nil); toolOf(t, answer)["status"] != "ready" {
		t.Errorf("Armature after the first connection = %v, want ready", answer)
	}
	var allowed int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = 'tool.allowed' AND target_id = $1`, id).
		Scan(&allowed); err != nil || allowed != 1 {
		t.Errorf("tool.allowed entries = %d %v, want 1", allowed, err)
	}
	_, me := bob.me()
	bobID, _ := me["user"].(map[string]any)["id"].(string)
	return stub, uuid.MustParse(bobID), uuid.MustParse(id)
}

// Armature keeps one access token per grant and ends the grant with it, so each run's token is exchanged from it:
// overlapping runs keep theirs, revoking one ends only it, and the grant lives on (#45).
func TestEachRunAtArmatureGetsAnExchangedTokenOfItsOwn(t *testing.T) {
	h := newHarness(t)
	stub, bobID, toolID := armatureConnected(t, h)
	plane := controlPlane(t, h, nil)
	ctx := context.Background()

	reading, err := plane.Mint(ctx, connections.MintRequest{UserID: bobID, ToolID: toolID, Scopes: []string{"read"}, RunID: "run-a"})
	if err != nil {
		t.Fatal(err)
	}
	writing, err := plane.Mint(ctx, connections.MintRequest{UserID: bobID, ToolID: toolID, Scopes: []string{"issue.write"}, RunID: "run-b"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(reading.Scopes, " ") != "read" || !reading.NarrowedAtTool || strings.Join(writing.Scopes, " ") != "issue.write read" {
		t.Errorf("tokens carry %v and %v, want read alone, then issue.write with read beside it", reading.Scopes, writing.Scopes)
	}
	for _, token := range []connections.Token{reading, writing} {
		if status, who := whoami(t, stub, token.Value); status != http.StatusOK || who != "bob" {
			t.Errorf("a run's token after another run's refresh = %d as %q, want bob", status, who)
		}
	}

	if err := plane.Revoke(ctx, toolID, reading.Value); err != nil {
		t.Fatal(err)
	}
	if status, _ := whoami(t, stub, reading.Value); status != http.StatusUnauthorized {
		t.Errorf("a revoked run's token = %d, want 401", status)
	}
	if status, _ := whoami(t, stub, writing.Value); status != http.StatusOK {
		t.Errorf("revoking one run's token ended another's: %d", status)
	}
	stats := stub.Stats()
	if stats["grantsEnded"] != float64(0) || stats["exchanges"] != float64(2) || stats["liveGrants"] != float64(1) {
		t.Errorf("the tool counts %v, want two exchanges and the grant alive", stats)
	}
	if _, err := plane.Mint(ctx, connections.MintRequest{UserID: bobID, ToolID: toolID, RunID: "run-c"}); err != nil {
		t.Errorf("the next run cannot mint: %v", err)
	}
	_, err = plane.Mint(ctx, connections.MintRequest{UserID: bobID, ToolID: toolID, Scopes: []string{"webhooks.partner"}, RunID: "run-d"})
	if !errors.Is(err, connections.ErrScopeNotConsented) {
		t.Errorf("a scope bob never consented to = %v, want refused", err)
	}
}

// Armature without token exchange would end the person's grant with every run, so Rotor does not add it.
func TestArmatureWithoutTokenExchangeIsRefused(t *testing.T) {
	stub := newToolStub(t, map[string]any{"product": "armature", "grantTypes": []string{"authorization_code", "refresh_token"}})
	alice := signedIn(t, "alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	if status != http.StatusUnprocessableEntity || !strings.Contains(stringOf(answer), "token exchange") {
		t.Errorf("adding Armature without the exchange = %d %v", status, answer)
	}
}
