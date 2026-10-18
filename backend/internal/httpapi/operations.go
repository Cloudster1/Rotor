package httpapi

import (
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// The API described in terms of the code that serves it. Every route has a row in operations and a test
// refuses a router and a table that disagree; the OpenAPI document is derived from the rows by reflection.

// env is a response envelope: the one or two keys a handler wraps its result
// in. A nil value is an untyped JSON value; a typed nil pointer is nullable.
type env map[string]any

// param is a query parameter.
type param struct {
	name, description string
	schema            *openapi.Schema
	repeated          bool
}

// operation is one row of the table.
type operation struct {
	method, path string
	handler      string
	// id names the operation when the handler's name would not be unique.
	id      string
	summary string
	tag     string
	query   []param
	// request is the JSON body type the handler decodes.
	request any
	// responses maps a status to its envelope or bare type; nil is no body.
	responses map[int]any
	// public routes need no session.
	public bool
	// root routes live outside APIPrefix, as the probes do.
	root        bool
	description string
	// pending routes are agreed but not built yet: they answer 501.
	pending bool
}

// operations is the table, by area, then by path.
var operations = []operation{
	{method: "GET", path: "/healthz", handler: "handleLiveness", tag: "health", summary: "Whether the process is running.",
		public: true, root: true, responses: ok(statusResponse{})},
	{method: "GET", path: "/readyz", handler: "handleReadiness", tag: "health", summary: "Whether the process can serve traffic.",
		public: true, root: true, responses: map[int]any{http.StatusOK: statusResponse{}, http.StatusServiceUnavailable: statusResponse{}}},
	{method: "GET", path: "/openapi.json", handler: "handleOpenAPI", tag: "health", summary: "This document.", public: true,
		responses: ok(openapi.Document{})},
	{method: "GET", path: "/meta", handler: "handleMeta", tag: "health", summary: "Which product and build answers here, and what it offers.",
		public: true, responses: ok(metaResponse{})},

	// Sign-in through the suite's identity provider, built in #6.
	{method: "GET", path: "/auth/oidc/start", handler: "handleOIDCStart", tag: "auth", public: true, pending: true,
		summary:   "Send the browser to the identity provider to sign in.",
		query:     []param{{name: "next", description: "Where in Rotor to land after signing in; a path, never another site."}},
		responses: map[int]any{http.StatusFound: nil}},
	{method: "GET", path: "/auth/oidc/callback", handler: "handleOIDCCallback", tag: "auth", public: true, pending: true,
		summary:   "Where the identity provider returns the browser; opens the session.",
		query:     []param{{name: "code"}, {name: "state"}, {name: "error"}},
		responses: map[int]any{http.StatusFound: nil}},
	{method: "POST", path: "/auth/oidc/backchannel-logout", handler: "handleBackchannelLogout", tag: "auth", public: true, pending: true,
		summary:     "Ends the sessions the identity provider says have ended.",
		description: "OpenID Connect Back-Channel Logout 1.0: a form with one field, logout_token, a JWT signed by the provider.",
		responses:   map[int]any{http.StatusOK: nil, http.StatusBadRequest: errorEnvelope{}}},
	{method: "POST", path: "/auth/logout", handler: "handleLogout", tag: "auth", pending: true,
		summary: "Ends this session.", responses: map[int]any{http.StatusNoContent: nil}},
	{method: "GET", path: "/auth/me", handler: "handleMe", tag: "auth", pending: true,
		summary: "Who is signed in, with their role and groups.", responses: ok(meResponse{})},
}

// meResponse is the signed-in person, as the web client shows them.
type meResponse struct {
	User   meUser   `json:"user"`
	Role   string   `json:"role"`
	Groups []string `json:"groups"`
}

// meUser is a person's identity, as the realm names them.
type meUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}
