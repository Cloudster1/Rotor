package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/preferences"
)

// Preferences are what a person chooses for themselves (#206); *preferences.Service is the real one.
type Preferences interface {
	Get(ctx context.Context) (preferences.Preferences, error)
	Set(ctx context.Context, in preferences.Preferences) (preferences.Preferences, error)
}

var errPreferencesUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "preferences_unavailable",
	Message: "Preferences are not set up on this installation. An operator connects Rotor to its database."}

func (s *Server) preferencesReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Preferences == nil {
		respondError(w, r, errPreferencesUnavailable)
		return false
	}
	return true
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	if !s.preferencesReady(w, r) {
		return
	}
	got, err := s.Preferences.Get(r.Context())
	answerPreferences(w, r, got, err)
}

func (s *Server) handleSetPreferences(w http.ResponseWriter, r *http.Request) {
	var req preferencesBody
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.preferencesReady(w, r) {
		return
	}
	in := preferences.Preferences{}
	if req.TimeZone != nil {
		in.TimeZone = *req.TimeZone
	}
	got, err := s.Preferences.Set(r.Context(), in)
	var invalid *preferences.Invalid
	switch {
	case errors.As(err, &invalid):
		err = ErrValidation(invalid.Fields)
	case errors.Is(err, preferences.ErrNotInPerson):
		err = ErrForbidden("Change your preferences yourself, signed in to Rotor; an assistant cannot change them for you.")
	}
	answerPreferences(w, r, got, err)
}

func answerPreferences(w http.ResponseWriter, r *http.Request, got preferences.Preferences, err error) {
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := preferencesBody{}
	if got.TimeZone != "" {
		out.TimeZone = &got.TimeZone
	}
	respondJSON(w, r, http.StatusOK, out)
}
