package spec

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// CodeInputs marks inputs a run starts with that the workflow's inputs schema refuses.
const CodeInputs = "inputs"

// inputsURL names a workflow's inputs schema while it compiles; nothing is fetched from it.
const inputsURL = "https://rotor.dev/schemas/inputs.json"

// CheckInputs checks what a run starts with against the workflow's inputs; no schema takes any object.
func (s *Spec) CheckInputs(inputs []byte) []Problem {
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(inputs))
	if err != nil {
		return []Problem{{Code: CodeInputs, Message: "The inputs are not JSON. Send them as a JSON object."}}
	}
	if _, ok := instance.(map[string]any); !ok {
		return []Problem{{Code: CodeInputs, Message: "The inputs are not an object. Send them as a JSON object, field by field."}}
	}
	if len(s.Inputs) == 0 {
		return nil
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(s.Inputs))
	if err != nil {
		return []Problem{{Code: CodeInputs, Message: "The workflow's inputs schema is not JSON; its owner must fix it."}}
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(inputsURL, doc); err != nil {
		return []Problem{{Code: CodeInputs, Message: "The workflow's inputs schema does not compile; its owner must fix it."}}
	}
	sch, err := c.Compile(inputsURL)
	if err != nil {
		return []Problem{{Code: CodeInputs, Message: "The workflow's inputs schema does not compile; its owner must fix it."}}
	}
	var invalid *jsonschema.ValidationError
	if err := sch.Validate(instance); !errors.As(err, &invalid) {
		return nil
	}
	var out []Problem
	seen := map[string]bool{}
	for _, leaf := range leaves(invalid) {
		pointer := pointerOf(leaf.InstanceLocation)
		text := sentence(leaf.ErrorKind.LocalizedString(printer))
		if seen[pointer+text] {
			continue
		}
		seen[pointer+text] = true
		where := pointer
		if where == "" {
			where = "the inputs"
		}
		out = append(out, Problem{Path: pointer, Code: CodeInputs, Message: fmt.Sprintf("At %s: %s", where, text)})
	}
	return out
}
