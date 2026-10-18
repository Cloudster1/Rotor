package runnerctl

import (
	"strings"
	"testing"
)

func TestEventsLoseEveryCredentialBeforeTheyAreStored(t *testing.T) {
	payload := `{"node": "ask", "arguments": {"apiKey": "plain-looking", "title": "Login"},
		"log": "GET /x with Authorization: Bearer abc.def-123 and stub_oat_0a1b2c and exciter_oat_9z",
		"nested": [{"refresh_token": "anything"}, "sk-ant-api03-xyz"], "count": 3}`
	out := string(redact([]byte(payload)))
	for _, secret := range []string{"plain-looking", "abc.def-123", "stub_oat_0a1b2c", "exciter_oat_9z", "anything", "sk-ant-api03-xyz"} {
		if strings.Contains(out, secret) {
			t.Errorf("the stored event still holds %q: %s", secret, out)
		}
	}
	for _, kept := range []string{`"title":"Login"`, `"count":3`, `"node":"ask"`, "GET /x with Authorization:"} {
		if !strings.Contains(out, kept) {
			t.Errorf("the stored event lost %q: %s", kept, out)
		}
	}
	if got := string(redact([]byte("not json"))); got != "not json" {
		t.Errorf("an unparsable payload became %q", got)
	}
}
