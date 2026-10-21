package httpapi

// Rotor as the authorization server of its MCP endpoint (#47, docs/api-contract-m10.md), adapted from Exciter at
// 3b340ca and ee72629. The protocol's own endpoints answer in the RFCs' shape; the consent page's are the API's.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/oauth"
)

// The client endpoints never read the session: a client authenticates itself, so a cookie the browser adds
// proves nothing and the same-site check has nothing to guard.
var clientEndpoints = map[string]bool{
	APIPrefix + "/oauth/register": true, APIPrefix + "/oauth/token": true, APIPrefix + "/oauth/revoke": true,
}

var errOAuthOff = &APIError{Status: http.StatusServiceUnavailable, Code: "oauth_unavailable",
	Message: "Assistants cannot connect to this Rotor: its authorization server is not set up. Ask its administrators."}

func (s *Server) oauthOn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.OAuth == nil {
			respondError(w, r, errOAuthOff)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mountOAuthDiscovery serves the metadata where the RFCs put it, outside the API's prefix.
func (s *Server) mountOAuthDiscovery(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.oauthOn)
		r.Get("/.well-known/oauth-protected-resource", s.handleProtectedResource)
		r.Get("/.well-known/oauth-protected-resource"+mcpPath, s.handleProtectedResource)
		r.Get("/.well-known/oauth-authorization-server", s.handleAuthorizationServer)
	})
}

// mountOAuth serves the protocol to clients, the consent page's calls to whoever signed in, and the lists of
// connected assistants; consenting and ending a grant are done from a session alone.
func (s *Server) mountOAuth(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.oauthOn)
		r.Post("/oauth/register", s.handleOAuthRegister)
		r.Get("/oauth/authorize", s.handleOAuthAuthorize)
		r.Post("/oauth/token", s.handleOAuthToken)
		r.Post("/oauth/revoke", s.handleOAuthRevoke)
		r.With(requireAuth, requireSession).Get("/oauth/requests/{requestID}", s.handleGetOAuthRequest)
		r.With(requireAuth, requireSession).Post("/oauth/requests/{requestID}", s.handleDecideOAuthRequest)
		r.With(requireAuth, requireSession).Get("/oauth/grants", s.handleListOAuthGrants)
		r.With(requireAuth, requireSession).Delete("/oauth/grants/{grantID}", s.handleEndOAuthGrant)
		r.With(requireAuth, requireSession, requireAdmin).Get("/oauth/grants/everyone", s.handleListEveryonesOAuthGrants)
		r.With(requireAuth, requireSession, requireAdmin).Delete("/oauth/grants/everyone/{grantID}", s.handleEndAnybodysOAuthGrant)
		r.With(requireAuth, requireSession, requireAdmin).Get("/settings/assistants", s.handleGetAssistants)
		r.With(requireAuth, requireSession, requireAdmin).Put("/settings/assistants", s.handleSetAssistants)
	})
}

// whileOn answers 404 for what is not there while assistants are switched off.
func (s *Server) whileOn(w http.ResponseWriter, r *http.Request) bool {
	on, err := s.OAuth.Enabled(r.Context())
	if err != nil {
		respondError(w, r, err)
		return false
	}
	if !on {
		respondError(w, r, ErrNotFound("Assistants are switched off on this Rotor. Ask its administrators to switch them on."))
		return false
	}
	return true
}

func (s *Server) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	if s.whileOn(w, r) {
		respondJSON(w, r, http.StatusOK, s.OAuth.ProtectedResource())
	}
}

func (s *Server) handleAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	if s.whileOn(w, r) {
		respondJSON(w, r, http.StatusOK, s.OAuth.Metadata())
	}
}

// answerOAuth answers a protocol endpoint: what it made, or its refusal in the RFCs' shape. Nothing of it is kept
// by a cache, as it carries secrets.
func answerOAuth(w http.ResponseWriter, r *http.Request, status int, out any, err error, basic bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	var refused *oauth.Refusal
	switch {
	case errors.As(err, &refused):
		if refused.Status == http.StatusUnauthorized && basic {
			w.Header().Set("WWW-Authenticate", `Basic realm="rotor"`)
		}
		respondJSON(w, r, refused.Status, refused.Body)
	case err != nil:
		respondError(w, r, err)
	default:
		respondJSON(w, r, status, out)
	}
}

func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	var in oauth.ClientMetadataInput
	// A registration may carry metadata Rotor does not use, which RFC 7591 says to ignore, so unknown fields pass.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, config.OAuthMaxRegistrationBytes)).Decode(&in); err != nil {
		answerOAuth(w, r, 0, nil, &oauth.Refusal{Status: http.StatusBadRequest, Body: oauth.Error{Error: oauth.ErrInvalidClientMetadata,
			ErrorDescription: "Send the client's metadata as one JSON object."}}, false)
		return
	}
	made, err := s.OAuth.Register(r.Context(), in, s.clientIP(r))
	answerOAuth(w, r, http.StatusCreated, made, err, false)
}

func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	to, err := s.OAuth.Authorize(r.Context(), r.URL.Query())
	if err != nil {
		respondError(w, r, err)
		return
	}
	http.Redirect(w, r, to, http.StatusFound)
}

// readForm reads a client's form and who it says it is, preferring HTTP Basic, whose parts are form-encoded (RFC
// 6749, 2.3.1).
func readForm(w http.ResponseWriter, r *http.Request) (url.Values, oauth.ClientAuth, error) {
	r.Body = http.MaxBytesReader(w, r.Body, config.OAuthMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		return nil, oauth.ClientAuth{}, &oauth.Refusal{Status: http.StatusBadRequest, Body: oauth.Error{Error: oauth.ErrInvalidRequest,
			ErrorDescription: "Send the request as a form, application/x-www-form-urlencoded."}}
	}
	f := r.PostForm
	if id, secret, ok := r.BasicAuth(); ok {
		id, idErr := url.QueryUnescape(id)
		secret, secretErr := url.QueryUnescape(secret)
		if idErr != nil || secretErr != nil {
			return nil, oauth.ClientAuth{}, &oauth.Refusal{Status: http.StatusUnauthorized, Body: oauth.Error{Error: oauth.ErrInvalidClient,
				ErrorDescription: "The client's id and secret in HTTP Basic are not form-encoded."}}
		}
		return f, oauth.ClientAuth{ID: id, Secret: secret, Basic: true}, nil
	}
	return f, oauth.ClientAuth{ID: f.Get("client_id"), Secret: f.Get("client_secret")}, nil
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	f, who, err := readForm(w, r)
	if err != nil {
		answerOAuth(w, r, 0, nil, err, false)
		return
	}
	out, err := s.OAuth.Token(r.Context(), oauth.TokenRequest{
		GrantType: f.Get("grant_type"), Code: f.Get("code"), RedirectURI: f.Get("redirect_uri"), CodeVerifier: f.Get("code_verifier"),
		RefreshToken: f.Get("refresh_token"), Resource: f.Get("resource"), Scope: f.Get("scope"),
	}, who)
	answerOAuth(w, r, http.StatusOK, out, err, who.Basic)
}

func (s *Server) handleOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	f, who, err := readForm(w, r)
	if err == nil {
		err = s.OAuth.Revoke(r.Context(), oauth.RevokeRequest{Token: f.Get("token"), TokenTypeHint: f.Get("token_type_hint")}, who)
	}
	if err != nil {
		answerOAuth(w, r, 0, nil, err, who.Basic)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

// consentError maps the consent's and the lists' refusals to sentences.
func consentError(err error) error {
	switch {
	case errors.Is(err, oauth.ErrNoSuchGrant):
		return ErrNotFound("That assistant was not found. It may have been disconnected already; reload the list.")
	case errors.Is(err, oauth.ErrNoRequest):
		return ErrNotFound("The request to connect has expired or was answered already. Start connecting from the assistant again.")
	case errors.Is(err, oauth.ErrWiderThanAsked):
		return ErrValidation(map[string]string{"readOnly": "The assistant asked only to read, so it can only be allowed to read."})
	case errors.Is(err, oauth.ErrNotAdministrator):
		return errAdminOnly
	}
	return err
}

func (s *Server) handleGetOAuthRequest(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "requestID", "request")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	req, err := s.OAuth.Request(r.Context(), id)
	if err != nil {
		respondError(w, r, consentError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"request": req})
}

func (s *Server) handleDecideOAuthRequest(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "requestID", "request")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var in oauth.ConsentInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	to, err := s.OAuth.Decide(r.Context(), auth.PrincipalFrom(r.Context()).UserID, id, in)
	if err != nil {
		respondError(w, r, consentError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, oauth.Decided{RedirectTo: to})
}

func (s *Server) handleListOAuthGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := s.OAuth.Grants(r.Context(), auth.PrincipalFrom(r.Context()).UserID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": grants})
}

func (s *Server) handleListEveryonesOAuthGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := s.OAuth.EveryonesGrants(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": grants})
}

func (s *Server) handleEndOAuthGrant(w http.ResponseWriter, r *http.Request) {
	s.endGrant(w, r, s.OAuth.EndGrant)
}

func (s *Server) handleEndAnybodysOAuthGrant(w http.ResponseWriter, r *http.Request) {
	s.endGrant(w, r, s.OAuth.EndAnybodysGrant)
}

func (s *Server) endGrant(w http.ResponseWriter, r *http.Request, end func(context.Context, uuid.UUID, uuid.UUID) error) {
	id, apiErr := pathUUID(r, "grantID", "assistant")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if err := end(r.Context(), auth.PrincipalFrom(r.Context()).UserID, id); err != nil {
		respondError(w, r, consentError(err))
		return
	}
	respondNoContent(w)
}

func (s *Server) handleGetAssistants(w http.ResponseWriter, r *http.Request) {
	setting, err := s.OAuth.Setting(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, setting)
}

func (s *Server) handleSetAssistants(w http.ResponseWriter, r *http.Request) {
	var in oauth.SettingInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	setting, err := s.OAuth.Switch(r.Context(), in)
	if err != nil {
		respondError(w, r, consentError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, setting)
}

// isClientEndpoint says whether a path is one a client calls with its own credentials, never the session's.
func isClientEndpoint(path string) bool { return clientEndpoints[strings.TrimRight(path, "/")] }
