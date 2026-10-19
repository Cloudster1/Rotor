// Package spec reads a workflow: YAML checked against api/workflow.v1.schema.json and against its own sense,
// with every problem placed by a JSON pointer, a line and a column. docs/workflows.md describes the format.
package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Problem codes, as the API and the runner name them.
const (
	CodeYAML          = "yaml"
	CodeTooLarge      = "too_large"
	CodeSchema        = "schema"
	CodeDuplicateID   = "duplicate_id"
	CodeUnknownNode   = "unknown_node"
	CodeUnreachable   = "unreachable"
	CodeRouterEdges   = "router_edges"
	CodeUnknownBind   = "unknown_binding"
	CodeCallNotAllow  = "call_not_allowed"
	CodeUnknownTool   = "unknown_tool"
	CodeUnknownScope  = "unknown_scope"
	CodeUnknownCall   = "unknown_call"
	CodeWritesOnRead  = "writes_on_read_binding"
	CodeNeedsApproval = "approval_missing"
	CodeCodeLimits    = "code_limits"
	// Approval nodes (#31): a form an approver cannot fill, an expiry past the longest wait, a review decided by a tool.
	CodeApprovalForm   = "approval_form"
	CodeApprovalExpiry = "approval_expiry"
	CodeDecidesReview  = "decides_review"
)

// Node types.
const (
	TypeAgent    = "agent"
	TypeTool     = "tool"
	TypeCode     = "code"
	TypeApproval = "approval"
	TypeRouter   = "router"
)

// Binding access and approval values.
const (
	AccessRead          = "read"
	AccessWrite         = "write"
	ApprovalBeforeWrite = "beforeWrite"
)

// Problem is one thing wrong with a workflow and where: a JSON pointer, and a line and column from 1 (0 when the
// place is not in the text, such as a missing field of the document itself).
type Problem struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Spec is a workflow, version 1.
type Spec struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   Metadata           `json:"metadata"`
	Inputs     json.RawMessage    `json:"inputs,omitempty"`
	Tools      map[string]Binding `json:"tools,omitempty"`
	Model      *Model             `json:"model,omitempty"`
	Limits     Limits             `json:"limits"`
	Entry      string             `json:"entry"`
	Nodes      []Node             `json:"nodes"`
	Edges      []Edge             `json:"edges,omitempty"`

	// places finds where a problem is written, for the checks that run after parsing.
	places *places
}

// Metadata names a workflow.
type Metadata struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Binding is a tool a run may use, under the name its nodes refer to.
type Binding struct {
	Tool       string   `json:"tool"`
	Scopes     []string `json:"scopes"`
	AllowTools []string `json:"allowTools"`
	Access     string   `json:"access,omitempty"`
	Approval   string   `json:"approval,omitempty"`
}

// Writes says whether the binding may write; read is the default.
func (b Binding) Writes() bool { return b.Access == AccessWrite }

// Model names the language model agent nodes use.
type Model struct {
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
}

// Limits bound a run.
type Limits struct {
	MaxSteps  int    `json:"maxSteps"`
	Timeout   string `json:"timeout"`
	MaxTokens int    `json:"maxTokens,omitempty"`
}

// TimeoutDuration is Timeout as a duration; the schema has checked its shape.
func (l Limits) TimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(l.Timeout)
	return d
}

// Node is one step; which fields it has depends on its type.
type Node struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Prompt    string            `json:"prompt,omitempty"`
	Tools     []string          `json:"tools,omitempty"`
	Output    *Output           `json:"output,omitempty"`
	Tool      string            `json:"tool,omitempty"`
	Call      string            `json:"call,omitempty"`
	Arguments json.RawMessage   `json:"arguments,omitempty"`
	Source    string            `json:"source,omitempty"`
	Timeout   string            `json:"timeout,omitempty"`
	Memory    string            `json:"memory,omitempty"`
	Message   string            `json:"message,omitempty"`
	Approvers *Approvers        `json:"approvers,omitempty"`
	ExpiresIn string            `json:"expiresIn,omitempty"`
	Form      *Form             `json:"form,omitempty"`
	On        string            `json:"on,omitempty"`
	Cases     map[string]string `json:"cases,omitempty"`
	Default   string            `json:"default,omitempty"`
}

// Approvers names the group whose members decide an approval; without it the run's starter decides.
type Approvers struct {
	Group string `json:"group"`
}

// Form is what an approver may change: a JSON Schema object and the values the run proposes, as templates.
type Form struct {
	Schema json.RawMessage `json:"schema"`
	Values json.RawMessage `json:"values,omitempty"`
}

// Output declares the shape of a node's output.
type Output struct {
	Schema json.RawMessage `json:"schema"`
}

// Edge leads from one node to the next.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Parse reads a workflow and lists everything wrong with it; the spec is nil when it could not be read at all.
func Parse(source []byte) (*Spec, []Problem) {
	if len(source) > config.MaxSpecBytes {
		return nil, []Problem{{Code: CodeTooLarge, Message: fmt.Sprintf("The workflow is %d bytes; keep it under %d.", len(source), config.MaxSpecBytes)}}
	}
	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return nil, []Problem{yamlProblem(err)}
	}
	value, err := toJSON(&root)
	if err != nil {
		return nil, []Problem{yamlProblem(err)}
	}
	document, err := json.Marshal(value)
	if err != nil {
		return nil, []Problem{yamlProblem(err)}
	}
	places := newPlaces(&root)
	if problems := validateSchema(document, places); len(problems) > 0 {
		return nil, problems
	}
	var s Spec
	if err := json.Unmarshal(document, &s); err != nil {
		return nil, []Problem{{Code: CodeSchema, Message: "The workflow does not have the shape of version 1: " + err.Error() + "."}}
	}
	s.places = places
	return &s, places.place(checkSense(&s))
}

// errorLine finds the line a YAML error names, as "yaml: line 3: ..." or "line 3: ...".
var errorLine = regexp.MustCompile(`line (\d+)`)

// yamlProblem places a parse error on the line the parser names.
func yamlProblem(err error) Problem {
	text := strings.TrimPrefix(err.Error(), "yaml: ")
	p := Problem{Code: CodeYAML, Message: "The YAML cannot be read: " + firstLine(text)}
	if m := errorLine.FindStringSubmatch(text); m != nil {
		p.Line, _ = strconv.Atoi(m[1])
		p.Column = 1
	}
	return p
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return sentence(s)
}

// sentence ends a library's message as a sentence, starting with a capital.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
