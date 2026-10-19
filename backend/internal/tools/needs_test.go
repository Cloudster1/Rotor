package tools

import (
	"errors"
	"strings"
	"testing"
)

// A tool is read by exact name@version, and a list never read counts as offering everything.
func TestWhatAToolOffersIsReadByExactName(t *testing.T) {
	if m := Offered(nil).Lacks("exciter", CapabilityMCP); m != nil {
		t.Errorf("a list never read lacks %v", m)
	}
	if m := Offered([]string{"mcp@2"}).Lacks("exciter", CapabilityMCP); m == nil || !errors.Is(m, ErrLacks) {
		t.Fatalf("mcp@2 stands in for mcp@1: %v", m)
	}
	m := Offered([]string{}).Lacks("armature", CapabilityWebhooks)
	if m == nil || !strings.Contains(m.Message, "Armature does not offer webhooks@1") || !strings.Contains(m.Message, "Update Armature") {
		t.Errorf("the sentence = %v, want the tool, the capability and what to update", m)
	}
	got := Tool{Name: "stator", Capabilities: []string{"webhooks@1"}}.Missing()
	if len(got) != 1 || got[0].Capability != CapabilityMCP {
		t.Errorf("Missing = %v, want mcp@1 alone", got)
	}
	if len(Needs()) != len(needs) {
		t.Errorf("Needs = %v", Needs())
	}
}
