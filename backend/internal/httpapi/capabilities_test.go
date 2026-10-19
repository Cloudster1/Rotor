package httpapi

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// capabilityName is name@version, as the shared list in Dynamo spells them.
var capabilityName = regexp.MustCompile(`^[a-z][a-z-]*(\.[a-z][a-z-]*)*@[1-9][0-9]*$`)

// A partner offers a feature on seeing its name at /meta, so the list is the one docs/integration.md describes,
// sorted, and nothing else.
func TestMetaReportsTheIntegrationCapabilities(t *testing.T) {
	want := []string{"hooks@1", "runs@1", "workflows@1"}
	if got := capabilities(); !slices.Equal(got, want) {
		t.Errorf("capabilities() = %v, want %v", got, want)
	}
}

// Renaming, dropping or leaving unbuilt an operation a capability promises must fail here, not at the partner that
// relied on it.
func TestEveryCapabilityRestsOnWhatTheBuildHas(t *testing.T) {
	routes := map[string]Route{}
	for _, r := range Catalog() {
		routes[r.Method+" "+r.Path] = r
	}
	for _, c := range rotorCapabilities {
		if len(c.operations) == 0 {
			t.Errorf("%s names no operation", c.name)
		}
		for _, op := range c.operations {
			r, ok := routes[op]
			switch {
			case !ok:
				t.Errorf("%s relies on %q, which is not in the operations table", c.name, op)
			case r.Pending:
				t.Errorf("%s relies on %q, which is agreed but not built", c.name, op)
			}
		}
	}
}

// A partner matches names exactly, and a breaking change raises the number.
func TestCapabilityNamesAreVersionedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range rotorCapabilities {
		if !capabilityName.MatchString(c.name) {
			t.Errorf("%q is not name@version, such as runs@1", c.name)
		}
		if seen[c.name] {
			t.Errorf("%q is listed twice", c.name)
		}
		seen[c.name] = true
		if strings.TrimSpace(c.promise) == "" {
			t.Errorf("%s says nothing of what it promises", c.name)
		}
	}
}
