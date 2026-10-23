package spec

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// places finds where a JSON pointer's value is written in the YAML.
type places struct {
	root *yaml.Node
}

func newPlaces(document *yaml.Node) *places {
	root := document
	if root != nil && root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	return &places{root: root}
}

// at is the line and column of the value at pointer, or of the nearest enclosing value that exists; a mapping's
// key stands for its value, as an editor would underline it.
func (p *places) at(pointer string) (int, int) {
	node := p.root
	if node == nil {
		return 0, 0
	}
	line, column := node.Line, node.Column
	for _, segment := range splitPointer(pointer) {
		next, at := child(resolve(node), segment)
		if next == nil {
			break
		}
		node, line, column = next, at.Line, at.Column
	}
	return line, column
}

// place fills in where each problem is written.
func (p *places) place(problems []Problem) []Problem {
	for i := range problems {
		problems[i].Line, problems[i].Column = p.at(problems[i].Path)
	}
	return problems
}

func splitPointer(pointer string) []string {
	if pointer == "" || pointer == "/" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for i, part := range parts {
		parts[i] = strings.NewReplacer("~1", "/", "~0", "~").Replace(part)
	}
	return parts
}

// child descends one segment, answering the value and the node that marks where it is written.
func child(node *yaml.Node, segment string) (*yaml.Node, *yaml.Node) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == segment {
				return node.Content[i+1], node.Content[i]
			}
		}
	case yaml.SequenceNode:
		i, err := strconv.Atoi(segment)
		if err == nil && i >= 0 && i < len(node.Content) {
			return node.Content[i], node.Content[i]
		}
	}
	return nil, nil
}

// resolve looks through an alias to the value it names.
func resolve(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node
}

// toJSON turns a YAML 1.2 node tree into the JSON it means, scalar by scalar from its resolved tag; a timestamp
// stays text, as the schema expects strings.
func toJSON(node *yaml.Node) (any, error) {
	node = resolve(node)
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return toJSON(node.Content[0])
	case yaml.MappingNode:
		out := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := resolve(node.Content[i])
			if key.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("line %d: a key must be text", key.Line)
			}
			value, err := toJSON(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[key.Value] = value
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(node.Content))
		for _, item := range node.Content {
			value, err := toJSON(item)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	case yaml.ScalarNode:
		return scalar(node)
	}
	return nil, fmt.Errorf("line %d: a value Rotor cannot read", node.Line)
}

func scalar(node *yaml.Node) (any, error) {
	switch node.ShortTag() {
	case "!!null":
		return nil, nil
	case "!!bool":
		var b bool
		err := node.Decode(&b)
		return b, err
	case "!!int", "!!float":
		var f json.Number
		var n float64
		if err := node.Decode(&n); err != nil {
			return nil, fmt.Errorf("line %d: %q is not a number JSON can hold", node.Line, node.Value)
		}
		// Without an exponent, so a whole number such as a maxTokens of 6000000 still reads into an integer field.
		f = json.Number(strconv.FormatFloat(n, 'f', -1, 64))
		return f, nil
	}
	return node.Value, nil
}
