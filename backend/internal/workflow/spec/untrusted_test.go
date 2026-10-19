package spec

import "testing"

func TestAWriteBehindAnApprovalAfterAReadIsNotWarnedAbout(t *testing.T) {
	s := parseFixture(t, "triage-with-approval.yaml")
	if got := s.Warnings(exciter); len(got) > 0 {
		t.Errorf("the approved write is warned about: %+v", got)
	}
	for _, name := range []string{"draft-requirements.yaml", "transform-with-code.yaml"} {
		if got := parseFixture(t, name).Warnings(exciter); len(got) > 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestAWriteReachedFromAReadWithoutAnApprovalIsWarnedAbout(t *testing.T) {
	s := parseFixture(t, "triage-with-approval.yaml")
	// Route straight to the write, past the approval.
	s.Nodes[1].Cases["review"] = "request"
	got := codes(s.Warnings(exciter))
	if got["/nodes/3"] != CodeUntrustedWrite || len(got) != 1 {
		t.Fatalf("a write after a read with no approval = %v", got)
	}
	// Unknown calls count as writes, so a tool not connected still warns.
	if got := codes(s.Warnings(nil)); got["/nodes/3"] != CodeUntrustedWrite {
		t.Errorf("with no catalog = %v", got)
	}
}

func TestAnAgentThatReadsAndWritesInOneStepIsWarnedAbout(t *testing.T) {
	s := parseFixture(t, "triage-with-approval.yaml")
	s.Nodes[0].Tools = append(s.Nodes[0].Tools, "exciter-write")
	got := codes(s.Warnings(exciter))
	if got["/nodes/0"] != CodeUntrustedWrite {
		t.Fatalf("an agent reading and writing = %v", got)
	}
	// Through a read binding it never writes, so nothing is said.
	s.Nodes[0].Tools = []string{"exciter-read"}
	if got := s.Warnings(exciter); len(got) > 0 {
		t.Errorf("a reading agent is warned about: %+v", got)
	}
}

func TestAWarningIsPlacedAndSavingIsNotRefused(t *testing.T) {
	source := []byte(`apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: steered}
tools:
  r: {tool: exciter, scopes: [mcp:read], allowTools: [get_requirement]}
  w: {tool: exciter, scopes: [mcp], access: write, allowTools: [create_requirement]}
limits: {maxSteps: 10, timeout: 1m}
entry: read
nodes:
  - {id: read, type: tool, tool: r, call: get_requirement, arguments: {key: R-1}}
  - {id: write, type: tool, tool: w, call: create_requirement, arguments: {title: "{{ nodes.read.title }}"}}
edges:
  - {from: read, to: write}
`)
	s, problems := Parse(source)
	if len(problems) > 0 || len(s.CheckAgainst(exciter)) > 0 {
		t.Fatalf("the workflow has problems: %v %v", problems, s.CheckAgainst(exciter))
	}
	got := s.Warnings(exciter)
	if len(got) != 1 || got[0].Code != CodeUntrustedWrite || got[0].Line == 0 {
		t.Errorf("warnings = %+v", got)
	}
}
