package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/langsmith"
)

// LangSmith is whether runs' spans also go to LangSmith (#37); *langsmith.Service is the real one.
type LangSmith interface {
	Get(ctx context.Context) (langsmith.Setting, error)
	Set(ctx context.Context, in langsmith.Input) (langsmith.Setting, error)
	RemoveKey(ctx context.Context) (langsmith.Setting, error)
}

var (
	errLangSmithUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "langsmith_unavailable",
		Message: "LangSmith cannot be set up on this installation, since Rotor has no OpenBao to keep its key in."}
	errDataLeaves = &APIError{Status: http.StatusUnprocessableEntity, Code: "data_leaves_unacknowledged",
		Message: "Turning LangSmith on sends runs' prompts, answers and tool calls to LangSmith's servers, outside this installation. " +
			"Confirm that this is allowed, then turn it on again.",
		Fields: map[string]string{"acknowledgeDataLeaves": "Confirm that run data may leave the installation."}}
	errLangSmithKeyMissing = &APIError{Status: http.StatusUnprocessableEntity, Code: "langsmith_key_missing",
		Message: "LangSmith needs a key before it is turned on. Paste one made in LangSmith's settings.",
		Fields:  map[string]string{"key": "Paste a LangSmith key."}}
	errLangSmithInput = ErrValidation(map[string]string{
		"key":     "A LangSmith key is one word of at most 512 characters, as LangSmith shows it.",
		"project": "Name the project in at most 128 characters.",
	})
)

func langSmithError(err error) error {
	switch {
	case errors.Is(err, langsmith.ErrNotAcknowledged):
		return errDataLeaves
	case errors.Is(err, langsmith.ErrNoKey):
		return errLangSmithKeyMissing
	case errors.Is(err, langsmith.ErrBadInput):
		return errLangSmithInput
	}
	return err
}

func (s *Server) langSmithReady(w http.ResponseWriter, r *http.Request) bool {
	if s.LangSmith == nil {
		respondError(w, r, errLangSmithUnavailable)
		return false
	}
	return true
}

func (s *Server) handleGetLangSmith(w http.ResponseWriter, r *http.Request) {
	if !s.langSmithReady(w, r) {
		return
	}
	setting, err := s.LangSmith.Get(r.Context())
	s.answerLangSmith(w, r, setting, err)
}

func (s *Server) handleSetLangSmith(w http.ResponseWriter, r *http.Request) {
	var req langSmithRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.langSmithReady(w, r) {
		return
	}
	setting, err := s.LangSmith.Set(r.Context(), langsmith.Input{Enabled: req.Enabled, Project: req.Project, Key: req.Key,
		AcknowledgeDataLeaves: req.AcknowledgeDataLeaves})
	s.answerLangSmith(w, r, setting, err)
}

func (s *Server) handleRemoveLangSmithKey(w http.ResponseWriter, r *http.Request) {
	if !s.langSmithReady(w, r) {
		return
	}
	setting, err := s.LangSmith.RemoveKey(r.Context())
	s.answerLangSmith(w, r, setting, err)
}

func (s *Server) answerLangSmith(w http.ResponseWriter, r *http.Request, setting langsmith.Setting, err error) {
	if err != nil {
		respondError(w, r, langSmithError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, langSmithResponse{Enabled: setting.Enabled, Project: setting.Project, KeySet: setting.KeySetAt != nil,
		KeySetAt: setting.KeySetAt, ChangedBy: setting.ChangedBy, UpdatedAt: setting.UpdatedAt, Endpoint: s.LangSmithEndpoint})
}
