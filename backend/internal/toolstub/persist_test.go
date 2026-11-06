package toolstub

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// restarted is a new stub that keeps its state in path, as the stack's container does after a rebuild.
func restarted(t *testing.T, path string) *contract {
	t.Helper()
	s := newTestStub(t)
	s.KeepStateIn(path)
	return newContractFor(t, s)
}

func TestClientAndGrantSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	before := restarted(t, path)
	conn := before.connect("kept", "alice", "")

	after := restarted(t, path)
	_, conn = after.refresh("kept", conn, "", http.StatusOK)
	if got := after.whoami("kept", conn.access); nameOf(got) != "alice" {
		t.Errorf("the restored grant answers as %q, want alice", nameOf(got))
	}
	if got := after.stats("kept"); got.Clients != 1 || got.LiveGrants != 1 {
		t.Errorf("the restored tenant holds %d clients and %d grants, want 1 and 1", got.Clients, got.LiveGrants)
	}
	// The refresh above rotated the token; the next restart must hold the new one, not the old.
	again := restarted(t, path)
	again.refresh("kept", conn, "", http.StatusOK)
}

func TestResetClearsTheSavedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	c := restarted(t, path)
	conn := c.connect("gone", "alice", "")
	c.controlled("DELETE", "/control/tenants", nil, http.StatusOK)

	after := restarted(t, path)
	resp, _ := after.call("POST", "/t/gone/api/v1/oauth/token", request{basicID: conn.clientID, basicToken: conn.clientSecret,
		form: url.Values{"grant_type": {"refresh_token"}, "refresh_token": {conn.refresh}}})
	if resp.StatusCode == http.StatusOK {
		t.Error("a reset stub still refreshes a token from before the reset")
	}
}

func TestForgetClientsIsSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	c := restarted(t, path)
	conn := c.connect("swept", "alice", "")
	c.controlled("DELETE", "/control/tenants/swept/clients", nil, http.StatusOK)
	if resp, _ := restarted(t, path).call("POST", "/t/swept/api/v1/oauth/token", request{basicID: conn.clientID, basicToken: conn.clientSecret,
		form: url.Values{"grant_type": {"refresh_token"}, "refresh_token": {conn.refresh}}}); resp.StatusCode == http.StatusOK {
		t.Error("a forgotten client came back after a restart")
	}
}

func TestMissingOrCorruptStateStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), stateFileMode); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(dir, "none.json"), "corrupt": corrupt} {
		s := newTestStub(t)
		s.KeepStateIn(path)
		if len(s.tenants) != 0 {
			t.Errorf("%s state left %d tenants", name, len(s.tenants))
		}
		c := newContractFor(t, s)
		c.connect("fresh", "alice", "")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s state was not written once something changed: %v", name, err)
		}
	}
}

func TestNoStateFileMeansNoPersistence(t *testing.T) {
	s := newTestStub(t)
	newContractFor(t, s).connect("memory", "alice", "")
	if s.statePath != "" || s.lastSaved != nil {
		t.Error("a stub told nothing about a state file saved one")
	}
}
