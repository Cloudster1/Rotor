package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/traces"
)

// TraceSettings is how long runs' traces are kept and whether their text is (#242); *traces.Service is the real one.
type TraceSettings interface {
	Get(ctx context.Context) (traces.Setting, error)
	Set(ctx context.Context, days int, keepContent bool) (traces.Setting, error)
}

var errTracesUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "traces_unavailable",
	Message: "Traces are not kept on this installation. An operator connects Rotor to its database."}

func (s *Server) tracesReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Traces == nil {
		respondError(w, r, errTracesUnavailable)
		return false
	}
	return true
}

func (s *Server) handleGetTraceSetting(w http.ResponseWriter, r *http.Request) {
	if !s.tracesReady(w, r) {
		return
	}
	setting, err := s.Traces.Get(r.Context())
	answerTraceSetting(w, r, setting, err)
}

func (s *Server) handleSetTraceSetting(w http.ResponseWriter, r *http.Request) {
	var req traceSettingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.tracesReady(w, r) {
		return
	}
	setting, err := s.Traces.Set(r.Context(), req.RetentionDays, req.KeepContent)
	if errors.Is(err, traces.ErrDays) {
		err = ErrValidation(map[string]string{"retentionDays": traces.DaysSentence})
	}
	answerTraceSetting(w, r, setting, err)
}

func answerTraceSetting(w http.ResponseWriter, r *http.Request, setting traces.Setting, err error) {
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, traceSettingResponse{RetentionDays: setting.RetentionDays, KeepContent: setting.KeepContent,
		ChangedBy: setting.ChangedBy, UpdatedAt: setting.UpdatedAt})
}
