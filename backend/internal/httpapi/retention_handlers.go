package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/retention"
)

// Retention is how long a finished run's data is kept (#41); *retention.Service is the real one.
type Retention interface {
	Get(ctx context.Context) (retention.Setting, error)
	Set(ctx context.Context, days int) (retention.Setting, error)
}

var errRetentionUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "retention_unavailable",
	Message: "Retention is not set up on this installation. An operator connects Rotor to its database and starts its worker."}

func (s *Server) retentionReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Retention == nil {
		respondError(w, r, errRetentionUnavailable)
		return false
	}
	return true
}

func (s *Server) handleGetRetention(w http.ResponseWriter, r *http.Request) {
	if !s.retentionReady(w, r) {
		return
	}
	setting, err := s.Retention.Get(r.Context())
	answerRetention(w, r, setting, err)
}

func (s *Server) handleSetRetention(w http.ResponseWriter, r *http.Request) {
	var req retentionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.retentionReady(w, r) {
		return
	}
	setting, err := s.Retention.Set(r.Context(), req.RunDataDays)
	if errors.Is(err, retention.ErrDays) {
		err = ErrValidation(map[string]string{"runDataDays": retention.DaysSentence})
	}
	answerRetention(w, r, setting, err)
}

func answerRetention(w http.ResponseWriter, r *http.Request, setting retention.Setting, err error) {
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, retentionResponse{RunDataDays: setting.RunDataDays, MaxDays: config.MaxRunDataRetentionDays,
		ChangedBy: setting.ChangedBy, UpdatedAt: setting.UpdatedAt})
}
