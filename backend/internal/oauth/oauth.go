// Package oauth makes Rotor the authorization server of its own MCP endpoint (#47): a person consents, and their
// assistant gets a token for MCP alone. Rotor as a client of the tools is package connections.
package oauth

import (
	"time"

	"github.com/google/uuid"
)

// The prefixes tell Rotor's own secrets apart from each other, and let redaction find them.
const (
	AccessTokenPrefix  = "rotor_oat_"
	RefreshTokenPrefix = "rotor_ort_"
	codePrefix         = "rotor_oac_"
	clientSecretPrefix = "rotor_ocs_"
)

// The scopes a client may ask for; the person may narrow the full one to reading at the consent page.
const (
	ScopeMCP     = "mcp"
	ScopeMCPRead = "mcp:read"
)

// Scopes lists every scope, for the metadata documents.
var Scopes = []string{ScopeMCP, ScopeMCPRead}

// ProtectedResource is the MCP endpoint described for a client that met a 401 there (RFC 9728).
type ProtectedResource struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceName           string   `json:"resource_name"`
}

// ServerMetadata is Rotor described as an authorization server (RFC 8414).
type ServerMetadata struct {
	Issuer                                 string   `json:"issuer"`
	AuthorizationEndpoint                  string   `json:"authorization_endpoint"`
	TokenEndpoint                          string   `json:"token_endpoint"`
	RegistrationEndpoint                   string   `json:"registration_endpoint"`
	RevocationEndpoint                     string   `json:"revocation_endpoint"`
	ScopesSupported                        []string `json:"scopes_supported"`
	ResponseTypesSupported                 []string `json:"response_types_supported"`
	GrantTypesSupported                    []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
	// AuthorizationResponseIssParameterSupported says the redirect names the issuer (RFC 9207).
	AuthorizationResponseIssParameterSupported bool `json:"authorization_response_iss_parameter_supported"`
}

// ClientMetadataInput is what a client registers itself with (RFC 7591).
type ClientMetadataInput struct {
	RedirectURIs []string `json:"redirect_uris"`
	ClientName   string   `json:"client_name,omitempty"`
	ClientURI    string   `json:"client_uri,omitempty"`
	// TokenEndpointAuthMethod is none for a client that keeps no secret, the default.
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	SoftwareID              string   `json:"software_id,omitempty"`
	SoftwareVersion         string   `json:"software_version,omitempty"`
}

// RegisteredClient is the registration's answer: what was registered, with the identity it was given.
type RegisteredClient struct {
	ClientMetadataInput
	ClientID string `json:"client_id"`
	// ClientSecret is answered once, to a client that asked for a secret.
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at"`
}

// TokenRequest is the form a client posts to the token endpoint, for either grant.
type TokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
	CodeVerifier string `json:"code_verifier,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Resource     string `json:"resource,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// TokenResponse hands a client its tokens (RFC 6749).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// RevokeRequest is the form a client ends a token with (RFC 7009).
type RevokeRequest struct {
	Token         string `json:"token"`
	TokenTypeHint string `json:"token_type_hint,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
}

// Error is how the protocol's endpoints refuse, in the shape the RFCs give clients, with a sentence a person reads.
type Error struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ClientRef is a client as a person sees it at the consent page and in the list of connected assistants.
type ClientRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URI  string `json:"uri"`
}

// AuthorizationRequest is an authorization waiting for its person's consent.
type AuthorizationRequest struct {
	ID     uuid.UUID `json:"id"`
	Client ClientRef `json:"client"`
	// RedirectHost is where the browser goes back to, which the person should recognise.
	RedirectHost string `json:"redirectHost"`
	// ReadOnly says the client asked only to read.
	ReadOnly  bool      `json:"readOnly"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ConsentInput is the person's answer; a client that asked for everything may be granted only reading.
type ConsentInput struct {
	Allow    bool `json:"allow"`
	ReadOnly bool `json:"readOnly"`
}

// Decided is where the browser goes back to after the person answered.
type Decided struct {
	RedirectTo string `json:"redirectTo"`
}

// Grant is an assistant a person connected, as their list shows it.
type Grant struct {
	ID           uuid.UUID  `json:"id"`
	Client       ClientRef  `json:"client"`
	RedirectHost string     `json:"redirectHost"`
	ReadOnly     bool       `json:"readOnly"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt"`
}

// Owner is the person a grant is theirs, in the installation's list.
type Owner struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// OwnedGrant is a grant in the installation's list, with whose it is.
type OwnedGrant struct {
	Grant
	Owner Owner `json:"owner"`
}

// Setting is whether assistants may connect at all, as administrators set it.
type Setting struct {
	ConnectionsOn bool      `json:"connectionsOn"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// SettingInput switches assistants on or off for the installation.
type SettingInput struct {
	ConnectionsOn bool `json:"connectionsOn"`
}
