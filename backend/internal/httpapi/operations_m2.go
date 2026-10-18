package httpapi

import (
	"net/http"
)

// The rows of M2 and M3, workflows and runs; docs/api-contract-m2.md says what they mean.
func init() {
	operations = append(operations, m2Operations...)
}

var notFound = errorEnvelope{}

var m2Operations = []operation{
	// Workflows: versioned specs, shared by group (#15).
	{method: "GET", path: "/workflows", handler: "handleListWorkflows", tag: "workflows", pending: true,
		summary:   "The workflows you may see: yours, those shared with a group of yours, and those shared with everyone.",
		query:     []param{{name: "archived", description: "true lists archived workflows instead."}},
		responses: ok(env{"workflows": []workflowSummary{}})},
	{method: "POST", path: "/workflows", handler: "handleCreateWorkflow", tag: "workflows", pending: true,
		summary: "Create a workflow from its YAML; the first save is version 1.", request: saveWorkflowRequest{},
		responses: map[int]any{http.StatusCreated: workflowResponse{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/workflows/{workflowID}", handler: "handleGetWorkflow", tag: "workflows", pending: true,
		summary: "A workflow with its current version.", responses: map[int]any{http.StatusOK: workflowResponse{}, http.StatusNotFound: notFound}},
	{method: "PUT", path: "/workflows/{workflowID}", handler: "handleSaveWorkflow", tag: "workflows", pending: true,
		summary:     "Save a new version; versions are never changed, so runs that pinned one keep it.",
		description: "baseVersion must be the current version, or the save is refused with 409 so nobody overwrites a colleague unseen.",
		request:     saveWorkflowRequest{},
		responses: map[int]any{http.StatusOK: workflowResponse{}, http.StatusNotFound: notFound, http.StatusConflict: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/workflows/{workflowID}", handler: "handleArchiveWorkflow", tag: "workflows", pending: true,
		summary: "Archive a workflow; its versions stay for the runs that used them.", responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound}},
	{method: "GET", path: "/workflows/{workflowID}/versions", handler: "handleListWorkflowVersions", tag: "workflows", pending: true,
		summary: "Every version of a workflow, newest first.", responses: map[int]any{http.StatusOK: env{"versions": []versionSummary{}}, http.StatusNotFound: notFound}},
	{method: "GET", path: "/workflows/{workflowID}/versions/{versionNumber}", handler: "handleGetWorkflowVersion", tag: "workflows", pending: true,
		summary: "One version, with its YAML.", responses: map[int]any{http.StatusOK: versionResponse{}, http.StatusNotFound: notFound}},
	{method: "GET", path: "/workflows/{workflowID}/diff", handler: "handleDiffWorkflowVersions", tag: "workflows", pending: true,
		summary:   "What changed between two versions, line by line.",
		query:     []param{{name: "from", description: "The older version's number."}, {name: "to", description: "The newer version's number."}},
		responses: map[int]any{http.StatusOK: diffResponse{}, http.StatusNotFound: notFound}},
	{method: "PUT", path: "/workflows/{workflowID}/sharing", handler: "handleShareWorkflow", tag: "workflows", pending: true,
		summary: "Share a workflow privately, with groups of the realm, or with everyone; runs still act as whoever starts them.",
		request: sharingRequest{}, responses: map[int]any{http.StatusOK: workflowResponse{}, http.StatusNotFound: notFound, http.StatusForbidden: errorEnvelope{}}},
	{method: "POST", path: "/workflows/validate", handler: "handleValidateWorkflow", tag: "workflows", pending: true,
		summary: "Check a workflow's YAML against the schema, its own sense, and the tools you can use, without saving it.",
		request: validateRequest{}, responses: ok(validateResponse{})},
	{method: "GET", path: "/schemas/workflow.v1.json", handler: "handleWorkflowSchema", tag: "workflows", pending: true,
		summary: "The JSON Schema of workflow version 1, for editors.", responses: ok(map[string]any{})},
	{method: "GET", path: "/tools/{toolID}/catalog", handler: "handleToolCatalog", tag: "tools", pending: true,
		summary: "The MCP tools you may call at a tool, as your connection sees them, with their read-only and destructive hints.",
		responses: map[int]any{http.StatusOK: catalogResponse{}, http.StatusNotFound: notFound, http.StatusConflict: errorEnvelope{},
			http.StatusBadGateway: errorEnvelope{}}},

	// Language model providers (#20).
	{method: "GET", path: "/providers", handler: "handleListProviders", tag: "providers", pending: true, admin: true,
		summary: "The language model providers workflows may use; keys never leave OpenBao.", responses: ok(env{"providers": []providerResponse{}})},
	{method: "POST", path: "/providers", handler: "handleAddProvider", tag: "providers", pending: true, admin: true,
		summary: "Add a provider; its key goes to OpenBao.", request: providerRequest{},
		responses: map[int]any{http.StatusCreated: env{"provider": providerResponse{}}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "PUT", path: "/providers/{providerID}", handler: "handleChangeProvider", tag: "providers", pending: true, admin: true,
		summary: "Change a provider; an empty key keeps the one in OpenBao.", request: providerRequest{},
		responses: map[int]any{http.StatusOK: env{"provider": providerResponse{}}, http.StatusNotFound: notFound, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/providers/{providerID}", handler: "handleRemoveProvider", tag: "providers", pending: true, admin: true,
		summary: "Remove a provider and its key.", responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound}},
	{method: "POST", path: "/providers/{providerID}/test", handler: "handleTestProvider", tag: "providers", pending: true, admin: true,
		summary: "Ask the provider which models it offers, with the key Rotor holds.", responses: map[int]any{http.StatusOK: providerTestResponse{}, http.StatusNotFound: notFound}},
	{method: "GET", path: "/models", handler: "handleListModels", tag: "providers", pending: true,
		summary: "The models a workflow may name, and the default.", responses: ok(env{"models": []modelResponse{}})},

	// Runs (M3: #22 starts and carries them, #24 streams their events).
	{method: "POST", path: "/workflows/{workflowID}/runs", handler: "handleStartRun", tag: "runs", pending: true,
		summary:     "Start a run of a workflow as yourself.",
		description: "An Idempotency-Key header makes a retried request answer the run it started. A binding to a tool you are not connected to is refused with 409 naming the tools to connect.",
		request:     startRunRequest{},
		responses: map[int]any{http.StatusCreated: runResponse{}, http.StatusOK: runResponse{}, http.StatusNotFound: notFound,
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/runs", handler: "handleListRuns", tag: "runs", pending: true,
		summary: "Runs you may see: your own and those of workflows you own; administrators see all.",
		query: []param{{name: "workflow", description: "Only runs of this workflow."}, {name: "status", description: "Only runs in this state.", repeated: true},
			{name: "mine", description: "true lists only the runs you started."}},
		responses: ok(env{"runs": []runResponse{}})},
	{method: "GET", path: "/runs/{runID}", handler: "handleGetRun", tag: "runs", pending: true,
		summary: "One run.", responses: map[int]any{http.StatusOK: runResponse{}, http.StatusNotFound: notFound}},
	{method: "POST", path: "/runs/{runID}/cancel", handler: "handleCancelRun", tag: "runs", pending: true,
		summary: "Cancel a run that has not ended.", responses: map[int]any{http.StatusOK: runResponse{}, http.StatusNotFound: notFound, http.StatusConflict: errorEnvelope{}}},
	{method: "GET", path: "/runs/{runID}/events", handler: "handleRunEvents", tag: "runs", pending: true,
		summary:     "A run's events as server-sent events, from the start or after Last-Event-ID.",
		description: "Content type text/event-stream; each event's id is its sequence number and its data the JSON payload in docs/api-contract-m2.md.",
		responses:   map[int]any{http.StatusOK: nil, http.StatusNotFound: notFound}},
}
