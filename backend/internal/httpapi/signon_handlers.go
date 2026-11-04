package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/oidc"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

// JoinRequests lets in or turns away who waits after signing in; *perm.Store is the real one.
type JoinRequests interface {
	JoinRequests(ctx context.Context) ([]perm.JoinRequest, error)
	AdmitJoinRequest(ctx context.Context, actor, userID uuid.UUID, admin bool) (*perm.User, error)
	DeclineJoinRequest(ctx context.Context, actor, userID uuid.UUID) error
}

// SingleSignOn keeps the identity provider an administrator sets up; *oidc.Service is the real one.
type SingleSignOn interface {
	Provider(ctx context.Context) (*oidc.Provider, error)
	Save(ctx context.Context, in oidc.ProviderInput) (*oidc.Provider, error)
	RedirectURL() string
	CanTurnOff() bool
}

var (
	errSignOnUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "sign_on_unavailable",
		Message: "Single sign-on cannot be read or changed right now. Try again in a moment."}
	errOnlyWayIn = &APIError{Status: http.StatusConflict, Code: "only_way_in",
		Message: "Single sign-on is the only way into Rotor: turned off, nobody could sign in again, you included, and only a " +
			"change in the database would bring it back. Change the issuer or the client instead."}
	errNoSecretStore = &APIError{Status: http.StatusUnprocessableEntity, Code: "no_secret_store",
		Message: "Rotor has no OpenBao to keep a client secret in, so it signs in with ROTOR_OIDC_CLIENT_SECRET alone. " +
			"Ask an operator to set ROTOR_OPENBAO_ADDR, or keep the issuer and client the environment names and leave the secret blank.",
		Fields: map[string]string{"clientSecret": "Leave it blank while Rotor has no OpenBao."}}
	errNoSuchRequest = ErrNotFound("Nobody by that name is waiting to be let in. Reload the page to see who is.")
)

func toJoinRequestView(r perm.JoinRequest) joinRequestView {
	return joinRequestView{UserID: r.UserID.String(), Email: r.Email, Name: r.Name, RequestedAt: r.RequestedAt}
}

func (s *Server) joinRequestsReady(w http.ResponseWriter, r *http.Request) bool {
	if s.JoinRequests == nil {
		respondError(w, r, errAccessUnavailable)
		return false
	}
	return true
}

func joinRequestError(err error) error {
	if errors.Is(err, perm.ErrNoSuchRequest) {
		return errNoSuchRequest
	}
	return accessError(err)
}

func (s *Server) handleListJoinRequests(w http.ResponseWriter, r *http.Request) {
	if !s.joinRequestsReady(w, r) {
		return
	}
	requests, err := s.JoinRequests.JoinRequests(r.Context())
	if err != nil {
		respondError(w, r, joinRequestError(err))
		return
	}
	out := make([]joinRequestView, 0, len(requests))
	for _, each := range requests {
		out = append(out, toJoinRequestView(each))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"requests": out})
}

func (s *Server) handleAdmitJoinRequest(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "userID", "person")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req admitRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			respondError(w, r, err)
			return
		}
	}
	switch req.Role {
	case "", admitMember, admitAdmin:
	default:
		respondError(w, r, ErrValidation(map[string]string{"role": "Let them in as member or as admin."}))
		return
	}
	if !s.joinRequestsReady(w, r) {
		return
	}
	user, err := s.JoinRequests.AdmitJoinRequest(r.Context(), actorOf(r), id, req.Role == admitAdmin)
	if err != nil {
		respondError(w, r, joinRequestError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"user": toUserView(*user)})
}

func (s *Server) handleDeclineJoinRequest(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "userID", "person")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.joinRequestsReady(w, r) {
		return
	}
	if err := s.JoinRequests.DeclineJoinRequest(r.Context(), actorOf(r), id); err != nil {
		respondError(w, r, joinRequestError(err))
		return
	}
	respondNoContent(w)
}

func (s *Server) signOnReady(w http.ResponseWriter, r *http.Request) bool {
	if s.SingleSignOn == nil {
		respondError(w, r, errSignOnUnavailable)
		return false
	}
	return true
}

func (s *Server) handleGetOIDCProvider(w http.ResponseWriter, r *http.Request) {
	if !s.signOnReady(w, r) {
		return
	}
	provider, err := s.SingleSignOn.Provider(r.Context())
	s.answerOIDCProvider(w, r, provider, err)
}

func (s *Server) handleSaveOIDCProvider(w http.ResponseWriter, r *http.Request) {
	var req saveOIDCProviderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.signOnReady(w, r) {
		return
	}
	enabled := req.Enabled == nil || *req.Enabled
	provider, err := s.SingleSignOn.Save(r.Context(), oidc.ProviderInput{Issuer: req.Issuer, ClientID: req.ClientID,
		ClientSecret: req.ClientSecret, GroupsClaim: req.GroupsClaim, AdmitByGroup: req.AdmitByGroup, Enabled: enabled})
	s.answerOIDCProvider(w, r, provider, err)
}

func (s *Server) answerOIDCProvider(w http.ResponseWriter, r *http.Request, provider *oidc.Provider, err error) {
	var refused *oidc.ProviderError
	switch {
	case errors.As(err, &refused):
		respondError(w, r, &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: refused.Message,
			Fields: refused.Fields})
		return
	case errors.Is(err, oidc.ErrOnlyWayIn):
		respondError(w, r, errOnlyWayIn)
		return
	case errors.Is(err, oidc.ErrNoSecretStore):
		respondError(w, r, errNoSecretStore)
		return
	case err != nil:
		respondError(w, r, err)
		return
	}
	out := oidcProviderResponse{RedirectURL: s.SingleSignOn.RedirectURL(), CanTurnOff: s.SingleSignOn.CanTurnOff()}
	if provider != nil {
		out.Provider = &oidcProviderView{Issuer: provider.Issuer, ClientID: provider.ClientID, HasSecret: provider.HasSecret,
			GroupsClaim: provider.GroupsClaim, AdmitByGroup: provider.AdmitByGroup, Enabled: provider.Enabled,
			ChangedBy: provider.ChangedBy, UpdatedAt: provider.UpdatedAt}
	}
	respondJSON(w, r, http.StatusOK, out)
}
