package spec

import (
	"os"
	"path/filepath"
	"testing"
)

type catalog map[string]ToolInfo

func (c catalog) Tool(name string) (ToolInfo, bool) { info, ok := c[name]; return info, ok }

var exciter = catalog{"exciter": {Scopes: []string{"mcp", "mcp:read"}, Calls: map[string]CallInfo{
	"get_requirement": {ReadOnly: true}, "check_requirement_text": {ReadOnly: true}, "search_requirements": {ReadOnly: true},
	"get_project": {ReadOnly: true}, "list_requirements": {ReadOnly: true},
	"request_review": {}, "create_requirement": {}, "update_requirement": {Destructive: true},
}}}

func parseFixture(t *testing.T, name string) *Spec {
	t.Helper()
	source, _ := os.ReadFile(filepath.Join(fixtures, "valid", name))
	s, problems := Parse(source)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	return s
}

func codes(problems []Problem) map[string]string {
	out := map[string]string{}
	for _, p := range problems {
		out[p.Path] = p.Code
	}
	return out
}

func TestTheExamplesHoldAgainstTheTools(t *testing.T) {
	for _, name := range []string{"draft-requirements.yaml", "triage-with-approval.yaml", "transform-with-code.yaml"} {
		if problems := parseFixture(t, name).CheckAgainst(exciter); len(problems) > 0 {
			t.Errorf("%s: %+v", name, problems)
		}
	}
}

func TestTheToolsTellWhatTheSpecCannot(t *testing.T) {
	s := parseFixture(t, "triage-with-approval.yaml")
	read := s.Tools["exciter-read"]
	read.AllowTools = append(read.AllowTools, "update_requirement", "no_such_call")
	read.Scopes = append(read.Scopes, "evidence:write")
	s.Tools["exciter-read"] = read
	got := codes(s.CheckAgainst(exciter))
	for path, want := range map[string]string{
		"/tools/exciter-read/allowTools/2": CodeWritesOnRead,
		"/tools/exciter-read/allowTools/3": CodeUnknownCall,
		"/tools/exciter-read/scopes/1":     CodeUnknownScope,
	} {
		if got[path] != want {
			t.Errorf("at %s got %q, want %s (all: %v)", path, got[path], want, got)
		}
	}
	if got := codes(s.CheckAgainst(catalog{})); got["/tools/exciter-read/tool"] != CodeUnknownTool {
		t.Errorf("an unknown tool = %v", got)
	}
}

func TestAWriteNeedsTheApprovalItsBindingAsksFor(t *testing.T) {
	s := parseFixture(t, "triage-with-approval.yaml")
	// Route straight to the write, past the approval.
	s.Nodes[1].Cases["review"] = "request"
	got := codes(s.CheckAgainst(exciter))
	if got["/nodes/3"] != CodeNeedsApproval {
		t.Fatalf("a write reachable without approval = %v", got)
	}
	// A tool not connected: unknown calls count as writes, so the rule still holds.
	unknown := catalog{"exciter": {Scopes: []string{"mcp", "mcp:read"}}}
	if got := codes(s.CheckAgainst(unknown)); got["/nodes/3"] != CodeNeedsApproval {
		t.Errorf("with the calls unknown = %v", got)
	}
}
