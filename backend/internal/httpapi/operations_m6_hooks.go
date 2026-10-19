package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// The hook rows of M6: where the tools deliver events (#35); docs/api-contract-m6.md says what they mean.
func init() {
	operations = append(operations, m6HookOperations...)
}

var m6HookOperations = []operation{
	{method: "GET", path: "/tools/{toolID}/hook", handler: "handleGetHook", tag: "hooks", admin: true, pending: true,
		summary:   "Where the tool delivers its events, and whether Rotor holds the secret it signs them with.",
		responses: map[int]any{http.StatusOK: hookResponse{}, http.StatusNotFound: notFound}},
	{method: "PUT", path: "/tools/{toolID}/hook", handler: "handleSetHookSecret", tag: "hooks", admin: true, pending: true,
		summary:     "Keep the secret the tool made for Rotor's webhook, in OpenBao.",
		description: "The tool shows the secret once, when its administrator adds the webhook or turns the secret over; paste it here right then.",
		request:     hookSecretRequest{},
		responses:   map[int]any{http.StatusOK: hookResponse{}, http.StatusNotFound: notFound, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/tools/{toolID}/hook", handler: "handleRemoveHookSecret", tag: "hooks", admin: true, pending: true,
		summary: "Forget the secret; the tool's events are refused from then on.", responses: map[int]any{http.StatusOK: hookResponse{}, http.StatusNotFound: notFound}},
	{method: "POST", path: "/hooks/{toolID}", handler: "handleReceiveHook", tag: "hooks", public: true, pending: true,
		summary: "Where a tool delivers an event, signed with the secret its administrator gave Rotor.",
		description: "Envelope v1 from Armature, Exciter and Stator, CloudEvents (v2) from Exciter. 202 once the event is kept for the " +
			"triggers it starts; 200 for an event already received or a ping; 401 for a signature that is wrong or older than the replay " +
			"window; 404 for a tool without a secret; 413 for a body too large; 422 for a version Rotor does not read.",
		request: json.RawMessage{},
		responses: map[int]any{http.StatusAccepted: statusResponse{}, http.StatusOK: statusResponse{}, http.StatusUnauthorized: errorEnvelope{},
			http.StatusNotFound: notFound, http.StatusRequestEntityTooLarge: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

// hookResponse is a tool's webhook as Rotor sees it: where to deliver, and whether Rotor can check what arrives.
type hookResponse struct {
	// URL is what the tool's administrator enters as the webhook's address.
	URL string `json:"url"`
	// Product names the tool's headers, such as X-Exciter-Signature-Timestamped-256.
	Product        string     `json:"product"`
	SecretSet      bool       `json:"secretSet"`
	SecretSetAt    *time.Time `json:"secretSetAt"`
	LastReceivedAt *time.Time `json:"lastReceivedAt"`
}

// hookSecretRequest carries the secret a tool made, such as exciter_whs_...; it is never answered back.
type hookSecretRequest struct {
	Secret string `json:"secret"`
}
