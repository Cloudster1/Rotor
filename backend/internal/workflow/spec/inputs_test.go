package spec

import (
	"strings"
	"testing"
)

func TestCheckInputsHoldsARunToTheWorkflowsInputs(t *testing.T) {
	s := &Spec{Inputs: []byte(`{"type": "object", "required": ["topic"], "properties": {"topic": {"type": "string", "minLength": 1}}}`)}
	cases := []struct {
		name, inputs, path string
	}{
		{"fits", `{"topic": "billing"}`, ""},
		{"missing", `{}`, "/"},
		{"empty", `{"topic": ""}`, "/topic"},
		{"not an object", `["topic"]`, "!"},
		{"not JSON", `{topic`, "!"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			problems := s.CheckInputs([]byte(c.inputs))
			switch {
			case c.path == "" && len(problems) != 0:
				t.Errorf("fitting inputs were refused: %+v", problems)
			case c.path == "!" && (len(problems) != 1 || !strings.Contains(problems[0].Message, "JSON object")):
				t.Errorf("want one sentence asking for a JSON object, got %+v", problems)
			case c.path == "/" && (len(problems) == 0 || problems[0].Path != ""):
				t.Errorf("a missing field is refused at the top: %+v", problems)
			case c.path == "/topic" && (len(problems) == 0 || problems[0].Path != "/topic"):
				t.Errorf("an empty topic is refused at /topic: %+v", problems)
			}
			for _, p := range problems {
				if p.Code != CodeInputs || !strings.HasSuffix(p.Message, ".") {
					t.Errorf("a problem is not a sentence of code inputs: %+v", p)
				}
			}
		})
	}
	if problems := (&Spec{}).CheckInputs([]byte(`{"anything": 1}`)); len(problems) != 0 {
		t.Errorf("a workflow without inputs refused an object: %+v", problems)
	}
}
