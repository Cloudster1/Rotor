package spec

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// SchemaURL is the schema's id; the copy beside this file is api/workflow.v1.schema.json, kept so by a test.
const SchemaURL = "https://rotor.dev/schemas/workflow.v1.json"

//go:embed workflow.v1.schema.json
var schemaDocument []byte

// Schema is the JSON Schema of version 1, as the API serves it.
func Schema() []byte { return schemaDocument }

var (
	compileOnce sync.Once
	compiled    *jsonschema.Schema
	compileErr  error
	printer     = message.NewPrinter(language.English)
)

func schema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaDocument))
		if err != nil {
			compileErr = err
			return
		}
		c := jsonschema.NewCompiler()
		if compileErr = c.AddResource(SchemaURL, doc); compileErr == nil {
			compiled, compileErr = c.Compile(SchemaURL)
		}
	})
	return compiled, compileErr
}

// validateSchema checks a document against the schema, answering the innermost reasons, one per place and kind.
func validateSchema(document []byte, at *places) []Problem {
	sch, err := schema()
	if err != nil {
		return []Problem{{Code: CodeSchema, Message: "Rotor's workflow schema does not compile: " + err.Error()}}
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(document))
	if err != nil {
		return []Problem{{Code: CodeYAML, Message: "The YAML is not a document Rotor can read: " + err.Error()}}
	}
	err = sch.Validate(instance)
	var invalid *jsonschema.ValidationError
	if !errors.As(err, &invalid) {
		if err != nil {
			return []Problem{{Code: CodeSchema, Message: sentence(err.Error())}}
		}
		return nil
	}
	seen := map[string]bool{}
	var out []Problem
	for _, leaf := range leaves(invalid) {
		pointer := pointerOf(leaf.InstanceLocation)
		text := leaf.ErrorKind.LocalizedString(printer)
		if seen[pointer+text] {
			continue
		}
		seen[pointer+text] = true
		line, column := at.at(pointer)
		where := pointer
		if where == "" {
			where = "the top level"
		}
		out = append(out, Problem{Path: pointer, Line: line, Column: column, Code: CodeSchema,
			Message: fmt.Sprintf("At %s: %s", where, sentence(text))})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// leaves are the errors without causes: what is actually wrong, beneath the schema's grouping.
func leaves(e *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(e.Causes) == 0 {
		return []*jsonschema.ValidationError{e}
	}
	var out []*jsonschema.ValidationError
	for _, cause := range e.Causes {
		out = append(out, leaves(cause)...)
	}
	return out
}

func pointerOf(location []string) string {
	if len(location) == 0 {
		return ""
	}
	escaped := make([]string, len(location))
	for i, part := range location {
		escaped[i] = strings.NewReplacer("~", "~0", "/", "~1").Replace(part)
	}
	return "/" + strings.Join(escaped, "/")
}
