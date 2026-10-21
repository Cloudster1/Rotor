package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// ErrSwitchedOff refuses everything OAuth while the installation's administrators have switched assistants off.
var ErrSwitchedOff = errors.New("assistants are switched off on this Rotor")

// MCPPath is the endpoint the tokens are for, under the API's prefix.
const MCPPath = "/mcp"

// ConsentPath is the web page a person decides at, which also says why a request was refused.
const ConsentPath = "/oauth/consent"

// secretBytes makes every secret this package hands out as unguessable as a session token.
const secretBytes = 32

// Service is the authorization server: it knows clients, keeps what people consented to and hands out tokens.
type Service struct {
	db *db.Cluster
	// issuer is ROTOR_APP_URL, the address everything is named from; api is it with the API's prefix.
	issuer, api string
	// fetch reads clients' metadata documents; nil offers none, and every client registers.
	fetch Fetcher
	now   func() time.Time
}

// NewService names the server from the web client's address and the API's prefix under it; fetch reads clients'
// metadata documents, through a client that refuses Rotor's own network.
func NewService(cluster *db.Cluster, appURL, apiPrefix string, fetch Fetcher) *Service {
	issuer := strings.TrimRight(appURL, "/")
	return &Service{db: cluster, issuer: issuer, api: issuer + apiPrefix, fetch: fetch, now: time.Now}
}

// Issuer is the server's own name, which a client compares and the redirect carries (RFC 9207).
func (s *Service) Issuer() string { return s.issuer }

// Resource is the MCP endpoint's address, the one resource a token is for (RFC 8707).
func (s *Service) Resource() string { return s.api + MCPPath }

// ResourceMetadata is where a client that met a 401 at the endpoint reads how to be let in (RFC 9728).
func (s *Service) ResourceMetadata() string {
	return s.issuer + "/.well-known/oauth-protected-resource" + strings.TrimPrefix(s.Resource(), s.issuer)
}

// ConsentPage is the web page a person decides at.
func (s *Service) ConsentPage() string { return s.issuer + ConsentPath }

// ProtectedResource describes the MCP endpoint for its clients.
func (s *Service) ProtectedResource() ProtectedResource {
	return ProtectedResource{Resource: s.Resource(), AuthorizationServers: []string{s.issuer}, ScopesSupported: Scopes,
		BearerMethodsSupported: []string{"header"}, ResourceName: "Rotor"}
}

// Metadata describes the server (RFC 8414); client ID metadata documents are offered while a fetcher reads them.
func (s *Service) Metadata() ServerMetadata {
	return ServerMetadata{
		Issuer: s.issuer, AuthorizationEndpoint: s.api + "/oauth/authorize", TokenEndpoint: s.api + "/oauth/token",
		RegistrationEndpoint: s.api + "/oauth/register", RevocationEndpoint: s.api + "/oauth/revoke",
		ScopesSupported: Scopes, ResponseTypesSupported: []string{ResponseCode}, GrantTypesSupported: GrantTypes,
		CodeChallengeMethodsSupported: []string{ChallengeS256}, TokenEndpointAuthMethodsSupported: AuthMethods,
		RevocationEndpointAuthMethodsSupported: AuthMethods, ClientIDMetadataDocumentSupported: s.fetch != nil,
		AuthorizationResponseIssParameterSupported: true,
	}
}

// secret is a new random value with a prefix, and the digest it is stored as; only the digest outlives the answer.
func secret(prefix string) (value string, digest []byte, err error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("read randomness: %w", err)
	}
	value = prefix + base64.RawURLEncoding.EncodeToString(buf)
	return value, auth.Hash(value), nil
}

// IsAccessToken says whether a credential claims to be an OAuth access token, which is good for MCP alone.
func IsAccessToken(credential string) bool {
	return strings.HasPrefix(credential, AccessTokenPrefix)
}
