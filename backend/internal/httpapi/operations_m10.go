package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/cloudster1/rotor/backend/internal/oauth"
	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// The rows of M10: Rotor's own MCP endpoint and its authorization server (#47); docs/api-contract-m10.md says what
// they mean.
func init() {
	operations = append(operations, m10Operations...)
}

var m10Operations = []operation{
	{method: "GET", path: "/.well-known/oauth-protected-resource", handler: "handleProtectedResource", id: "getProtectedResource",
		tag: "assistants", root: true, public: true, summary: "Rotor's MCP endpoint described for an assistant (RFC 9728).",
		responses: map[int]any{http.StatusOK: oauth.ProtectedResource{}, http.StatusNotFound: notFound}},
	{method: "GET", path: "/.well-known/oauth-protected-resource" + mcpPath, handler: "handleProtectedResource", id: "getMCPProtectedResource",
		tag: "assistants", root: true, public: true, summary: "The same, at the address with the endpoint's path that a 401 at MCP names.",
		responses: map[int]any{http.StatusOK: oauth.ProtectedResource{}, http.StatusNotFound: notFound}},
	{method: "GET", path: "/.well-known/oauth-authorization-server", handler: "handleAuthorizationServer", tag: "assistants",
		root: true, public: true, summary: "Rotor described as the authorization server of its MCP endpoint (RFC 8414).",
		responses: map[int]any{http.StatusOK: oauth.ServerMetadata{}, http.StatusNotFound: notFound}},

	{method: "POST", path: "/oauth/register", handler: "handleOAuthRegister", tag: "assistants", public: true,
		summary:     "Register an assistant as a client (RFC 7591).",
		description: "At most 10 registrations an hour from one address; more are refused with 429.",
		request:     oauth.ClientMetadataInput{},
		responses: map[int]any{http.StatusCreated: oauth.RegisteredClient{}, http.StatusBadRequest: oauth.Error{},
			http.StatusTooManyRequests: oauth.Error{}}},
	{method: "GET", path: "/oauth/authorize", handler: "handleOAuthAuthorize", tag: "assistants", public: true,
		summary: "Start connecting an assistant: the browser goes to the consent page, or back to the assistant with an error.",
		query: []param{{name: "response_type"}, {name: "client_id"}, {name: "redirect_uri"}, {name: "state"}, {name: "code_challenge"},
			{name: "code_challenge_method"}, {name: "resource"}, {name: "scope"}},
		responses: map[int]any{http.StatusFound: nil}},
	{method: "POST", path: "/oauth/token", handler: "handleOAuthToken", tag: "assistants", public: true,
		summary:     "Trade a code and its PKCE verifier, or a refresh token, for an access and a refresh token.",
		description: "The answer is never cached. A code or refresh token used twice ends its grant.",
		form:        oauth.TokenRequest{}, responses: map[int]any{http.StatusOK: oauth.TokenResponse{}, http.StatusBadRequest: oauth.Error{},
			http.StatusUnauthorized: oauth.Error{}}},
	{method: "POST", path: "/oauth/revoke", handler: "handleOAuthRevoke", tag: "assistants", public: true,
		summary:     "End an access token, or a refresh token with its grant (RFC 7009).",
		description: "A token unknown or ended already answers 200 too.",
		form:        oauth.RevokeRequest{}, responses: map[int]any{http.StatusOK: nil, http.StatusBadRequest: oauth.Error{},
			http.StatusUnauthorized: oauth.Error{}}},
	{method: "GET", path: "/oauth/requests/{requestID}", handler: "handleGetOAuthRequest", tag: "assistants",
		summary:   "An assistant's request to connect, as the consent page shows it.",
		responses: map[int]any{http.StatusOK: env{"request": oauth.AuthorizationRequest{}}, http.StatusNotFound: notFound}},
	{method: "POST", path: "/oauth/requests/{requestID}", handler: "handleDecideOAuthRequest", tag: "assistants",
		summary: "Allow or deny an assistant, from the browser alone; the answer says where the browser goes back to.",
		request: oauth.ConsentInput{},
		responses: map[int]any{http.StatusOK: oauth.Decided{}, http.StatusNotFound: notFound, http.StatusForbidden: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/oauth/grants", handler: "handleListOAuthGrants", tag: "assistants",
		summary: "The assistants you connected, last used first.", responses: ok(env{"grants": []oauth.Grant{}})},
	{method: "DELETE", path: "/oauth/grants/{grantID}", handler: "handleEndOAuthGrant", tag: "assistants",
		summary:   "Disconnect one of your assistants; its tokens stop at once.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound}},
	{method: "GET", path: "/oauth/grants/everyone", handler: "handleListEveryonesOAuthGrants", tag: "assistants", admin: true,
		summary: "Everybody's connected assistants, with whose each is.", responses: ok(env{"grants": []oauth.OwnedGrant{}})},
	{method: "DELETE", path: "/oauth/grants/everyone/{grantID}", handler: "handleEndAnybodysOAuthGrant", tag: "assistants", admin: true,
		summary:   "Disconnect anybody's assistant; its tokens stop at once.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound}},
	{method: "GET", path: "/settings/assistants", handler: "handleGetAssistants", tag: "assistants", admin: true,
		summary: "Whether assistants may connect to this Rotor.", responses: ok(oauth.Setting{})},
	{method: "PUT", path: "/settings/assistants", handler: "handleSetAssistants", tag: "assistants", admin: true,
		summary:     "Switch assistants on or off for everyone.",
		description: "Off refuses every assistant's token at once and keeps the grants, so switching back on lets them in again.",
		request:     oauth.SettingInput{}, responses: ok(oauth.Setting{})},

	{method: "POST", path: "/mcp", handler: "handleMCP", id: "mcp", tag: "assistants",
		summary: "The Model Context Protocol endpoint: one JSON-RPC 2.0 request, answered in JSON.",
		description: "Takes an assistant's access token, or a session. A 401 names the protected resource metadata in WWW-Authenticate. " +
			"The tools are rows of this document; approvals are decided by a person in Rotor, never through a tool.",
		headers:   []param{{name: mcpVersionHeader, description: "The protocol revision the client speaks, after initialize."}},
		request:   json.RawMessage{},
		responses: map[int]any{http.StatusOK: rpcResponse{}, http.StatusAccepted: nil, http.StatusUnauthorized: errorEnvelope{}}},
}

// registerM10Schemas names the OAuth types for the document, where their Go names alone would say too little.
func registerM10Schemas(b *openapi.Builder) {
	for t, name := range map[reflect.Type]string{
		reflect.TypeOf(oauth.AuthorizationRequest{}): "OAuthAuthorizationRequest",
		reflect.TypeOf(oauth.ClientMetadataInput{}):  "OAuthClientMetadata",
		reflect.TypeOf(oauth.ClientRef{}):            "OAuthClient",
		reflect.TypeOf(oauth.ConsentInput{}):         "OAuthConsent",
		reflect.TypeOf(oauth.Decided{}):              "OAuthDecided",
		reflect.TypeOf(oauth.Error{}):                "OAuthError",
		reflect.TypeOf(oauth.Grant{}):                "OAuthGrant",
		reflect.TypeOf(oauth.OwnedGrant{}):           "OAuthOwnedGrant",
		reflect.TypeOf(oauth.Owner{}):                "OAuthGrantOwner",
		reflect.TypeOf(oauth.ProtectedResource{}):    "OAuthProtectedResource",
		reflect.TypeOf(oauth.RegisteredClient{}):     "OAuthRegisteredClient",
		reflect.TypeOf(oauth.RevokeRequest{}):        "OAuthRevokeRequest",
		reflect.TypeOf(oauth.ServerMetadata{}):       "OAuthServerMetadata",
		reflect.TypeOf(oauth.Setting{}):              "AssistantSetting",
		reflect.TypeOf(oauth.SettingInput{}):         "AssistantSettingInput",
		reflect.TypeOf(oauth.TokenRequest{}):         "OAuthTokenRequest",
		reflect.TypeOf(oauth.TokenResponse{}):        "OAuthTokenResponse",
	} {
		b.Names[t] = name
	}
	kinds := make([]string, len(oauth.ClientKinds))
	for i, k := range oauth.ClientKinds {
		kinds[i] = string(k)
	}
	b.Enums[reflect.TypeOf(oauth.ClientKind(""))] = kinds
}
