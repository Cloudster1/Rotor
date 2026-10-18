package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// loginPath is the web client's sign-in page, where a failed sign-in lands with the reason.
const loginPath = "/login"

var (
	errSignInUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "sign_in_unavailable",
		Message: "Signing in is not set up on this installation. An operator sets ROTOR_OIDC_ISSUER to the identity provider."}
	errProviderUnreachable = &APIError{Status: http.StatusServiceUnavailable, Code: "provider_unreachable",
		Message: "The identity provider cannot be reached. Try again in a moment."}
)

// safeNext keeps a sign-in landing inside Rotor: a path of this site, never another site's address.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") ||
		strings.ContainsAny(next, "\r\n\t") || strings.HasPrefix(next, loginPath) {
		return "/"
	}
	return next
}

func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	target, err := s.SignIn.Start(r.Context(), safeNext(r.URL.Query().Get("next")))
	switch {
	case errors.Is(err, oidc.ErrNotConfigured):
		respondError(w, r, errSignInUnavailable)
	case errors.Is(err, oidc.ErrUnreachable):
		loggerFrom(r.Context()).Warn("identity provider unreachable", "error", err)
		respondError(w, r, errProviderUnreachable)
	case err != nil:
		respondError(w, r, ErrInternal(err))
	default:
		redirect(w, target)
	}
}

// redirect sends the browser on without the HTML body http.Redirect writes, as nothing here answers HTML.
func redirect(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusFound)
}

// handleOIDCCallback lands the browser where it was headed, signed in, or on the sign-in page with the reason.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("error") != "" {
		s.failSignIn(w, r, "denied", errors.New(q.Get("error")))
		return
	}
	token, next, err := s.SignIn.Finish(r.Context(), q.Get("state"), q.Get("code"),
		oidc.Browser{UserAgent: r.UserAgent(), IP: clientIP(r)})
	switch {
	case errors.Is(err, auth.ErrSwitchedOff):
		s.failSignIn(w, r, "switched_off", err)
	case errors.Is(err, oidc.ErrUnknownLogin):
		s.failSignIn(w, r, "expired", err)
	case errors.Is(err, oidc.ErrNoEmail), errors.Is(err, oidc.ErrEmailUnverified):
		s.failSignIn(w, r, "no_email", err)
	case err != nil:
		s.failSignIn(w, r, "failed", err)
	default:
		s.setSessionCookie(w, token, s.SessionTTL)
		redirect(w, safeNext(next))
	}
}

func (s *Server) failSignIn(w http.ResponseWriter, r *http.Request, reason string, err error) {
	loggerFrom(r.Context()).Warn("sign-in failed", "reason", reason, "error", err)
	redirect(w, loginPath+"?error="+url.QueryEscape(reason))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.CookieName); err == nil && cookie.Value != "" {
		if err := s.Sessions.End(r.Context(), cookie.Value); err != nil {
			respondError(w, r, ErrInternal(err))
			return
		}
	}
	s.clearSessionCookie(w)
	respondNoContent(w)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	respondJSON(w, r, http.StatusOK, meResponse{
		User: meUser{ID: p.UserID.String(), Email: p.Email, Name: p.Name}, Role: p.Role, Groups: p.Groups,
	})
}

// handleBackchannelLogout answers the provider as the specification asks: 200 when the sessions are ended,
// 400 when the token is refused, never cached either way.
func (s *Server) handleBackchannelLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, config.MaxLogoutTokenBody)
	if err := r.ParseForm(); err != nil || r.PostForm.Get("logout_token") == "" {
		respondError(w, r, ErrBadRequest("Send the logout token as the form field logout_token."))
		return
	}
	err := s.SignIn.BackchannelLogout(r.Context(), r.PostForm.Get("logout_token"))
	switch {
	case errors.Is(err, oidc.ErrBadLogoutToken):
		loggerFrom(r.Context()).Warn("logout token refused", "error", err)
		respondError(w, r, &APIError{Status: http.StatusBadRequest, Code: "invalid_logout_token", Message: "The logout token is not valid."})
	case errors.Is(err, oidc.ErrNotConfigured):
		respondError(w, r, errSignInUnavailable)
	case err != nil:
		respondError(w, r, ErrInternal(err))
	default:
		w.WriteHeader(http.StatusOK)
	}
}
