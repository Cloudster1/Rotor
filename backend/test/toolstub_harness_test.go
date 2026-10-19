//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// Where the suite finds the tool test double (#8): the address a browser and an administrator use, the one on the
// stack's network it is reached at instead, and its control API, which only the stack's network reaches.
const (
	envToolStub         = "ROTOR_TEST_TOOLSTUB_URL"
	envToolStubInternal = "ROTOR_TEST_TOOLSTUB_INTERNAL_URL"
	envToolStubControl  = "ROTOR_TEST_TOOLSTUB_CONTROL_URL"
)

// toolStub is one tenant of the test double: a tool of its own that no other test shares.
type toolStub struct {
	t *testing.T
	// BaseURL is what an administrator types to add the tool; client reaches it as a browser on this machine would.
	BaseURL, name, control string
	client                 *http.Client
}

// newToolStub makes a fresh tenant with the options given (see toolstub.Options), named after the test.
func newToolStub(t *testing.T, options map[string]any) *toolStub {
	t.Helper()
	for _, key := range []string{envToolStub, envToolStubInternal, envToolStubControl} {
		if os.Getenv(key) == "" {
			t.Skipf("%s is not set; run the suite with make test-integration", key)
		}
	}
	name := strings.ToLower(strings.NewReplacer("/", "-", "_", "-").Replace(t.Name()))
	name = strings.Trim(name, "-")
	if len(name) > 40 {
		name = name[:40]
	}
	name += "-" + uuid.NewString()[:8]
	s := &toolStub{t: t, name: name, control: os.Getenv(envToolStubControl), BaseURL: os.Getenv(envToolStub) + "/t/" + name,
		client: oidc.HTTPClient(map[string]string{os.Getenv(envToolStub): os.Getenv(envToolStubInternal)})}
	body := map[string]any{"name": name}
	for k, v := range options {
		body[k] = v
	}
	s.controlCall(http.MethodPost, "/control/tenants", body, http.StatusCreated)
	t.Cleanup(func() { s.controlCall(http.MethodDelete, "/control/tenants/"+name, nil, http.StatusNoContent) })
	return s
}

// controlCall drives the control API and answers its JSON.
func (s *toolStub) controlCall(method, path string, body any, want int) map[string]any {
	s.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, s.control+path, reader)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("control %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		s.t.Fatalf("control %s %s = %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// Stats are the tenant's counters: refreshes, reuseEvents, liveGrants and the rest of toolstub.Stats.
func (s *toolStub) Stats() map[string]any {
	return s.controlCall(http.MethodGet, "/control/tenants/"+s.name+"/stats", nil, http.StatusOK)
}

// EndGrants ends grants as the tool would on its own, one person's when username is given.
func (s *toolStub) EndGrants(username string) {
	path := "/control/tenants/" + s.name + "/grants"
	if username != "" {
		path += "?username=" + username
	}
	s.controlCall(http.MethodDelete, path, nil, http.StatusOK)
}

// ListAsWrites makes the tenant list the calls named as writing, as a tool that changed them after a workflow was
// saved against it (#90).
func (s *toolStub) ListAsWrites(calls ...string) {
	s.controlCall(http.MethodPut, "/control/tenants/"+s.name+"/writes", map[string]any{"calls": calls}, http.StatusOK)
}

// ForgetClients forgets every client and its grants, as Exciter sweeps an unused one.
func (s *toolStub) ForgetClients() {
	s.controlCall(http.MethodDelete, "/control/tenants/"+s.name+"/clients", nil, http.StatusOK)
}

// get fetches a path under the tenant's public address as a browser would.
func (s *toolStub) get(path string) (*http.Response, []byte) {
	s.t.Helper()
	resp, err := s.client.Get(s.BaseURL + path)
	if err != nil {
		s.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}
