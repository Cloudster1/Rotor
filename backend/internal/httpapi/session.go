package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/oauth"
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

// authenticate puts the person a session cookie, or an assistant's access token, names on the request; it never
// refuses an anonymous one, which requireAuth does where it matters. A switched-off person is refused rather than
// made anonymous.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A call MCP makes in this process starts from the outer request's context, whose caller is checked again.
		ctx := audit.WithIP(forgetCaller(r.Context()), s.clientIP(r))
		credential, bearer := credentialFrom(r, s.CookieName)
		if credential == "" || (!bearer && s.Sessions == nil) {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		var (
			p   *auth.Principal
			err error
		)
		if bearer {
			p, err = s.assistantOf(r.WithContext(ctx), credential)
		} else {
			p, err = s.Sessions.Authenticate(ctx, credential)
		}
		var refused *APIError
		switch {
		case errors.Is(err, auth.ErrNoSession), errors.Is(err, oauth.ErrInvalidToken):
			if !bearer {
				s.clearSessionCookie(w)
			} else if !oauth.IsAccessToken(credential) {
				// A token that is not Rotor's own may be the realm's, which only the serviceAccount routes check.
				ctx = withRealmToken(ctx, credential)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		case errors.Is(err, auth.ErrSwitchedOff):
			if !bearer {
				s.clearSessionCookie(w)
			}
			respondError(w, r, errSwitchedOff)
		case errors.As(err, &refused):
			respondError(w, r, refused)
		case err != nil:
			respondError(w, r, ErrInternal(err))
		default:
			ctx = db.WithUser(auth.WithPrincipal(ctx, p), p.UserID)
			// What is done through an assistant is marked so for the database, which refuses what only a person
			// may do and names the assistant in the audit log (#47).
			if p.Assistant != nil {
				ctx = db.WithVia(ctx, p.Assistant.GrantID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		}
	})
}

// forgetCaller clears whoever an outer request acted for, so a call made in this process is held to its own
// credential alone.
func forgetCaller(ctx context.Context) context.Context {
	if !inProcessCall(ctx) {
		return ctx
	}
	return db.WithVia(db.WithUser(auth.WithPrincipal(ctx, nil), uuid.Nil), uuid.Nil)
}

// requireAuth refuses a request nobody is signed in for, and a realm's token where it is not taken in a sentence
// naming where it is.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.PrincipalFrom(r.Context()) == nil {
			if realmTokenFrom(r.Context()) != "" {
				respondError(w, r, errSessionCookieOnly())
				return
			}
			respondError(w, r, ErrUnauthorized(""))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// errAdminOnly refuses a member what only an administrator may do.
var errAdminOnly = ErrForbidden("Only an administrator of Rotor may do this. Ask one to do it, or to make you one in the realm.")

// requireAdmin refuses anyone but an administrator; it sits behind requireAuth.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.PrincipalFrom(r.Context()).IsAdmin() {
			respondError(w, r, errAdminOnly)
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
