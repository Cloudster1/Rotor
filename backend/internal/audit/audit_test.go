package audit

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestEveryActionIsNamedOnceAsDomainDotVerb(t *testing.T) {
	shape := regexp.MustCompile(`^[a-z]+\.[a-z_]+$`)
	seen := map[string]bool{}
	for _, a := range Actions {
		if !shape.MatchString(a) {
			t.Errorf("action %q is not domain.verb", a)
		}
		if seen[a] {
			t.Errorf("action %q is listed twice", a)
		}
		seen[a] = true
	}
}

func TestACredentialNeverReachesTheRecord(t *testing.T) {
	data := map[string]any{
		"name":            "Deploy bot",
		"clientSecret":    "s3cr3t-value",
		"webhookSecret":   "kept",
		"apiToken":        "anything at all",
		"password":        "",
		"tokensForgotten": 3,
		"enabled":         true,
		"note":            "exciter_pat_abcdef",
		"idToken":         "eyJhbGciOiJSUzI1NiJ9.e30.sig",
		"forwarded":       "eyJhbGciOiJSUzI1NiJ9.e30.sig",
		"grants": []any{
			map[string]any{"id": "x", "token": "armature_pat_123"},
			"stator_whs_abc",
		},
		"nested": map[string]any{"Password": "hunter2", "secretChange": "set"},
	}
	raw, err := Scrub(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"s3cr3t-value", "anything at all", "exciter_pat_", "armature_pat_", "stator_whs_", "hunter2", "eyJ"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("the record holds %q: %s", leaked, raw)
		}
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Deploy bot" || got["webhookSecret"] != "kept" || got["tokensForgotten"] != float64(3) || got["enabled"] != true {
		t.Errorf("what is no secret was changed: %s", raw)
	}
	if got["clientSecret"] != Redacted || got["note"] != Redacted || got["forwarded"] != Redacted {
		t.Errorf("a secret is not marked redacted: %s", raw)
	}
	if nested := got["nested"].(map[string]any); nested["secretChange"] != "set" || nested["Password"] != Redacted {
		t.Errorf("the nested data reads %v", nested)
	}
}
