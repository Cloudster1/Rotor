package redact

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryKnownShapeOfCredentialIsCut(t *testing.T) {
	secrets := map[string]string{
		"a bearer header":      "Authorization: Bearer abc.def-123",
		"Rotor's own token":    "rotor_pat_0123456789",
		"an assistant's token": "rotor_oat_0123456789",
		"an assistant's code":  "rotor_oac_0123456789",
		"an Exciter token":     "exciter_oat_9z",
		"an Armature secret":   "armature_whs_abcdef",
		"the stub's token":     "stub_oat_0a1b2c",
		"an OpenBao token":     "hvs.CAESIJ0123456789",
		"an Anthropic key":     "sk-ant-api03-xyz",
		"an OpenAI key":        "sk-proj-" + "0123456789abcdefABCDEF",
		"a GitHub token":       "ghp_" + "0123456789abcdefABCDEF0123456789",
		"a GitHub fine token":  "github_pat_" + "11ABCDEFG0123456789_abcdefghijk",
		"a GitLab token":       "glpat-" + "0123456789abcdefABCD",
		"a Slack token":        "xoxb-" + "1234567890-abcdefghij",
		"an AWS key id":        "AKIAIOSFODNN7EXAMPLE",
		"a Google key":         "AIza" + "SyA0123456789abcdefghijklmnopqrstuv",
		"a Stripe key":         "sk_live_" + "0123456789abcdefgh",
		"a LangSmith key":      "lsv2_pt_0123456789",
		"a JWT":                "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2lnbmF0dXJl",
		"a private key":        "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
		"a password in a URL":  "https://alice:hunter2@git.example/repo",
		"a token mid-sentence": "use ghp_" + "0123456789abcdefABCDEF0123456789 to push",
	}
	for name, text := range secrets {
		out := Text("before " + text + " after")
		if !strings.Contains(out, Redacted) || !strings.HasPrefix(out, "before ") || !strings.HasSuffix(out, " after") {
			t.Errorf("%s: %q became %q", name, text, out)
		}
	}
	for _, secret := range []string{"hunter2", "MIIEow", "AKIAIOSFODNN7EXAMPLE"} {
		for _, text := range secrets {
			if strings.Contains(Text(text), secret) {
				t.Errorf("%q survived in %q", secret, Text(text))
			}
		}
	}
}

func TestOrdinaryTextIsLeftAlone(t *testing.T) {
	for _, text := range []string{
		"a task-force met about the risk-free plan",
		"eyJ is how base64 of JSON starts",
		"see https://exciter.example/requirements/12",
		"AKIA alone is not a key",
		"the sk-learn library",
		"tokens: 1200",
	} {
		if out := Text(text); out != text {
			t.Errorf("%q became %q", text, out)
		}
	}
}

func TestAFieldNamedForACredentialIsCutWhateverItHolds(t *testing.T) {
	out := string(JSON([]byte(`{"node": "ask", "arguments": {"apiKey": "plain-looking", "title": "Login"},
		"nested": [{"refresh_token": "anything"}], "key": "set", "secret": "kept", "count": 3}`)))
	for _, gone := range []string{"plain-looking", "anything"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q survived: %s", gone, out)
		}
	}
	for _, kept := range []string{`"title":"Login"`, `"count":3`, `"node":"ask"`, `"key":"set"`, `"secret":"kept"`} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q was lost: %s", kept, out)
		}
	}
	if got := string(JSON([]byte("not json"))); got != "not json" {
		t.Errorf("an unparsable document became %q", got)
	}
}

func TestALogLineLosesEveryCredential(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: Attr}))
	log.Error("called with Bearer abc.def-123", "error", errors.New("tool said ghp_"+"0123456789abcdefABCDEF0123456789"),
		"token", "plain-looking", "run", "r1", "key", "set")
	out := buf.String()
	for _, gone := range []string{"abc.def-123", "ghp_" + "0123456789", "plain-looking"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q reached the log: %s", gone, out)
		}
	}
	for _, kept := range []string{`"run":"r1"`, `"key":"set"`, "called with"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q was lost from the log: %s", kept, out)
		}
	}
}

func TestTheEmbeddedPatternsAreTheOnesInAPI(t *testing.T) {
	published, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "secret-patterns.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(published, patternsFile) {
		t.Fatal("backend/internal/redact/secret-patterns.json differs from api/; run make secret-patterns")
	}
}
