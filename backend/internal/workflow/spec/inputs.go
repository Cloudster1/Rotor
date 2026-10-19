package spec

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// CodeInputs marks inputs a run starts with that the workflow's inputs schema refuses.
const CodeInputs = "inputs"

// CodeValues marks values an approver sends that the approval's form refuses.
const CodeValues = "values"

// carriedURL names a schema a workflow carries while it compiles; nothing is fetched from it.
const carriedURL = "https://rotor.dev/schemas/carried.json"

// compileSchema compiles a JSON Schema a workflow carries, such as its inputs or an approval's form.
func compileSchema(raw []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(carriedURL, doc); err != nil {
		return nil, err
	}
	return c.Compile(carriedURL)
}

// CheckInputs checks what a run starts with against the workflow's inputs; no schema takes any object.
func (s *Spec) CheckInputs(inputs []byte) []Problem {
	return checkObject(s.Inputs, inputs, CodeInputs, "the inputs")
}

// CheckValues checks the values an approver sends, already merged over the proposed ones, against the form.
func (f *Form) CheckValues(values []byte) []Problem {
	return checkObject(f.Schema, values, CodeValues, "the values")
}

// checkObject checks a JSON object against a schema a workflow carries, each problem placed by a JSON pointer.
func checkObject(schema, raw []byte, code, what string) []Problem {
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return []Problem{{Code: code, Message: fmt.Sprintf("%s are not JSON. Send them as a JSON object.", capital(what))}}
	}
	if _, ok := instance.(map[string]any); !ok {
		return []Problem{{Code: code, Message: fmt.Sprintf("%s are not an object. Send them as a JSON object, field by field.", capital(what))}}
	}
	if len(schema) == 0 {
		return nil
	}
	sch, err := compileSchema(schema)
	if err != nil {
		return []Problem{{Code: code, Message: "The workflow's schema for " + what + " does not compile; its owner must fix it."}}
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
			where = what
		}
		out = append(out, Problem{Path: pointer, Code: code, Message: fmt.Sprintf("At %s: %s", where, text)})
	}
	return out
}

func capital(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
