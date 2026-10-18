package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/providers"
)

// Providers is the language model providers; *providers.Service is the real one.
type Providers interface {
	List(ctx context.Context) ([]providers.Provider, error)
	Add(ctx context.Context, in providers.Input) (providers.Provider, error)
	Change(ctx context.Context, id uuid.UUID, in providers.Input) (providers.Provider, error)
	Remove(ctx context.Context, id uuid.UUID) error
	Test(ctx context.Context, id uuid.UUID) (providers.TestResult, error)
	Models(ctx context.Context) ([]providers.Model, error)
}

var (
	errNoSuchProvider = ErrNotFound("There is no such provider. Reload the settings to see the ones Rotor has.")
	errProvidersOff   = &APIError{Status: http.StatusServiceUnavailable, Code: "providers_unavailable",
		Message: "Model providers are not set up on this installation. An operator sets ROTOR_OPENBAO_ADDR to OpenBao."}
)

func providerError(err error) error {
	var refusal *providers.Refusal
	switch {
	case errors.As(err, &refusal):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "provider_refused", Message: refusal.Message}
	case errors.Is(err, providers.ErrNotFound):
		return errNoSuchProvider
	}
	return err
}

func toProviderResponse(p providers.Provider) providerResponse {
	return providerResponse{ID: p.ID.String(), Name: p.Name, Kind: providerKind(p.Kind), BaseURL: p.BaseURL, APIVersion: p.APIVersion,
		Models: p.Models, DefaultModel: p.DefaultModel, IsDefault: p.IsDefault, Enabled: p.Enabled, HasKey: p.HasKey}
}

func toInput(req providerRequest) providers.Input {
	return providers.Input{Name: req.Name, Kind: string(req.Kind), BaseURL: req.BaseURL, APIVersion: req.APIVersion,
		Models: req.Models, DefaultModel: req.DefaultModel, IsDefault: req.IsDefault, Enabled: req.Enabled, APIKey: req.APIKey}
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	if s.Providers == nil {
		respondError(w, r, errProvidersOff)
		return
	}
	list, err := s.Providers.List(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := make([]providerResponse, 0, len(list))
	for _, p := range list {
		out = append(out, toProviderResponse(p))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"providers": out})
}

func (s *Server) handleAddProvider(w http.ResponseWriter, r *http.Request) {
	var req providerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if s.Providers == nil {
		respondError(w, r, errProvidersOff)
		return
	}
	p, err := s.Providers.Add(r.Context(), toInput(req))
	if err != nil {
		respondError(w, r, providerError(err))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"provider": toProviderResponse(p)})
}

func (s *Server) handleChangeProvider(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "providerID", "provider")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req providerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if s.Providers == nil {
		respondError(w, r, errProvidersOff)
		return
	}
	p, err := s.Providers.Change(r.Context(), id, toInput(req))
	if err != nil {
		respondError(w, r, providerError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"provider": toProviderResponse(p)})
}

func (s *Server) handleRemoveProvider(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "providerID", "provider")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if s.Providers == nil {
		respondError(w, r, errProvidersOff)
		return
	}
	if err := s.Providers.Remove(r.Context(), id); err != nil {
		respondError(w, r, providerError(err))
		return
	}
	respondNoContent(w)
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "providerID", "provider")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if s.Providers == nil {
		respondError(w, r, errProvidersOff)
		return
	}
	result, err := s.Providers.Test(r.Context(), id)
	if err != nil {
		respondError(w, r, providerError(err))
		return
	}
	models := result.Models
	if models == nil {
		models = []string{}
	}
	respondJSON(w, r, http.StatusOK, providerTestResponse{OK: result.OK, Message: result.Message, Models: models})
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	if s.Providers == nil {
		respondJSON(w, r, http.StatusOK, map[string]any{"models": []modelResponse{}})
		return
	}
	list, err := s.Providers.Models(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := make([]modelResponse, 0, len(list))
	for _, m := range list {
		out = append(out, modelResponse{Provider: personRef{ID: m.ProviderID.String(), Name: m.ProviderName}, Name: m.Name, Default: m.Default})
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"models": out})
}
