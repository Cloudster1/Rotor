package httpapi

import (
	"net/http"
	"time"
)

// The retention rows of M8: how long a finished run's data is kept (#41); docs/api-contract-m8.md says what they mean.
func init() {
	operations = append(operations, m8RetentionOperations...)
}

var m8RetentionOperations = []operation{
	{method: "GET", path: "/settings/retention", handler: "handleGetRetention", tag: "retention", admin: true,
		summary: "How many days a finished run's data is kept before Rotor deletes it.", responses: ok(retentionResponse{})},
	{method: "PUT", path: "/settings/retention", handler: "handleSetRetention", tag: "retention", admin: true,
		summary:     "Change how many days a finished run's data is kept, from 1 to 3650.",
		description: "A workflow may keep its runs for fewer days with its own retention, never for more. The audit log is never deleted.",
		request:     retentionRequest{},
		responses:   map[int]any{http.StatusOK: retentionResponse{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

// retentionResponse is the installation's retention as an administrator sees it.
type retentionResponse struct {
	RunDataDays int `json:"runDataDays"`
	// MaxDays is the longest retention Rotor takes.
	MaxDays int `json:"maxDays"`
	// ChangedBy names who changed it last; null while it is the default.
	ChangedBy *string   `json:"changedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// retentionRequest changes the retention.
type retentionRequest struct {
	RunDataDays int `json:"runDataDays"`
}
