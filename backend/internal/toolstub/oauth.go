package toolstub

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// The authorization server, as Exciter's backend/internal/oauth is one: the same endpoints, the same refusals in the
// RFCs' shape, refresh tokens that rotate, and a replaced one that ends its grant when it comes back.

// Prefixes tell the stub's secrets apart, as Exciter's exciter_oat_ and exciter_ort_ do.
const (
	accessPrefix       = "stub_oat_"
	refreshPrefix      = "stub_ort_"
	codePrefix         = "stub_oac_"
	clientSecretPrefix = "stub_ocs_"
	// PATPrefix is the personal token a tenant without OAuth takes at MCP: stub_pat_<username>.
	PATPrefix = "stub_pat_"
)

// The scopes a tenant offers, Exciter's: everything, reading, or evidence alone, which never reaches MCP.
const (
	ScopeMCP      = "mcp"
	ScopeMCPRead  = "mcp:read"
	ScopeEvidence = "evidence"
)

var (
	scopes      = []string{ScopeMCP, ScopeMCPRead, ScopeEvidence}
	authMethods = []string{"none", "client_secret_basic", "client_secret_post"}
	grantTypes  = []string{"authorization_code", "refresh_token"}
)

// The RFCs' error codes the stub answers with.
const (
	errInvalidRequest       = "invalid_request"
	errInvalidClient        = "invalid_client"
	errInvalidGrant         = "invalid_grant"
	errUnauthorizedClient   = "unauthorized_client"
	errUnsupportedGrantType = "unsupported_grant_type"
	errUnsupportedResponse  = "unsupported_response_type"
	errInvalidScope         = "invalid_scope"
	errInvalidTarget        = "invalid_target"
	errAccessDenied         = "access_denied"
	errInvalidRedirectURI   = "invalid_redirect_uri"
	errInvalidMetadata      = "invalid_client_metadata"
)

type client struct {
	id, name, authMethod string
	secretHash           []byte
	redirectURIs         []string
	grantTypes           []string
}

type authRequest struct {
	client          *client
	redirect, state string
	challenge       string
	readOnly, evid  bool
	expires         time.Time
}

type code struct {
	grant     *grant
	redirect  string
	challenge string
	used      bool
	expires   time.Time
}

type grant struct {
	id, clientID, username string
	readOnly, evidence     bool
}

type token struct {
	grant    *grant
	refresh  bool
	readOnly bool
	expires  time.Time
	replaced bool
}

// oauthError is how the protocol's endpoints refuse (RFC 6749, 5.2).
type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func refuse(w http.ResponseWriter, status int, code, description string, basic bool) {
	if status == http.StatusUnauthorized && basic {
		w.Header().Set("WWW-Authenticate", `Basic realm="tool-stub"`)
	}
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, oauthError{Error: code, ErrorDescription: description})
}

func secret(prefix string) string {
	b := make([]byte, config.ToolStubSecretBytes)
	_, _ = rand.Read(b)
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

func digest(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func (s *Stub) handleProtectedResource(c *call) {
	writeJSON(c.w, http.StatusOK, map[string]any{"resource": c.resource(), "authorization_servers": []string{c.issuer},
		"scopes_supported": scopes, "bearer_methods_supported": []string{"header"}, "resource_name": c.t.product()})
}

func (s *Stub) handleServerMetadata(c *call) {
	api := c.issuer + apiPrefix
	writeJSON(c.w, http.StatusOK, map[string]any{
		"issuer": c.issuer, "authorization_endpoint": api + "/oauth/authorize", "token_endpoint": api + "/oauth/token",
		"registration_endpoint": api + "/oauth/register", "revocation_endpoint": api + "/oauth/revoke",
		"scopes_supported": scopes, "response_types_supported": []string{"code"}, "grant_types_supported": grantTypes,
		"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": authMethods,
		"revocation_endpoint_auth_methods_supported": authMethods, "client_id_metadata_document_supported": false,
		"authorization_response_iss_parameter_supported": true,
	})
}

// registration is RFC 7591's client metadata, as Exciter reads it; unknown fields are ignored as the RFC says.
type registration struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

func (s *Stub) handleRegister(c *call) {
	var in registration
	if err := json.NewDecoder(http.MaxBytesReader(c.w, c.r.Body, MaxFormBytes)).Decode(&in); err != nil {
		refuse(c.w, http.StatusBadRequest, errInvalidMetadata, "Send the client's metadata as one JSON object.", false)
		return
	}
	if code, why := checkRegistration(&in); code != "" {
		refuse(c.w, http.StatusBadRequest, code, why, false)
		return
	}
	cl := &client{id: secret(""), name: in.ClientName, authMethod: in.TokenEndpointAuthMethod,
		redirectURIs: in.RedirectURIs, grantTypes: in.GrantTypes}
	out := map[string]any{"client_id": cl.id, "client_id_issued_at": s.now().Unix(), "client_secret_expires_at": 0,
		"redirect_uris": in.RedirectURIs, "client_name": in.ClientName, "token_endpoint_auth_method": in.TokenEndpointAuthMethod,
		"grant_types": in.GrantTypes}
	if in.ResponseTypes != nil {
		out["response_types"] = in.ResponseTypes
	}
	if cl.authMethod != "none" {
		plain := secret(clientSecretPrefix)
		cl.secretHash, out["client_secret"] = digest(plain), plain
	}
	c.t.clients[cl.id] = cl
	c.t.stats.Registrations++
	writeJSON(c.w, http.StatusCreated, out)
}

// checkRegistration holds a registration to Exciter's rules and fills its defaults; code is empty when it passes.
func checkRegistration(in *registration) (code, why string) {
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > config.ToolStubMaxRedirectURIs {
		return errInvalidRedirectURI, "Register between 1 and 10 redirect addresses."
	}
	for _, r := range in.RedirectURIs {
		if !validRedirect(r) {
			return errInvalidRedirectURI, "The redirect " + r + " is neither https nor http on a loopback address, or it has a fragment."
		}
	}
	if in.TokenEndpointAuthMethod == "" {
		in.TokenEndpointAuthMethod = "none"
	}
	if !slices.Contains(authMethods, in.TokenEndpointAuthMethod) {
		return errInvalidMetadata, "The token endpoint takes none, client_secret_basic or client_secret_post."
	}
	if len(in.GrantTypes) == 0 {
		in.GrantTypes = slices.Clone(grantTypes)
	}
	for _, g := range in.GrantTypes {
		if !slices.Contains(grantTypes, g) {
			return errInvalidMetadata, "The grants offered are authorization_code and refresh_token; " + g + " is not one of them."
		}
	}
	if !slices.Contains(in.GrantTypes, "authorization_code") {
		return errInvalidMetadata, "A client is connected through authorization_code; register it among its grants."
	}
	for _, t := range in.ResponseTypes {
		if t != "code" {
			return errInvalidMetadata, "The one response type is code."
		}
	}
	if in.ClientName = strings.TrimSpace(in.ClientName); len([]rune(in.ClientName)) > config.ToolStubMaxClientName {
		return errInvalidMetadata, "A client's name is at most 100 characters."
	}
	if in.ClientName == "" {
		in.ClientName = redirectHost(in.RedirectURIs[0])
	}
	return "", ""
}

func loopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// validRedirect is https, or http on a loopback address, without a fragment.
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Fragment != "" || u.User != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && loopback(u.Hostname()))
}

// redirectMatches is Exciter's rule: the address registered, but for the port of a loopback one (RFC 8252).
func redirectMatches(registered []string, asked string) bool {
	if slices.Contains(registered, asked) {
		return true
	}
	a, err := url.Parse(asked)
	if err != nil || a.Scheme != "http" || !loopback(a.Hostname()) {
		return false
	}
	for _, each := range registered {
		r, err := url.Parse(each)
		if err == nil && r.Scheme == a.Scheme && r.Hostname() == a.Hostname() && r.Path == a.Path && r.RawQuery == a.RawQuery {
			return true
		}
	}
	return false
}

func redirectHost(redirect string) string {
	if u, err := url.Parse(redirect); err == nil {
		return u.Host
	}
	return redirect
}

func withQuery(address string, q url.Values) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	values := u.Query()
	for k, v := range q {
		values[k] = v
	}
	u.RawQuery = values.Encode()
	return u.String()
}

// challengeShape is an S256 challenge, verifierShape RFC 7636's code_verifier.
var (
	challengeShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	verifierShape  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

func verifyPKCE(challenge, verifier string) bool {
	if !verifierShape.MatchString(verifier) || !challengeShape.MatchString(challenge) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// scopeAsked reads a scope as Exciter does: mcp or nothing is everything, mcp:read reading, evidence alone evidence.
func scopeAsked(asked string) (readOnly, evidence, ok bool) {
	fields := strings.Fields(asked)
	if slices.Contains(fields, ScopeEvidence) {
		for _, f := range fields {
			if f != ScopeEvidence {
				return false, false, false
			}
		}
		return false, true, true
	}
	readOnly = len(fields) > 0
	for _, f := range fields {
		switch f {
		case ScopeMCP:
			readOnly = false
		case ScopeMCPRead:
		default:
			return false, false, false
		}
	}
	return readOnly, false, true
}

func scopeName(readOnly, evidence bool) string {
	switch {
	case evidence:
		return ScopeEvidence
	case readOnly:
		return ScopeMCPRead
	}
	return ScopeMCP
}

// handleAuthorize checks a request in Exciter's order: a client or redirect that is wrong never goes back to the
// client, everything else goes back with the error, and a good one waits at the consent page.
func (s *Stub) handleAuthorize(c *call) {
	q := c.r.URL.Query()
	page := c.issuer + "/oauth/consent"
	cl, known := c.t.clients[q.Get("client_id")]
	if !known {
		http.Redirect(c.w, c.r, page+"?error=invalid_client", http.StatusFound)
		return
	}
	redirect := q.Get("redirect_uri")
	if !redirectMatches(cl.redirectURIs, redirect) {
		http.Redirect(c.w, c.r, page+"?error=invalid_redirect", http.StatusFound)
		return
	}
	state := q.Get("state")
	back := func(code, why string) {
		http.Redirect(c.w, c.r, withQuery(redirect, url.Values{"error": {code}, "error_description": {why},
			"state": {state}, "iss": {c.issuer}}), http.StatusFound)
	}
	readOnly, evidence, scopeOK := scopeAsked(q.Get("scope"))
	switch {
	case q.Get("response_type") != "code":
		back(errUnsupportedResponse, "The one response type is code.")
	case state == "" || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256":
		back(errInvalidRequest, "Send state, and a PKCE code_challenge with code_challenge_method S256.")
	case !challengeShape.MatchString(q.Get("code_challenge")):
		back(errInvalidRequest, "The code_challenge is not an S256 challenge.")
	case strings.TrimRight(q.Get("resource"), "/") != c.resource():
		back(errInvalidTarget, "Name the MCP endpoint, "+c.resource()+", as the resource.")
	case !scopeOK:
		back(errInvalidScope, "Ask for mcp, mcp:read to only read, or evidence alone to upload evidence.")
	case !slices.Contains(cl.grantTypes, "authorization_code"):
		back(errUnauthorizedClient, "The client did not register for authorization_code.")
	default:
		id := secret("")
		c.t.requests[id] = &authRequest{client: cl, redirect: redirect, state: state, challenge: q.Get("code_challenge"),
			readOnly: readOnly, evid: evidence, expires: s.now().Add(RequestTTL)}
		c.t.stats.Authorizations++
		http.Redirect(c.w, c.r, page+"?request="+url.QueryEscape(id), http.StatusFound)
	}
}
