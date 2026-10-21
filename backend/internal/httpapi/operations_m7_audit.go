package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// The rows of the audit log's read side (#36); docs/api-contract-m7-audit.md says what they mean.
func init() {
	operations = append(operations, m7AuditOperations...)
}

// auditQuery is the filter every audit operation takes.
var auditQuery = []param{
	{name: "action", description: "Only entries of this action, one of the actions the log records."},
	{name: "actor", description: "Only entries of the person with this id."},
	{name: "targetType", description: "Only entries about this kind of target, such as run or trigger."},
	{name: "target", description: "Only entries about the target with this id."},
	{name: "from", description: "From this day (YYYY-MM-DD, the whole day in UTC) or instant (RFC 3339), inclusive."},
	{name: "to", description: "Up to this day (YYYY-MM-DD, the whole day in UTC) or instant (RFC 3339)."},
}

var m7AuditOperations = []operation{
	{method: "GET", path: "/audit", handler: "handleListAudit", tag: "audit", admin: true,
		summary: "The audit log, newest first, one window at a time.",
		query: append(append([]param{}, auditQuery...),
			param{name: "limit", description: "How many entries one window holds, at most 200."},
			param{name: "cursor", description: "The next window's cursor, from the window before."}),
		responses: map[int]any{http.StatusOK: auditWindow{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/audit/facets", handler: "handleAuditFacets", tag: "audit", admin: true,
		summary: "The actions, people and kinds of target the log holds, to filter it by.", responses: ok(auditFacets{})},
	{method: "GET", path: "/audit/export", handler: "handleExportAudit", tag: "audit", admin: true,
		summary: "The filtered log as one file, CSV or a JSON array; the export is itself recorded.",
		description: "Refused with 422 export_too_large when the filter matches more than 50000 entries; narrow it rather than " +
			"receive a file cut short.",
		query:     append(append([]param{}, auditQuery...), param{name: "format", description: "csv (the default) or json."}),
		responses: map[int]any{http.StatusOK: nil, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

// auditEntry is one entry as an administrator reads it; the actor is null when Rotor itself acted.
type auditEntry struct {
	ID         string          `json:"id"`
	Action     string          `json:"action"`
	TargetType string          `json:"targetType"`
	TargetID   string          `json:"targetId"`
	Actor      *personRef      `json:"actor"`
	Data       json.RawMessage `json:"data"`
	IP         string          `json:"ip"`
	RequestID  string          `json:"requestId"`
	At         time.Time       `json:"at"`
	// Via names the assistant the actor acted through, null when they acted in the browser (#47).
	Via *string `json:"via"`
}

// auditWindow is one window of the log and the cursor of the next, null at the end.
type auditWindow struct {
	Entries []auditEntry `json:"entries"`
	Next    *string      `json:"next"`
}

// auditFacets are what the log holds to narrow it by.
type auditFacets struct {
	Actions     []string    `json:"actions"`
	Actors      []personRef `json:"actors"`
	TargetTypes []string    `json:"targetTypes"`
}
