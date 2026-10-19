package httpapi

import (
	"net/http"
	"time"
)

// The telemetry rows of M7: whether runs' spans also go to LangSmith (#37); docs/api-contract-m7-telemetry.md says
// what they mean.
func init() {
	operations = append(operations, m7TelemetryOperations...)
}

var m7TelemetryOperations = []operation{
	{method: "GET", path: "/settings/langsmith", handler: "handleGetLangSmith", tag: "telemetry", admin: true,
		summary: "Whether runs' spans also go to LangSmith, and under which project; never the key.", responses: ok(langSmithResponse{})},
	{method: "PUT", path: "/settings/langsmith", handler: "handleSetLangSmith", tag: "telemetry", admin: true,
		summary: "Turn LangSmith on or off, name its project, or give a new key, kept in OpenBao.",
		description: "Turning it on is refused with 422 data_leaves_unacknowledged unless acknowledgeDataLeaves is true, since run data " +
			"then leaves the installation, and with 422 langsmith_key_missing while no key is held. Leaving key out keeps the one held.",
		request:   langSmithRequest{},
		responses: map[int]any{http.StatusOK: langSmithResponse{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/settings/langsmith/key", handler: "handleRemoveLangSmithKey", tag: "telemetry", admin: true,
		summary: "Forget the LangSmith key, which turns LangSmith off.", responses: ok(langSmithResponse{})},
}

// langSmithResponse is the LangSmith setting as an administrator sees it.
type langSmithResponse struct {
	Enabled  bool       `json:"enabled"`
	Project  string     `json:"project"`
	KeySet   bool       `json:"keySet"`
	KeySetAt *time.Time `json:"keySetAt"`
	// ChangedBy names who changed it last.
	ChangedBy *string   `json:"changedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Endpoint is where spans go while it is on, so an operator sees which host the api reaches.
	Endpoint string `json:"endpoint"`
}

// langSmithRequest changes the setting; key is written once and never answered back.
type langSmithRequest struct {
	Enabled               bool    `json:"enabled"`
	Project               string  `json:"project"`
	Key                   *string `json:"key,omitempty"`
	AcknowledgeDataLeaves bool    `json:"acknowledgeDataLeaves"`
}
