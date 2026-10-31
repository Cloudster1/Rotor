package httpapi

// How a service account of the realm adds tools and hands Rotor their webhook secrets (#168): its access token is
// taken at the routes the table marks serviceAccount and nowhere else, so dynamo-connect wires a suite unattended.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// ServiceAccounts lets in the account a realm's access token names; *oidc.Service is the real one.
type ServiceAccounts interface {
	Authenticate(ctx context.Context, token string) (*auth.Principal, error)
}

var (
	errRealmUnconfigured = &APIError{Status: http.StatusUnauthorized, Code: "invalid_token",
		Message: "This Rotor has no identity provider, so it takes no token of a realm. An operator sets ROTOR_OIDC_ISSUER."}
	errRealmUnreachable = &APIError{Status: http.StatusServiceUnavailable, Code: "realm_unreachable",
		Message: "Rotor cannot reach the realm to check the token. Try again once the identity provider answers."}
)

// serviceAccountRoutes names the routes that take a realm's token, in the words of the refusal everywhere else.
var serviceAccountRoutes = sync.OnceValue(func() string {
	var routes []string
	for _, op := range operations {
		if op.serviceAccount {
			routes = append(routes, op.method+" "+APIPrefix+op.path)
		}
	}
	return strings.Join(routes, ", ")
})

// errSessionCookieOnly refuses a realm's token at a route that takes the session cookie alone.
func errSessionCookieOnly() *APIError {
	return &APIError{Status: http.StatusUnauthorized, Code: "invalid_token",
		Message: "This route takes the session cookie of Rotor in your browser alone. A token of the realm is taken only at " +
			serviceAccountRoutes() + "."}
}

type ctxKeyRealmToken struct{}

// withRealmToken keeps a bearer token that is not Rotor's own, for the routes that take one to check.
func withRealmToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, ctxKeyRealmToken{}, token)
}

func realmTokenFrom(ctx context.Context) string {
	token, _ := ctx.Value(ctxKeyRealmToken{}).(string)
	return token
}

// serviceAccount lets in the account a realm's token names; it is mounted on the serviceAccount routes alone, and
// never for a call MCP makes in this process.
func (s *Server) serviceAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token := realmTokenFrom(ctx)
		if token == "" || auth.PrincipalFrom(ctx) != nil || inProcessCall(ctx) {
			next.ServeHTTP(w, r)
			return
		}
		if s.ServiceAccounts == nil {
			respondError(w, r, errRealmUnconfigured)
			return
		}
		p, err := s.ServiceAccounts.Authenticate(ctx, token)
		if err != nil {
			respondError(w, r, realmTokenError(w, r, err))
			return
		}
		next.ServeHTTP(w, r.WithContext(db.WithUser(auth.WithPrincipal(ctx, p), p.UserID)))
	})
}

// realmTokenError says why a realm's token was not taken, in the sentence the check wrote.
func realmTokenError(w http.ResponseWriter, r *http.Request, err error) error {
	var refusal *oidc.Refusal
	switch {
	case errors.As(err, &refusal):
		loggerFrom(r.Context()).Warn("realm token refused", "error", refusal.Reason)
		if errors.Is(err, oidc.ErrTokenNotAdmin) {
			return &APIError{Status: http.StatusForbidden, Code: "forbidden", Message: refusal.Message}
		}
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		return &APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: refusal.Message}
	case errors.Is(err, auth.ErrSwitchedOff):
		return errSwitchedOff
	case errors.Is(err, oidc.ErrNotConfigured):
		return errRealmUnconfigured
	case errors.Is(err, oidc.ErrUnreachable):
		return errRealmUnreachable
	}
	return ErrInternal(err)
}
