package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/config"
)

func specialists(t *testing.T) *Spec {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(fixtures, "valid", "specialist-agents.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s, problems := Parse(source)
	if s == nil || len(problems) > 0 {
		t.Fatalf("the fixture does not read: %+v", problems)
	}
	return s
}

func TestAnAgentKeepsItsOwnModelRoleAndSteps(t *testing.T) {
	s := specialists(t)
	security := s.Nodes[1]
	if security.Model == nil || security.Model.Name != "claude-opus-5-5" || security.Model.Provider != "anthropic" {
		t.Errorf("the security reviewer's model is %+v", security.Model)
	}
	if security.Role == "" || security.MaxSteps != 40 {
		t.Errorf("the security reviewer is %+v", security)
	}
	if got := s.AgentModels(); len(got) != 1 || got["security"].Name != "claude-opus-5-5" {
		t.Errorf("the agents' own models are %+v", got)
	}
	if !s.UsesWorkflowModel() {
		t.Error("the writer has no model of its own, so the workflow's is used")
	}
}

func TestAnAgentsOwnModelIsOneAnAdministratorAllows(t *testing.T) {
	s := specialists(t)
	if problems := s.CheckModels(func(Model) bool { return true }); len(problems) != 0 {
		t.Fatalf("an allowed model is refused: %+v", problems)
	}
	var asked []Model
	problems := s.CheckModels(func(m Model) bool { asked = append(asked, m); return false })
	if len(asked) != 1 || asked[0].Name != "claude-opus-5-5" {
		t.Errorf("only the agent's own model is checked, not the workflow's: %+v", asked)
	}
	if len(problems) != 1 || problems[0].Code != CodeModelNotAllowed || problems[0].Path != "/nodes/1/model" || problems[0].Line == 0 {
		t.Fatalf("the refusal is %+v", problems)
	}
}

func TestTheSchemaBoundsARoleAsTheConfigDoes(t *testing.T) {
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				MaxLength int `json:"maxLength"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaDocument, &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Defs["agentNode"].Properties["role"].MaxLength; got != config.MaxRoleChars {
		t.Errorf("the schema bounds a role at %d characters, the config at %d", got, config.MaxRoleChars)
	}
}

func TestAnAgentMayTakeAsManyStepsAsTheWorkflow(t *testing.T) {
	source := []byte(`apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: steps}
limits: {maxSteps: 20, timeout: 5m}
entry: a
nodes:
  - {id: a, type: agent, maxSteps: 20, role: "You count {braces} but hold no template.", prompt: hi}
`)
	if s, problems := Parse(source); s == nil || len(problems) > 0 {
		t.Fatalf("an agent at the workflow's steps is refused: %+v", problems)
	}
}
