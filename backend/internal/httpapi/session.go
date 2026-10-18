package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// Sessions is what the middleware asks of the session store; *auth.Service is the real one.
type Sessions interface {
	Authenticate(ctx context.Context, token string) (*auth.Principal, error)
	End(ctx context.Context, token string) error
}

// SignIn is the identity provider's flow; *oidc.Service is the real one.
type SignIn interface {
	Start(ctx context.Context, next string) (string, error)
	Finish(ctx context.Context, state, code string, browser oidc.Browser) (token, next string, err error)
	BackchannelLogout(ctx context.Context, logoutToken string) error
}

var errSwitchedOff = &APIError{Status: http.StatusForbidden, Code: "account_switched_off",
	Message: "Your account is switched off. Ask an administrator of Rotor to switch it on again."}

// authenticate puts the person a session cookie names on the request; it never refuses an anonymous one,
// which requireAuth does where it matters. A switched-off person is refused rather than made anonymous.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := audit.WithIP(r.Context(), clientIP(r))
		cookie, err := r.Cookie(s.CookieName)
		if err != nil || cookie.Value == "" || s.Sessions == nil {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		p, err := s.Sessions.Authenticate(ctx, cookie.Value)
		switch {
		case errors.Is(err, auth.ErrNoSession):
			s.clearSessionCookie(w)
			next.ServeHTTP(w, r.WithContext(ctx))
		case errors.Is(err, auth.ErrSwitchedOff):
			s.clearSessionCookie(w)
			respondError(w, r, errSwitchedOff)
		case err != nil:
			respondError(w, r, ErrInternal(err))
		default:
			ctx = db.WithUser(auth.WithPrincipal(ctx, p), p.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		}
	})
}

// requireAuth refuses a request nobody is signed in for.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.PrincipalFrom(r.Context()) == nil {
			respondError(w, r, ErrUnauthorized(""))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: s.CookieName, Value: token, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

// clientIP is the address the request came from, for the audit log. Behind the stack's nginx it is the proxy's;
// trusting forwarded headers waits for a list of trusted proxies.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}
