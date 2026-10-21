package httpapi

// How an assistant's access token is let in (#47), adapted from Exciter at d6dd00c: at MCP alone and for the calls
// MCP makes in this process, never for what a person does at a keyboard.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/oauth"
)

// OAuthServer is Rotor as the authorization server of its MCP endpoint; *oauth.Service is the real one.
type OAuthServer interface {
	Issuer() string
	Resource() string
	ResourceMetadata() string
	ProtectedResource() oauth.ProtectedResource
	Metadata() oauth.ServerMetadata
	Enabled(ctx context.Context) (bool, error)
	Register(ctx context.Context, in oauth.ClientMetadataInput, from string) (*oauth.RegisteredClient, error)
	Authorize(ctx context.Context, q url.Values) (string, error)
	Request(ctx context.Context, id uuid.UUID) (*oauth.AuthorizationRequest, error)
	Decide(ctx context.Context, person, id uuid.UUID, in oauth.ConsentInput) (string, error)
	Token(ctx context.Context, in oauth.TokenRequest, who oauth.ClientAuth) (*oauth.TokenResponse, error)
	Revoke(ctx context.Context, in oauth.RevokeRequest, who oauth.ClientAuth) error
	Authenticate(ctx context.Context, token string) (*auth.Principal, error)
	Grants(ctx context.Context, person uuid.UUID) ([]oauth.Grant, error)
	EveryonesGrants(ctx context.Context) ([]oauth.OwnedGrant, error)
	EndGrant(ctx context.Context, person, id uuid.UUID) error
	EndAnybodysGrant(ctx context.Context, actor, id uuid.UUID) error
	Setting(ctx context.Context) (oauth.Setting, error)
	Switch(ctx context.Context, in oauth.SettingInput) (oauth.Setting, error)
}

// mcpPath is where assistants call Rotor.
const mcpPath = APIPrefix + oauth.MCPPath

var (
	errMCPOnly = &APIError{Status: http.StatusUnauthorized, Code: "invalid_token",
		Message: "This token is for Rotor's MCP endpoint alone. Call " + mcpPath + " with it, or sign in to Rotor in your browser."}
	errAssistantsOff = &APIError{Status: http.StatusUnauthorized, Code: "invalid_token",
		Message: "Assistants are switched off on this Rotor. Ask its administrators to switch them on."}
	errSessionOnly = &APIError{Status: http.StatusForbidden, Code: "session_only",
		Message: "An assistant cannot do this. Do it yourself in Rotor in your browser."}
	errReadOnlyGrant = &APIError{Status: http.StatusForbidden, Code: "read_only",
		Message: "You connected this assistant to read only. Connect it again and allow it to act, or do this yourself in Rotor."}
)

// credentialFrom prefers the Authorization header over the session cookie, so a request that carries a token is
// never also taken for the browser's; bearer says which it was.
func credentialFrom(r *http.Request, cookieName string) (credential string, bearer bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			return "", false
		}
		return strings.TrimSpace(token), true
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value, false
	}
	return "", false
}

// assistantOf reads who an access token acts for: at MCP and for the calls it carries, nowhere else.
func (s *Server) assistantOf(r *http.Request, token string) (*auth.Principal, error) {
	if s.OAuth == nil || !oauth.IsAccessToken(token) {
		return nil, oauth.ErrInvalidToken
	}
	if r.URL.Path != mcpPath && !inProcessCall(r.Context()) {
		return nil, errMCPOnly
	}
	p, err := s.OAuth.Authenticate(r.Context(), token)
	if errors.Is(err, oauth.ErrSwitchedOff) {
		return nil, errAssistantsOff
	}
	return p, err
}

// requireMCPAuth is requireAuth for MCP, whose 401 tells a client where to learn how to be let in (RFC 9728).
func (s *Server) requireMCPAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.PrincipalFrom(r.Context()) != nil {
			next.ServeHTTP(w, r)
			return
		}
		if s.OAuth != nil {
			if on, err := s.OAuth.Enabled(r.Context()); err == nil && on {
				challenge := `Bearer resource_metadata="` + s.OAuth.ResourceMetadata() + `", scope="` + oauth.ScopeMCP + `"`
				if credential, _ := credentialFrom(r, s.CookieName); credential != "" {
					challenge += `, error="invalid_token"`
				}
				w.Header().Set("WWW-Authenticate", challenge)
			}
		}
		respondError(w, r, ErrUnauthorized(""))
	})
}

type ctxKeyInProcess struct{}

// withinProcess marks a call MCP makes in this process, which an access token is good for.
func withinProcess(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyInProcess{}, true)
}

func inProcessCall(ctx context.Context) bool {
	on, _ := ctx.Value(ctxKeyInProcess{}).(bool)
	return on
}

// readingPosts are the rows that change nothing though they are posted, by method and path.
var readingPosts = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	for _, op := range operations {
		if op.reads {
			out[op.method+" "+APIPrefix+op.path] = true
		}
	}
	return out
})

// readOnlyGrant refuses a write through an assistant connected to read, before any handler, so the promise holds
// for every route including ones added later.
func readOnlyGrant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFrom(r.Context())
		// MCP carries reads and writes alike; the rule holds for the calls it carries, which run through this again.
		if p.ThroughAssistant() && p.Assistant.ReadOnly && !safeMethod(r.Method) && r.URL.Path != mcpPath &&
			!readingPosts()[r.Method+" "+r.URL.Path] {
			respondError(w, r, errReadOnlyGrant)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireSession refuses an assistant what a person does at a keyboard: deciding an approval, connecting a tool,
// consenting, ending a grant, and administering Rotor.
func requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.PrincipalFrom(r.Context()).ThroughAssistant() {
			respondError(w, r, errSessionOnly)
			return
		}
		next.ServeHTTP(w, r)
	})
}
