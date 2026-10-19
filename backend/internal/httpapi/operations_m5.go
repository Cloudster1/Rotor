package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"time"

	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// The rows of M5, approvals; docs/api-contract-m5.md says what they mean.
func init() {
	operations = append(operations, m5Operations...)
}

var m5Operations = []operation{
	// Approvals: a person decides before a run goes on (#31).
	{method: "GET", path: "/approvals", handler: "handleListApprovals", tag: "approvals", pending: true,
		summary: "Approvals you may see: those you may decide, and those of runs you may see; newest first.",
		query: []param{
			{name: "status", description: "pending (the default), decided or all."},
			{name: "waitingOnMe", description: "true lists only the pending approvals you may decide."},
		},
		responses: ok(env{"approvals": []approvalSummary{}})},
	{method: "GET", path: "/approvals/count", handler: "handleCountApprovals", tag: "approvals", pending: true,
		summary: "How many approvals wait for you to decide them, for the badge.", responses: ok(approvalCount{})},
	{method: "GET", path: "/approvals/{approvalID}", handler: "handleGetApproval", tag: "approvals", pending: true,
		summary:   "One approval, with what the run is about to write and the fields an approver may change.",
		responses: map[int]any{http.StatusOK: approvalResponse{}, http.StatusNotFound: notFound}},
	{method: "POST", path: "/approvals/{approvalID}/decision", handler: "handleDecideApproval", tag: "approvals", pending: true,
		summary: "Approve or reject, with a comment and, when approving, changed values.",
		description: "Refused with 403 not_approver when you may see the approval but not decide it, 409 approval_decided once it is decided " +
			"or expired, and 422 with a field per value that does not fit the form.",
		request: decisionRequest{},
		responses: map[int]any{http.StatusOK: approvalResponse{}, http.StatusForbidden: errorEnvelope{}, http.StatusNotFound: notFound,
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

// approvalStatus is where an approval is: pending until decided, expired or its run ends.
type approvalStatus string

// approvalDecision is what an approver answers.
type approvalDecision string

// m5Enums gives the builder the values each M5 string type takes.
var m5Enums = map[reflect.Type][]string{
	reflect.TypeOf(approvalStatus("")):   {"pending", "approved", "rejected", "expired", "cancelled"},
	reflect.TypeOf(approvalDecision("")): {"approve", "reject"},
}

// registerM5Schemas tells the builder about the M5 enums.
func registerM5Schemas(b *openapi.Builder) {
	for t, values := range m5Enums {
		b.Enums[t] = values
	}
}

// approvalRun is the run an approval belongs to.
type approvalRun struct {
	ID        string    `json:"id"`
	Workflow  personRef `json:"workflow"`
	StartedBy personRef `json:"startedBy"`
}

// approvers says who decides: the group the node names, or the person who started the run when it names none.
type approvers struct {
	Group *string `json:"group"`
}

// approvalSummary is an approval in the inbox.
type approvalSummary struct {
	ID          string         `json:"id"`
	Run         approvalRun    `json:"run"`
	Node        string         `json:"node"`
	Message     string         `json:"message"`
	Status      approvalStatus `json:"status"`
	Approvers   approvers      `json:"approvers"`
	CanDecide   bool           `json:"canDecide"`
	RequestedAt time.Time      `json:"requestedAt"`
	ExpiresAt   time.Time      `json:"expiresAt"`
	DecidedAt   *time.Time     `json:"decidedAt"`
}

// previewWrite is one write the run may make after the approval: a tool node's call with its arguments, or an
// agent's or code node's write binding with the calls it allows and no arguments, since those choose their own.
type previewWrite struct {
	Node      string          `json:"node"`
	Binding   string          `json:"binding"`
	Tool      string          `json:"tool"`
	Call      string          `json:"call,omitempty"`
	Calls     []string        `json:"calls,omitempty"`
	Arguments json.RawMessage `json:"arguments"`
}

// approvalForm is what an approver may change: a JSON Schema and the values the run proposes.
type approvalForm struct {
	Schema json.RawMessage `json:"schema"`
	Values json.RawMessage `json:"values"`
}

// approvalDecided is a decision as it was taken; values are the form's after the approver's changes.
type approvalDecided struct {
	Decision approvalDecision `json:"decision"`
	By       personRef        `json:"by"`
	Comment  string           `json:"comment"`
	Values   json.RawMessage  `json:"values"`
	At       time.Time        `json:"at"`
}

// approvalResponse is one approval with its preview, its form and, once taken, its decision.
type approvalResponse struct {
	approvalSummary
	Preview  []previewWrite   `json:"preview"`
	Form     *approvalForm    `json:"form"`
	Decision *approvalDecided `json:"decision"`
}

// approvalCount is the badge's number.
type approvalCount struct {
	WaitingOnMe int `json:"waitingOnMe"`
}

// decisionRequest is an approver's answer; values are only read when approving.
type decisionRequest struct {
	Decision approvalDecision `json:"decision"`
	Comment  string           `json:"comment,omitempty"`
	Values   json.RawMessage  `json:"values,omitempty"`
}
