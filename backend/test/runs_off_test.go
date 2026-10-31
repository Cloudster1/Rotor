//go:build integration

package test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/httpapi"
	"github.com/cloudster1/rotor/backend/internal/runs"
)

// An api that is told it has no sandbox backend refuses the start with a sentence before any run row exists, and
// says so at /meta; the stack's own api, which has the Docker backend, says runs are on.
func TestAnApiWithoutSandboxesRefusesAStartAndSaysSo(t *testing.T) {
	h := newHarness(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	workflowID := createWorkflow(t, alice, chatty())

	if _, meta := alice.api(http.MethodGet, "/meta", nil); meta["runs"] != true {
		t.Errorf("the stack's /meta says runs = %v, want true", meta["runs"])
	}

	service := runs.NewService(h.cluster, runKeys(t), nil, nil, nil, nil)
	service.NoSandboxes = true
	off := &httpapi.Server{DB: h.cluster, Log: slog.New(slog.DiscardHandler), Sessions: auth.NewService(h.cluster, h.cfg.Auth.SessionTTL),
		CookieName: h.cfg.Auth.SessionCookie, Runs: service, NoSandboxes: true}
	server := httptest.NewServer(off.Routes(nil))
	t.Cleanup(server.Close)

	// Another api than the stack's: the session cookie goes along by hand.
	call := func(method, path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(method, server.URL+"/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		app, _ := url.Parse(alice.app + "/api/v1")
		for _, c := range alice.client.Jar.Cookies(app) {
			req.AddCookie(c)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if _, meta := call(http.MethodGet, "/meta", ""); meta["runs"] != false {
		t.Errorf("/meta without sandboxes says runs = %v, want false", meta["runs"])
	}
	status, answer := call(http.MethodPost, "/workflows/"+workflowID+"/runs", `{"inputs":{}}`)
	refusal, _ := answer["error"].(map[string]any)
	message, _ := refusal["message"].(string)
	if status != http.StatusConflict || refusal["code"] != "runs_off" ||
		!strings.Contains(message, "runs no workflows") || !strings.Contains(message, "installs Rotor with sandboxes") {
		t.Errorf("starting a run without sandboxes = %d %v", status, answer)
	}
	var rows int
	if err := h.super.QueryRow(t.Context(), `SELECT count(*) FROM run WHERE workflow_id = $1`, uuid.MustParse(workflowID)).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("the refused start left %d run rows", rows)
	}
}
