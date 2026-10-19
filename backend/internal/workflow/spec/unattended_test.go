package spec

import "testing"

func TestATriggerNeedsEveryWriteBehindAnApproval(t *testing.T) {
	for _, name := range []string{"draft-requirements.yaml", "transform-with-code.yaml"} {
		if problems, known := parseFixture(t, name).UnattendedWrites(exciter); len(problems) > 0 || !known {
			t.Errorf("%s: %+v (known %v)", name, problems, known)
		}
	}

	// Without the binding asking for it, the approval still has to come first.
	s := parseFixture(t, "draft-requirements.yaml")
	write := s.Tools["exciter-write"]
	write.Approval = ""
	s.Tools["exciter-write"] = write
	if problems, _ := s.UnattendedWrites(exciter); len(problems) > 0 {
		t.Errorf("a write after the approval = %+v", problems)
	}
	s.Edges = []Edge{{From: "draft", To: "file"}, {From: "file", To: "review"}}
	if got, _ := s.UnattendedWrites(exciter); codes(got)["/nodes/2"] != CodeUnattended {
		t.Errorf("a write before the approval = %+v", got)
	}

	// A read binding cannot write, whatever its calls; a tool that does not say what its calls do is not known.
	if problems, known := s.UnattendedWrites(catalog{"exciter": {Scopes: []string{"mcp"}}}); known || len(problems) == 0 {
		t.Errorf("with the calls unknown = %+v, known %v", problems, known)
	}
}
