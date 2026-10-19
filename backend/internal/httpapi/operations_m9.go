package httpapi

import "net/http"

// The rows of M9: Rotor in the suite (#43); docs/api-contract-m9.md says what they mean.
func init() {
	operations = append(operations, m9Operations...)
}

var m9Operations = []operation{
	{method: "POST", path: "/tools/{toolID}/check", handler: "handleCheckTool", tag: "tools", admin: true,
		summary: "Read the tool's meta and metadata again, for a tool that was updated, without registering again.",
		description: "Keeps the tool's name, endpoints, scopes and capabilities as they answer now. A tool that stopped offering " +
			"mcp@1 is kept and lists it under missing, so its pages say what to update.",
		responses: map[int]any{http.StatusOK: env{"tool": toolResponse{}}, http.StatusNotFound: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}, http.StatusBadGateway: errorEnvelope{}}},
}

// missingCapability is a capability Rotor needs that the tool did not list when last asked (docs/integration.md).
type missingCapability struct {
	// Capability is the name@version, such as webhooks@1.
	Capability string `json:"capability"`
	// Message says what Rotor cannot do without it and what to update.
	Message string `json:"message"`
}
