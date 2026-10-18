package spec

import (
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// places finds where a JSON pointer's value is written in the YAML.
type places struct {
	root ast.Node
}

func newPlaces(file *ast.File) *places {
	p := &places{}
	if len(file.Docs) > 0 {
		p.root = file.Docs[0].Body
	}
	return p
}

// at is the line and column of the value at pointer, or of the nearest enclosing value that exists.
func (p *places) at(pointer string) (int, int) {
	node, line, column := p.root, 0, 0
	if node != nil {
		line, column = position(node)
	}
	for _, segment := range splitPointer(pointer) {
		next, keyLine, keyColumn := child(node, segment)
		if next == nil {
			break
		}
		node = next
		line, column = keyLine, keyColumn
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

// child descends one segment, answering where a mapping's key or a sequence's item is written.
func child(node ast.Node, segment string) (ast.Node, int, int) {
	switch n := unwrap(node).(type) {
	case *ast.MappingNode:
		for _, pair := range n.Values {
			if next, line, column, ok := matchPair(pair, segment); ok {
				return next, line, column
			}
		}
	case *ast.MappingValueNode:
		if next, line, column, ok := matchPair(n, segment); ok {
			return next, line, column
		}
	case *ast.SequenceNode:
		i, err := strconv.Atoi(segment)
		if err == nil && i >= 0 && i < len(n.Values) {
			line, column := position(n.Values[i])
			return n.Values[i], line, column
		}
	}
	return nil, 0, 0
}

func matchPair(pair *ast.MappingValueNode, segment string) (ast.Node, int, int, bool) {
	if pair == nil || pair.Key == nil || strings.Trim(pair.Key.String(), `"'`) != segment {
		return nil, 0, 0, false
	}
	line, column := position(pair.Key)
	return pair.Value, line, column, true
}

// unwrap looks through tags and anchors to the value they mark.
func unwrap(node ast.Node) ast.Node {
	for {
		switch n := node.(type) {
		case *ast.TagNode:
			node = n.Value
		case *ast.AnchorNode:
			node = n.Value
		default:
			return node
		}
	}
}

func position(node ast.Node) (int, int) {
	if node == nil || node.GetToken() == nil || node.GetToken().Position == nil {
		return 0, 0
	}
	at := node.GetToken().Position
	return at.Line, at.Column
}
