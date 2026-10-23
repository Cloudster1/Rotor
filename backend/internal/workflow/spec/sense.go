package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// templateRef finds what a {{ ... }} in an argument names; the schema checks a router's on.
var templateRef = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// checkSense finds what the schema cannot: names that point nowhere and nodes nothing leads to.
func checkSense(s *Spec) []Problem {
	var out []Problem
	add := func(path, code, format string, args ...any) {
		out = append(out, Problem{Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
	}
	index := map[string]int{}
	for i, n := range s.Nodes {
		if _, dup := index[n.ID]; dup {
			add(fmt.Sprintf("/nodes/%d/id", i), CodeDuplicateID, "Another node is already called %s; give each node its own id.", n.ID)
			continue
		}
		index[n.ID] = i
	}
	known := func(id string) bool { _, ok := index[id]; return ok }
	if days := s.RetentionDays(); days > config.MaxRunDataRetentionDays {
		add("/retention", CodeRetention, "Keep a run's data for at most %d days; %d is longer than any installation keeps it.",
			config.MaxRunDataRetentionDays, days)
	}
	if !known(s.Entry) {
		add("/entry", CodeUnknownNode, "The entry %s is not a node of this workflow.", s.Entry)
	}
	for i, e := range s.Edges {
		if !known(e.From) {
			add(fmt.Sprintf("/edges/%d/from", i), CodeUnknownNode, "The edge starts at %s, which is not a node.", e.From)
		} else if s.Nodes[index[e.From]].Type == TypeRouter {
			add(fmt.Sprintf("/edges/%d/from", i), CodeRouterEdges, "The router %s chooses its next node by its cases, so it takes no edges.", e.From)
		}
		if !known(e.To) {
			add(fmt.Sprintf("/edges/%d/to", i), CodeUnknownNode, "The edge leads to %s, which is not a node.", e.To)
		}
	}
	for i, n := range s.Nodes {
		base := fmt.Sprintf("/nodes/%d", i)
		switch n.Type {
		case TypeRouter:
			for _, value := range sortedKeys(n.Cases) {
				if !known(n.Cases[value]) {
					add(base+"/cases/"+escape(value), CodeUnknownNode, "The case %s leads to %s, which is not a node.", value, n.Cases[value])
				}
			}
			if n.Default != "" && !known(n.Default) {
				add(base+"/default", CodeUnknownNode, "The default leads to %s, which is not a node.", n.Default)
			}
			if ref := stateNode(n.On); ref != "" && !known(ref) {
				add(base+"/on", CodeUnknownNode, "The router reads the output of %s, which is not a node.", ref)
			}
		case TypeTool:
			b, ok := s.Tools[n.Tool]
			if !ok {
				add(base+"/tool", CodeUnknownBind, "The node calls through %s, which is not one of the workflow's tools.", n.Tool)
			} else if !slices.Contains(b.AllowTools, n.Call) {
				add(base+"/call", CodeCallNotAllow, "%s is not in the allowTools of %s; add it there to call it.", n.Call, n.Tool)
			}
			for _, ref := range argumentRefs(n.Arguments, base+"/arguments") {
				if !known(ref.node) {
					add(ref.path, CodeUnknownNode, "The argument reads the output of %s, which is not a node.", ref.node)
				}
			}
		case TypeApproval:
			out = append(out, approvalSense(n, base, known)...)
		case TypeAgent, TypeCode:
			if n.Type == TypeCode {
				out = append(out, codeLimits(n, base)...)
			} else {
				out = append(out, agentSense(s, n, base)...)
			}
			for j, name := range n.Tools {
				if _, ok := s.Tools[name]; !ok {
					add(fmt.Sprintf("%s/tools/%d", base, j), CodeUnknownBind, "%s is not one of the workflow's tools.", name)
				}
			}
		}
	}
	out = append(out, reviewDeciders(s)...)
	out = append(out, unattendedOutsideAllow(s)...)
	out = append(out, notBuilt(s)...)
	if known(s.Entry) {
		reached := reach(s, index, s.Entry, nil)
		for i, n := range s.Nodes {
			if !reached[n.ID] && index[n.ID] == i {
				add(fmt.Sprintf("/nodes/%d", i), CodeUnreachable, "Nothing leads to %s from the entry %s; connect it or remove it.", n.ID, s.Entry)
			}
		}
	}
	return out
}

// codeLimits refuses a code node asking for more time or memory than the runner gives any node.
func codeLimits(n Node, base string) []Problem {
	var out []Problem
	if d, err := time.ParseDuration(n.Timeout); err == nil && d > config.MaxCodeTimeout {
		out = append(out, Problem{Path: base + "/timeout", Code: CodeCodeLimits,
			Message: fmt.Sprintf("A code node may run for %s at most, not %s.", config.MaxCodeTimeout, n.Timeout)})
	}
	if size := memoryBytes(n.Memory); size > config.MaxCodeMemory {
		out = append(out, Problem{Path: base + "/memory", Code: CodeCodeLimits,
			Message: fmt.Sprintf("A code node may use 1Gi of memory at most, not %s.", n.Memory)})
	}
	return out
}

// memoryBytes reads a size such as 256Mi; the schema has checked its shape.
func memoryBytes(size string) int64 {
	units := map[string]int64{"Mi": 1 << 20, "Gi": 1 << 30}
	if len(size) < 3 {
		return 0
	}
	n, err := strconv.ParseInt(size[:len(size)-2], 10, 64)
	if err != nil {
		return 0
	}
	return n * units[size[len(size)-2:]]
}

// next lists the nodes that may follow a node: its edges, or a router's cases and default.
func next(s *Spec, n Node) []string {
	if n.Type == TypeRouter {
		out := make([]string, 0, len(n.Cases)+1)
		for _, value := range sortedKeys(n.Cases) {
			out = append(out, n.Cases[value])
		}
		if n.Default != "" {
			out = append(out, n.Default)
		}
		return out
	}
	var out []string
	for _, e := range s.Edges {
		if e.From == n.ID {
			out = append(out, e.To)
		}
	}
	return out
}

// reach is every node a run can get to from start; a node stop holds is reached but not passed.
func reach(s *Spec, index map[string]int, start string, stop func(Node) bool) map[string]bool {
	reached := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		i, ok := index[id]
		if !ok || (stop != nil && stop(s.Nodes[i])) {
			continue
		}
		for _, to := range next(s, s.Nodes[i]) {
			if !reached[to] {
				reached[to] = true
				queue = append(queue, to)
			}
		}
	}
	return reached
}

// stateNode is the node a state path such as nodes.draft.title reads, or empty for inputs.
func stateNode(path string) string {
	parts := strings.Split(path, ".")
	if len(parts) >= 2 && parts[0] == "nodes" {
		return parts[1]
	}
	return ""
}

type argumentRef struct{ path, node string }

// argumentRefs lists the nodes the templates in a tool node's arguments read, wherever they are nested.
func argumentRefs(raw json.RawMessage, base string) []argumentRef {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var out []argumentRef
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch v := v.(type) {
		case map[string]any:
			for _, k := range sortedKeys(v) {
				walk(v[k], path+"/"+escape(k))
			}
		case []any:
			for i, item := range v {
				walk(item, fmt.Sprintf("%s/%d", path, i))
			}
		case string:
			for _, m := range templateRef.FindAllStringSubmatch(v, -1) {
				if ref := stateNode(m[1]); ref != "" {
					out = append(out, argumentRef{path: path, node: ref})
				}
			}
		}
	}
	walk(value, base)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func escape(key string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(key) }
