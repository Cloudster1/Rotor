package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"time"

	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// The trigger rows of M6; docs/api-contract-m6.md says what they mean. The hook rows are in operations_m6_hooks.go.
func init() {
	operations = append(operations, m6Operations...)
}

var m6Operations = []operation{
	// Triggers: a person's schedules and tool events that start a workflow as them (#34, #35).
	{method: "GET", path: "/triggers", handler: "handleListTriggers", tag: "triggers",
		summary: "Your triggers, on every workflow; a trigger Rotor turned off says why.", responses: ok(env{"triggers": []triggerResponse{}})},
	{method: "GET", path: "/workflows/{workflowID}/triggers", handler: "handleListWorkflowTriggers", tag: "triggers",
		summary:   "Your triggers on one workflow; an administrator sees everyone's.",
		responses: map[int]any{http.StatusOK: env{"triggers": []triggerResponse{}}, http.StatusNotFound: notFound}},
	{method: "POST", path: "/workflows/{workflowID}/triggers", handler: "handleCreateTrigger", tag: "triggers",
		summary: "Add a trigger that starts the workflow as you, enabled.",
		description: "Refused with 422 unattended_write while a node may write without an approval before it, until writes are confirmed " +
			"at the tool (#32), and with 422 and a field per problem for a schedule, filter or input that does not fit.",
		request: triggerRequest{},
		responses: map[int]any{http.StatusCreated: triggerResponse{}, http.StatusNotFound: notFound, http.StatusConflict: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/triggers/{triggerID}", handler: "handleGetTrigger", tag: "triggers",
		summary: "One of your triggers.", responses: map[int]any{http.StatusOK: triggerResponse{}, http.StatusNotFound: notFound}},
	{method: "PUT", path: "/triggers/{triggerID}", handler: "handleChangeTrigger", tag: "triggers",
		summary: "Change one of your triggers; its kind stays.", request: triggerRequest{},
		responses: map[int]any{http.StatusOK: triggerResponse{}, http.StatusNotFound: notFound, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/triggers/{triggerID}", handler: "handleRemoveTrigger", tag: "triggers",
		summary: "Remove one of your triggers; runs it started stay.", responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound}},
	{method: "POST", path: "/triggers/{triggerID}/enable", handler: "handleEnableTrigger", tag: "triggers",
		summary: "Turn one of your triggers on, checked again as when it was made.",
		responses: map[int]any{http.StatusOK: triggerResponse{}, http.StatusNotFound: notFound, http.StatusConflict: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/triggers/{triggerID}/disable", handler: "handleDisableTrigger", tag: "triggers",
		summary: "Turn one of your triggers off.", responses: map[int]any{http.StatusOK: triggerResponse{}, http.StatusNotFound: notFound}},
	{method: "POST", path: "/triggers/schedule", handler: "handlePreviewSchedule", tag: "triggers",
		summary: "When a schedule would fire next, or what is wrong with it.", request: scheduleSpec{},
		responses: map[int]any{http.StatusOK: schedulePreview{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

// triggerKind is what starts a trigger: a schedule or a tool's event.
type triggerKind string

// fireOutcome is what became of one firing of a trigger.
type fireOutcome string

// disabledReason says why Rotor turned a trigger off; docs/api-contract-m6.md says each in a sentence.
type disabledReason string

// conditionOp is how a condition compares a value of the event.
type conditionOp string

// m6Enums gives the builder the values each M6 string type takes.
var m6Enums = map[reflect.Type][]string{
	reflect.TypeOf(triggerKind("")): {"schedule", "event"},
	reflect.TypeOf(fireOutcome("")): {"started", "skipped", "refused", "failed"},
	reflect.TypeOf(disabledReason("")): {"owner_switched_off", "connection_lost", "tool_removed", "workflow_archived", "workflow_hidden",
		"unattended_write", "inputs_refused"},
	reflect.TypeOf(conditionOp("")): {"equals", "in", "exists"},
}

// registerM6Schemas tells the builder about the M6 enums.
func registerM6Schemas(b *openapi.Builder) {
	for t, values := range m6Enums {
		b.Enums[t] = values
	}
}

// scheduleSpec is when a schedule trigger fires: five cron fields read in a time zone.
type scheduleSpec struct {
	Cron     string `json:"cron"`
	TimeZone string `json:"timeZone"`
	// NoOverlap skips a firing while the trigger's last run has not ended, a run waiting for an approval included.
	NoOverlap bool `json:"noOverlap"`
}

// triggerCondition holds when the event's value at path compares as op says; path is dotted, such as payload.review.state.
type triggerCondition struct {
	Path  string          `json:"path"`
	Op    conditionOp     `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

// eventSpec is which of a tool's events start an event trigger: every topic listed, in the projects listed if any,
// where every condition holds.
type eventSpec struct {
	ToolID     string             `json:"toolId"`
	Topics     []string           `json:"topics"`
	Projects   []string           `json:"projects"`
	Conditions []triggerCondition `json:"conditions"`
}

// triggerRequest makes or changes a trigger: schedule for a schedule trigger, event for an event trigger.
type triggerRequest struct {
	Name     string          `json:"name"`
	Kind     triggerKind     `json:"kind"`
	Schedule *scheduleSpec   `json:"schedule,omitempty"`
	Event    *eventSpec      `json:"event,omitempty"`
	Inputs   json.RawMessage `json:"inputs,omitempty"`
}

// triggerDisabled is why and when Rotor turned a trigger off; the owner turns it on again once that is mended.
type triggerDisabled struct {
	Reason  disabledReason `json:"reason"`
	Message string         `json:"message"`
	At      time.Time      `json:"at"`
}

// triggerFire is the last firing of a trigger and the run it started, if any.
type triggerFire struct {
	Outcome   fireOutcome `json:"outcome"`
	Reason    string      `json:"reason"`
	At        time.Time   `json:"at"`
	RunID     *string     `json:"runId"`
	RunStatus *runStatus  `json:"runStatus"`
}

// triggerResponse is a trigger as its owner sees it.
type triggerResponse struct {
	ID        string           `json:"id"`
	Workflow  personRef        `json:"workflow"`
	Owner     personRef        `json:"owner"`
	Name      string           `json:"name"`
	Kind      triggerKind      `json:"kind"`
	Enabled   bool             `json:"enabled"`
	Disabled  *triggerDisabled `json:"disabled"`
	Schedule  *scheduleSpec    `json:"schedule"`
	Event     *eventSpec       `json:"event"`
	Inputs    json.RawMessage  `json:"inputs"`
	NextRunAt *time.Time       `json:"nextRunAt"`
	LastFire  *triggerFire     `json:"lastFire"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

// schedulePreview is when a schedule fires next, in UTC.
type schedulePreview struct {
	NextRuns []time.Time `json:"nextRuns"`
}
