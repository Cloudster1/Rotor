//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/oidc"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// mintRuns is how many runs mint at once in the single-flight test, as the issue asks.
const mintRuns = 20

// controlPlane builds the api's connections in this process, against the stack's database, OpenBao and the double,
// so a test calls Mint as M2's runs will; secrets may wrap the real OpenBao client.
func controlPlane(t *testing.T, h *harness, secrets connections.Secrets) *connections.Service {
	t.Helper()
	return controlPlaneReaching(t, h, secrets, map[string]string{os.Getenv(envToolStub): os.Getenv(envToolStubInternal)})
}

// controlPlaneReaching is controlPlane for tools at other addresses, such as the suite's Exciter.
func controlPlaneReaching(t *testing.T, h *harness, secrets connections.Secrets, rewrites map[string]string) *connections.Service {
	t.Helper()
	real, _ := newVault(t, os.Getenv(envOpenBao), config.DefaultOpenBaoRole)
	if secrets == nil {
		secrets = real
	}
	reach := oidc.HTTPClient(rewrites)
	callback := os.Getenv(envApp) + config.ConnectionCallbackPath
	registry := tools.NewService(h.cluster, real, reach, callback, true)
	return connections.NewService(h.cluster, secrets, registry, reach, callback, slog.New(slog.DiscardHandler))
}

// connected adds a tool and connects bob to it through the api, answering bob's id and the tool's.
func connected(t *testing.T, stub *toolStub) (*browser, uuid.UUID, uuid.UUID) {
	t.Helper()
	toolID := addTool(t, stub)
	bob := newBrowser(t)
	bob.signIn("bob")
	if landed := bob.landing(t, bob.consent(t, toolID, "bob", "allow")); !strings.Contains(landed, "connected=") {
		t.Fatalf("connecting landed on %q", landed)
	}
	_, me := bob.me()
	id, _ := me["user"].(map[string]any)["id"].(string)
	return bob, uuid.MustParse(id), uuid.MustParse(toolID)
}

// whoami asks the tool's MCP endpoint who a token acts as; it answers the status and the username.
func whoami(t *testing.T, stub *toolStub, token string) (int, string) {
	t.Helper()
	call, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "whoami", "arguments": map[string]any{}}})
	req, _ := http.NewRequest(http.MethodPost, stub.BaseURL+"/api/v1/mcp", bytes.NewReader(call))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := stub.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var answer struct {
		Result struct {
			StructuredContent struct {
				User struct {
					Name string `json:"name"`
				} `json:"user"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &answer)
	return resp.StatusCode, answer.Result.StructuredContent.User.Name
}

func TestTwentyRunsMintAtOnceWithoutEndingTheGrant(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	_, bobID, toolID := connected(t, stub)
	plane := controlPlane(t, h, nil)

	tokens := make([]connections.Token, mintRuns)
	errs := make([]error, mintRuns)
	var wg sync.WaitGroup
	for i := range mintRuns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tokens[i], errs[i] = plane.Mint(context.Background(), connections.MintRequest{UserID: bobID, ToolID: toolID, Scopes: []string{"mcp:read"}, RunID: "run-" + uuid.NewString()})
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("run %d could not mint: %v", i, err)
		}
		if seen[tokens[i].Value] || tokens[i].NarrowedAtTool || tokens[i].ExpiresAt.IsZero() {
			t.Errorf("run %d got %+v, shared or claiming to be narrowed", i, tokens[i])
		}
		seen[tokens[i].Value] = true
	}
	stats := stub.Stats()
	if stats["refreshes"] != float64(mintRuns) || stats["reuseEvents"] != float64(0) || stats["liveGrants"] != float64(1) {
		t.Fatalf("after %d mints the tool counts %v; want one refresh each, no reuse and the grant alive", mintRuns, stats)
	}
	if status, who := whoami(t, stub, tokens[0].Value); status != http.StatusOK || who != "bob" {
		t.Errorf("a minted token at the tool's MCP endpoint = %d as %q, want bob", status, who)
	}
	for _, token := range tokens {
		if where := h.find(t, token.Value); where != "" {
			t.Fatalf("a minted token is in the database, in %s", where)
		}
	}
	var minted int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = 'token.minted' AND target_id = $1`,
		toolID.String()).Scan(&minted); err != nil || minted != mintRuns {
		t.Errorf("the audit log holds %d token.minted entries (%v), want %d", minted, err, mintRuns)
	}
}

func TestARunAskingBeyondTheConsentIsRefusedNamingWhatIsMissing(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	_, bobID, toolID := connected(t, stub)
	_, err := controlPlane(t, h, nil).Mint(context.Background(), connections.MintRequest{UserID: bobID, ToolID: toolID, Scopes: []string{"mcp", "evidence"}})
	var refused *connections.ScopeError
	if !errors.As(err, &refused) || len(refused.Missing) != 1 || refused.Missing[0] != "evidence" {
		t.Fatalf("minting beyond the consent = %v, want evidence named as missing", err)
	}
	if stub.Stats()["refreshes"] != float64(0) {
		t.Error("a refused mint still refreshed at the tool")
	}
}

// reconnecting is OpenBao as the api sees it, except that the first write is overtaken by the person connecting again.
type reconnecting struct {
	*vault.Client
	once  sync.Once
	again func()
}

func (r *reconnecting) PutConnection(ctx context.Context, userID, toolID uuid.UUID, s vault.ConnectionSecret, version int) (int, error) {
	r.once.Do(r.again)
	return r.Client.PutConnection(ctx, userID, toolID, s, version)
}

func TestAMintOvertakenByAReconnectStartsOverWithTheNewGrant(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	bob, bobID, toolID := connected(t, stub)
	real, _ := newVault(t, os.Getenv(envOpenBao), config.DefaultOpenBaoRole)
	secrets := &reconnecting{Client: real, again: func() {
		if landed := bob.landing(t, bob.consent(t, toolID.String(), "bob", "allow")); !strings.Contains(landed, "connected=") {
			t.Errorf("connecting again landed on %q", landed)
		}
	}}
	token, err := controlPlane(t, h, secrets).Mint(context.Background(), connections.MintRequest{UserID: bobID, ToolID: toolID})
	if err != nil {
		t.Fatalf("a mint overtaken by a reconnect = %v, want it to start over", err)
	}
	if status, who := whoami(t, stub, token.Value); status != http.StatusOK || who != "bob" {
		t.Errorf("the token minted after starting over = %d as %q", status, who)
	}
	if stats := stub.Stats(); stats["reuseEvents"] != float64(0) || stats["liveGrants"] != float64(1) {
		t.Errorf("starting over left the tool counting %v; want no reuse and the new grant alive", stats)
	}
}

func TestAGrantTheToolEndedAsksThePersonToConnectAgain(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	bob, bobID, toolID := connected(t, stub)
	stub.EndGrants("bob")
	plane := controlPlane(t, h, nil)
	if _, err := plane.Mint(context.Background(), connections.MintRequest{UserID: bobID, ToolID: toolID}); !errors.Is(err, connections.ErrReconnect) {
		t.Fatalf("minting from an ended grant = %v, want ErrReconnect", err)
	}
	_, answer := bob.api(http.MethodGet, "/connections", nil)
	if list, _ := answer["connections"].([]any); len(list) != 1 || list[0].(map[string]any)["status"] != "reconnect" {
		t.Errorf("bob's connections after the tool ended the grant = %v", answer)
	}
	if secret, err := rootBao(t).Logical().Read("rotor/data/connections/" + bobID.String() + "/" + toolID.String()); err != nil || secret != nil {
		t.Errorf("the dead refresh token is still in OpenBao: %v %v", secret, err)
	}
	if _, err := plane.Mint(context.Background(), connections.MintRequest{UserID: bobID, ToolID: toolID}); !errors.Is(err, connections.ErrReconnect) {
		t.Errorf("minting again before reconnecting = %v, want ErrReconnect", err)
	}
}

func TestRevokingOneRunsTokenLeavesTheOthers(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	bob, bobID, toolID := connected(t, stub)
	plane := controlPlane(t, h, nil)
	first, err := plane.Mint(context.Background(), connections.MintRequest{UserID: bobID, ToolID: toolID, RunID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := plane.Mint(context.Background(), connections.MintRequest{UserID: bobID, ToolID: toolID, RunID: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if err := plane.Revoke(context.Background(), toolID, first.Value); err != nil {
		t.Fatal(err)
	}
	if status, _ := whoami(t, stub, first.Value); status != http.StatusUnauthorized {
		t.Errorf("a revoked run's token answered %d at the tool, want 401", status)
	}
	if status, who := whoami(t, stub, second.Value); status != http.StatusOK || who != "bob" {
		t.Errorf("another run's token after the revocation = %d as %q", status, who)
	}
	_, answer := bob.api(http.MethodGet, "/connections", nil)
	if list, _ := answer["connections"].([]any); len(list) != 1 || list[0].(map[string]any)["lastUsedAt"] == nil {
		t.Errorf("minting did not mark the connection used: %v", answer)
	}
}
