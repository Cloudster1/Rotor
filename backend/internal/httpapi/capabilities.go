package httpapi

import "slices"

// capability is one integration feature a partner checks for by name at /meta, with the operations it promises;
// docs/integration.md lists them. Adapted from Armature (see NOTICE).
type capability struct {
	name, promise string
	// operations are "METHOD /path" as the operations table spells them.
	operations []string
}

// rotorCapabilities may only use names from Dynamo's shared list (Dynamo#4).
var rotorCapabilities = []capability{
	{
		name:    "workflows@1",
		promise: "Workflows defined as versioned specs: listed, read, written, compared, shared and checked before saving.",
		operations: []string{
			"GET /workflows",
			"POST /workflows",
			"GET /workflows/{workflowID}",
			"PUT /workflows/{workflowID}",
			"DELETE /workflows/{workflowID}",
			"GET /workflows/{workflowID}/versions",
			"GET /workflows/{workflowID}/versions/{versionNumber}",
			"GET /workflows/{workflowID}/diff",
			"PUT /workflows/{workflowID}/sharing",
			"POST /workflows/validate",
		},
	},
	{
		name:    "runs@1",
		promise: "A workflow started as the caller, its runs listed and read, cancelled, and its events streamed as they happen.",
		operations: []string{
			"POST /workflows/{workflowID}/runs",
			"GET /runs",
			"GET /runs/{runID}",
			"POST /runs/{runID}/cancel",
			"GET /runs/{runID}/events",
		},
	},
	{
		name: "hooks@1",
		promise: "A tool's signed webhook deliveries, envelope version 1 or CloudEvents, received for the triggers they start, " +
			"with the secret kept by an administrator.",
		operations: []string{
			"POST /hooks/{toolID}",
			"GET /tools/{toolID}/hook",
			"PUT /tools/{toolID}/hook",
			"DELETE /tools/{toolID}/hook",
		},
	},
}

// capabilities is what this build announces beyond its API version; a partner checks for a name here rather than
// comparing versions.
func capabilities() []string {
	names := make([]string, 0, len(rotorCapabilities))
	for _, c := range rotorCapabilities {
		names = append(names, c.name)
	}
	slices.Sort(names)
	return names
}
