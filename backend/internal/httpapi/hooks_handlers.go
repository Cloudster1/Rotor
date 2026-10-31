package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/hooks"
	"github.com/cloudster1/rotor/backend/internal/tools"
)

// Hooks keeps the tools' webhook secrets and takes their deliveries (#35); *hooks.Service is the real one.
type Hooks interface {
	Get(ctx context.Context, toolID uuid.UUID) (hooks.Hook, error)
	SetSecret(ctx context.Context, toolID uuid.UUID, secret string) (hooks.Hook, error)
	RemoveSecret(ctx context.Context, toolID uuid.UUID) (hooks.Hook, error)
	Receive(ctx context.Context, toolID uuid.UUID, header http.Header, body []byte) (hooks.Outcome, error)
}

var (
	errHooksUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "hooks_unavailable",
		Message: "Tool events are not set up on this installation. An operator connects Rotor to OpenBao."}
	errHookNotFound = ErrNotFound("There is no such tool, or Rotor holds no webhook secret for it. Its administrator adds the secret under Tools.")
	errNotProduct   = &APIError{Status: http.StatusUnprocessableEntity, Code: "hook_not_product",
		Message: "Rotor takes events only from Armature, Exciter and Stator; this tool is none of them."}
	errBadHookSecret = ErrValidation(map[string]string{"secret": "That is not this tool's webhook secret. Copy the whole secret the tool showed, which starts with its name and _whs_."})
	errBadSignature  = &APIError{Status: http.StatusUnauthorized, Code: "signature_invalid",
		Message: "The delivery's signature is missing, wrong or older than five minutes. Check the secret Rotor holds and the sender's clock."}
	errHookTooLarge = &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
		Message: "The delivery is larger than Rotor takes. Send fewer topics to this webhook."}
	errUnknownEnvelope = &APIError{Status: http.StatusUnprocessableEntity, Code: "envelope_unknown",
		Message: "Rotor does not read this envelope version. Set the webhook to version 1, or 2 for Exciter."}
)

// hookError says what went wrong with a hook in the words its caller needs.
func hookError(err error) error {
	var lacking *tools.Missing
	switch {
	case errors.As(err, &lacking):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "tool_lacks", Message: lacking.Message}
	case errors.Is(err, hooks.ErrNotFound):
		return errHookNotFound
	case errors.Is(err, hooks.ErrNotProduct):
		return errNotProduct
	case errors.Is(err, hooks.ErrBadSecret):
		return errBadHookSecret
	case errors.Is(err, hooks.ErrBadSignature):
		return errBadSignature
	case errors.Is(err, hooks.ErrUnknownVersion), errors.Is(err, hooks.ErrNotEnvelope):
		return errUnknownEnvelope
	}
	return err
}

func (s *Server) hooksReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Hooks == nil {
		respondError(w, r, errHooksUnavailable)
		return false
	}
	return true
}

// hookURL is where a tool delivers its events to Rotor, under the address Rotor is opened at.
func (s *Server) hookURL(toolID uuid.UUID) string {
	return strings.TrimSuffix(s.AppBaseURL, "/") + APIPrefix + "/hooks/" + toolID.String()
}

func (s *Server) toHookResponse(h hooks.Hook) hookResponse {
	word := config.HookProducts[h.Product]
	return hookResponse{URL: s.hookURL(h.ToolID), Product: word,
		SecretSet: h.SecretSetAt != nil, SecretSetAt: h.SecretSetAt, LastReceivedAt: h.LastReceivedAt}
}

func (s *Server) handleGetHook(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.hooksReady(w, r) {
		return
	}
	h, err := s.Hooks.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, hookError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, s.toHookResponse(h))
}

func (s *Server) handleSetHookSecret(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req hookSecretRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.hooksReady(w, r) {
		return
	}
	h, err := s.Hooks.SetSecret(r.Context(), id, req.Secret)
	if err != nil {
		respondError(w, r, hookError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, s.toHookResponse(h))
}

func (s *Server) handleRemoveHookSecret(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.hooksReady(w, r) {
		return
	}
	h, err := s.Hooks.RemoveSecret(r.Context(), id)
	if err != nil && !errors.Is(err, hooks.ErrNotProduct) {
		respondError(w, r, hookError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, s.toHookResponse(h))
}

// handleReceiveHook takes a tool's delivery; nobody is signed in, the tool's signature vouches for it.
func (s *Server) handleReceiveHook(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.hooksReady(w, r) {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.MaxHookBody))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		respondError(w, r, errHookTooLarge)
		return
	}
	if err != nil {
		respondError(w, r, ErrBadRequest("The delivery's body could not be read."))
		return
	}
	outcome, err := s.Hooks.Receive(r.Context(), id, r.Header, body)
	if err != nil {
		respondError(w, r, hookError(err))
		return
	}
	switch outcome {
	case hooks.Duplicate:
		respondJSON(w, r, http.StatusOK, statusResponse{Status: "duplicate"})
	case hooks.Ping:
		respondJSON(w, r, http.StatusOK, statusResponse{Status: "pong"})
	default:
		respondJSON(w, r, http.StatusAccepted, statusResponse{Status: "accepted"})
	}
}
