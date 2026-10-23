package spec

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtures is api/workflow-fixtures, which the runner's tests read too: both must judge each file alike.
var fixtures = filepath.Join("..", "..", "..", "..", "api", "workflow-fixtures")

// expected reads a fixture's "# expect: <path> <code>" lines; a path of - is the document itself.
func expected(t *testing.T, source []byte) [][2]string {
	t.Helper()
	var out [][2]string
	for _, line := range strings.Split(string(source), "\n") {
		rest, ok := strings.CutPrefix(line, "# expect: ")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) != 2 {
			t.Fatalf("a malformed expectation %q", line)
		}
		if fields[0] == "-" {
			fields[0] = ""
		}
		out = append(out, [2]string{fields[0], fields[1]})
	}
	return out
}

func TestTheValidFixturesHaveNoProblems(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtures, "valid", "*.yaml"))
	if len(files) == 0 {
		t.Fatal("no valid fixtures found")
	}
	for _, file := range files {
		source, _ := os.ReadFile(file)
		if s, problems := Parse(source); s == nil || len(problems) > 0 {
			t.Errorf("%s: %+v", filepath.Base(file), problems)
		}
	}
}

// examples is the workflows people start from; each must be one the control plane saves.
var examples = filepath.Join("..", "..", "..", "..", "examples")

func TestTheExamplesHaveNoProblems(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(examples, "*.yaml"))
	if len(files) == 0 {
		t.Fatal("no examples found")
	}
	for _, file := range files {
		source, _ := os.ReadFile(file)
		if s, problems := Parse(source); s == nil || len(problems) > 0 {
			t.Errorf("%s: %+v", filepath.Base(file), problems)
		}
	}
}

func TestEachInvalidFixtureNamesItsProblemWhereItIs(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtures, "invalid", "*.yaml"))
	if len(files) == 0 {
		t.Fatal("no invalid fixtures found")
	}
	for _, file := range files {
		source, _ := os.ReadFile(file)
		_, problems := Parse(source)
		for _, want := range expected(t, source) {
			found := false
			for _, p := range problems {
				if p.Path == want[0] && p.Code == want[1] {
					found = true
					if p.Message == "" || !strings.HasSuffix(p.Message, ".") {
						t.Errorf("%s: %q is not a sentence", filepath.Base(file), p.Message)
					}
					if want[1] != CodeYAML && want[0] != "" && p.Line == 0 {
						t.Errorf("%s: %s is not placed on a line", filepath.Base(file), want[0])
					}
				}
			}
			if !found {
				t.Errorf("%s: want %s at %q, got %+v", filepath.Base(file), want[1], want[0], problems)
			}
		}
	}
}

func TestAProblemIsPlacedOnItsLineAndColumn(t *testing.T) {
	source, _ := os.ReadFile(filepath.Join(fixtures, "invalid", "call-not-allowed.yaml"))
	_, problems := Parse(source)
	if len(problems) != 1 {
		t.Fatalf("problems = %+v", problems)
	}
	// The call is on the last line, after "  - {id: a, type: tool, tool: t, ".
	lines := strings.Split(strings.TrimRight(string(source), "\n"), "\n")
	if problems[0].Line != len(lines) || problems[0].Column != strings.Index(lines[len(lines)-1], "call")+1 {
		t.Errorf("placed at %d:%d, want %d:%d", problems[0].Line, problems[0].Column, len(lines), strings.Index(lines[len(lines)-1], "call")+1)
	}
}

func TestAWorkflowTooLargeIsRefusedBeforeReading(t *testing.T) {
	_, problems := Parse(bytes.Repeat([]byte("#"), 300<<10))
	if len(problems) != 1 || problems[0].Code != CodeTooLarge {
		t.Fatalf("problems = %+v", problems)
	}
}

// The schema served and embedded is the one in api/, which editors and the runner read.
func TestTheEmbeddedSchemaIsTheOneInAPI(t *testing.T) {
	published, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "api", "workflow.v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(published, Schema()) {
		t.Fatal("backend/internal/workflow/spec/workflow.v1.schema.json differs from api/; run make workflow-schema")
	}
}

// Numbers keep their digits on the way from YAML to the spec, so a large whole number still reads as one.
func TestLargeWholeNumbersReadIntoIntegerFields(t *testing.T) {
	s, problems := Parse([]byte(`apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: large}
limits: {maxSteps: 40, timeout: 10m, maxTokens: 6000000, maxCost: 2.5}
entry: done
nodes:
  - {id: done, type: code, output: {schema: {type: object}}, source: "def run(state, ctx):\n    return {}\n"}
`))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if s.Limits.MaxTokens != 6000000 || s.Limits.MaxCost != 2.5 {
		t.Errorf("the limits read as %+v", s.Limits)
	}
}

func TestTheSpecIsReadIntoItsTypes(t *testing.T) {
	source, _ := os.ReadFile(filepath.Join(fixtures, "valid", "triage-with-approval.yaml"))
	s, problems := Parse(source)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if s.Metadata.Name != "triage" || len(s.Nodes) != 5 || s.Nodes[1].Cases["review"] != "confirm" ||
		!s.Tools["exciter-write"].Writes() || s.Tools["exciter-read"].Writes() || s.Limits.TimeoutDuration().Minutes() != 10 {
		t.Errorf("read %+v", s)
	}
}
