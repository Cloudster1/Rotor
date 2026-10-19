package toolstub

import (
	"io"
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// LangSmithPath is where the stub takes OTLP/HTTP as LangSmith's OpenTelemetry endpoint does (#37), so a test sees
// exactly what Rotor would send LangSmith; the development stack points ROTOR_LANGSMITH_ENDPOINT here.
const LangSmithPath = "/langsmith/otel/v1/traces"

// LangSmithCall is one export that reached the stand-in, with the headers LangSmith reads it by.
type LangSmithCall struct {
	APIKey      string `json:"apiKey"`
	Project     string `json:"project"`
	ContentType string `json:"contentType"`
	// Body is the export as sent, protobuf; JSON carries it as base64.
	Body []byte `json:"body"`
}

// handleLangSmith keeps the export and answers as an OTLP receiver does, with an empty success.
func (s *Stub) handleLangSmith(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.ToolStubMaxOTLPBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "The export is larger than the stand-in keeps.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.langsmith) == config.ToolStubMaxOTLPCalls {
		s.langsmith = s.langsmith[1:]
	}
	s.langsmith = append(s.langsmith, LangSmithCall{APIKey: r.Header.Get("x-api-key"), Project: r.Header.Get("Langsmith-Project"),
		ContentType: r.Header.Get("Content-Type"), Body: body})
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

// handleLangSmithCalls answers every export the stand-in kept, oldest first.
func (s *Stub) handleLangSmithCalls(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"calls": append([]LangSmithCall{}, s.langsmith...)})
}

// handleForgetLangSmith forgets what reached the stand-in, so a test starts from nothing.
func (s *Stub) handleForgetLangSmith(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.langsmith = nil
	w.WriteHeader(http.StatusNoContent)
}
