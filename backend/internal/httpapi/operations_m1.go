package httpapi

import (
	"net/http"
	"reflect"
	"time"

	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// The rows of M1, tool connections; docs/api-contract-m1.md says what they mean.
func init() {
	operations = append(operations, m1Operations...)
}

var m1Operations = []operation{
	// The tools Rotor knows and is registered with as an OAuth client (#9).
	{method: "GET", path: "/tools", handler: "handleListTools", tag: "tools",
		summary: "The tools Rotor can act in, with what each offers.", responses: ok(env{"tools": []toolResponse{}})},
	{method: "POST", path: "/tools", handler: "handleAddTool", tag: "tools", admin: true,
		summary: "Add a tool by its address; Rotor registers itself there as an OAuth client.",
		description: "Reads the tool's /api/v1/meta, which must offer mcp@1, then its protected resource and " +
			"authorization server metadata (RFC 9728, RFC 8414), and registers by RFC 7591 unless a client is given. " +
			"Adding an address Rotor already knows registers again, for a tool that forgot its client.",
		request: addToolRequest{},
		responses: map[int]any{http.StatusCreated: env{"tool": toolResponse{}}, http.StatusOK: env{"tool": toolResponse{}},
			http.StatusUnprocessableEntity: errorEnvelope{}, http.StatusBadGateway: errorEnvelope{}, http.StatusServiceUnavailable: errorEnvelope{}}},
	{method: "GET", path: "/tools/{toolID}", handler: "handleGetTool", tag: "tools",
		summary: "One tool.", responses: map[int]any{http.StatusOK: env{"tool": toolResponse{}}, http.StatusNotFound: errorEnvelope{}}},
	{method: "DELETE", path: "/tools/{toolID}", handler: "handleRemoveTool", tag: "tools", admin: true,
		summary:   "Remove a tool, ending every connection to it.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: errorEnvelope{}}},

	// A person's own connections to the tools (#11).
	{method: "GET", path: "/connections", handler: "handleListConnections", tag: "connections",
		summary: "Your connections, one per tool you connected.", responses: ok(env{"connections": []connectionResponse{}})},
	{method: "POST", path: "/tools/{toolID}/connection", handler: "handleConnect", tag: "connections",
		summary:     "Start connecting your account in a tool; the browser goes to the address answered.",
		description: "Authorization code with PKCE S256, a state bound to this session and the tool's MCP endpoint as resource (RFC 8707).",
		query:       []param{{name: "next", description: "A path of Rotor's the browser returns to once connected, such as the workflow a run was started from; the Connections page otherwise."}},
		responses: map[int]any{http.StatusOK: connectResponse{}, http.StatusNotFound: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusBadGateway: errorEnvelope{}}},
	{method: "GET", path: "/connections/callback", handler: "handleConnectionCallback", tag: "connections", public: true,
		summary:     "Where a tool returns the browser after consent; keeps the connection and goes on to the page the connection was started from.",
		description: "Public so it can always redirect: an answer without the session that started it lands on the page as mismatch.",
		query: []param{{name: "code"}, {name: "state"}, {name: "iss", description: "The authorization server that answers (RFC 9207)."},
			{name: "error"}, {name: "error_description"}},
		responses: map[int]any{http.StatusFound: nil}},
	{method: "DELETE", path: "/tools/{toolID}/connection", handler: "handleDisconnect", tag: "connections",
		summary:   "End your connection to a tool, revoking it there.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: errorEnvelope{}}},
}

// toolStatus says whether people can connect to a tool.
type toolStatus string

// Tool statuses; needs_registration is a tool that no longer knows Rotor's client.
const (
	toolReady             toolStatus = "ready"
	toolNeedsRegistration toolStatus = "needs_registration"
)

// toolRegistration says how Rotor became the tool's client.
type toolRegistration string

const (
	registrationDynamic toolRegistration = "dynamic"
	registrationManual  toolRegistration = "manual"
)

// connectionStatus says whether a connection can be used or must be made again.
type connectionStatus string

const (
	connectionConnected connectionStatus = "connected"
	connectionReconnect connectionStatus = "reconnect"
)

// m1Enums gives the builder the values each M1 string type takes.
var m1Enums = map[reflect.Type][]string{
	reflect.TypeOf(toolStatus("")):       {string(toolReady), string(toolNeedsRegistration)},
	reflect.TypeOf(toolRegistration("")): {string(registrationDynamic), string(registrationManual)},
	reflect.TypeOf(connectionStatus("")): {string(connectionConnected), string(connectionReconnect)},
}

// toolResponse is a tool as everyone signed in sees it; its client secret never leaves OpenBao.
type toolResponse struct {
	ID string `json:"id"`
	// Name is the product the tool's /api/v1/meta names, such as exciter.
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	// Resource is the MCP endpoint tokens are asked for (RFC 8707).
	Resource     string   `json:"resource"`
	Issuer       string   `json:"issuer"`
	Capabilities []string `json:"capabilities"`
	// Missing is what Rotor needs that the tool does not offer; empty for a tool that offers it all.
	Missing         []missingCapability `json:"missing"`
	ScopesSupported []string            `json:"scopesSupported"`
	Registration    toolRegistration    `json:"registration"`
	Status          toolStatus          `json:"status"`
	AddedAt         time.Time           `json:"addedAt"`
}

// addToolRequest names a tool by its address; a pre-registered client comes with its secret.
type addToolRequest struct {
	BaseURL      string `json:"baseURL"`
	ClientID     string `json:"clientID,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
}

// connectionResponse is one of the caller's connections; the tokens behind it never leave OpenBao.
type connectionResponse struct {
	Tool connectionTool `json:"tool"`
	// Scopes are what the person consented to at the tool.
	Scopes      []string         `json:"scopes"`
	Status      connectionStatus `json:"status"`
	ConnectedAt time.Time        `json:"connectedAt"`
	LastUsedAt  *time.Time       `json:"lastUsedAt"`
}

// connectionTool names the tool a connection is to.
type connectionTool struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
}

// connectResponse is where the browser goes to consent at the tool.
type connectResponse struct {
	AuthorizeURL string `json:"authorizeURL"`
}

// registerM1Schemas tells the builder about the M1 enums.
func registerM1Schemas(b *openapi.Builder) {
	for t, values := range m1Enums {
		b.Enums[t] = values
	}
}
