package toolstub

import (
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// OTLPPath is where the stub takes OTLP/HTTP as any trace destination does (#246), one sink per name, so each test
// points a destination at a sink of its own and reads back exactly what Rotor sent it.
const OTLPPath = "/otlp/{sink}/v1/traces"

// OTLPCall is one export that reached a sink, with every header it came with.
type OTLPCall struct {
	Headers     map[string]string `json:"headers"`
	ContentType string            `json:"contentType"`
	// Body is the export as sent, protobuf; JSON carries it as base64.
	Body []byte `json:"body"`
}

// handleOTLP keeps the export under its sink and answers as an OTLP receiver does, with an empty success.
func (s *Stub) handleOTLP(w http.ResponseWriter, r *http.Request) {
	sink := chi.URLParam(r, "sink")
	if !tenantName.MatchString(sink) {
		writeError(w, http.StatusNotFound, "not_found", "Name the sink in lower case letters, digits and dashes.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.ToolStubMaxOTLPBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "The export is larger than the stand-in keeps.")
		return
	}
	headers := make(map[string]string, len(r.Header))
	for k := range r.Header {
		headers[strings.ToLower(k)] = r.Header.Get(k)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.otlp == nil || (s.otlp[sink] == nil && len(s.otlp) == config.ToolStubMaxOTLPSinks) {
		s.otlp = map[string][]OTLPCall{}
	}
	calls := s.otlp[sink]
	if len(calls) == config.ToolStubMaxOTLPCalls {
		calls = calls[1:]
	}
	s.otlp[sink] = append(calls, OTLPCall{Headers: headers, ContentType: r.Header.Get("Content-Type"), Body: body})
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

// handleOTLPCalls answers every export a sink kept, oldest first.
func (s *Stub) handleOTLPCalls(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"calls": append([]OTLPCall{}, s.otlp[chi.URLParam(r, "sink")]...)})
}

// handleForgetOTLP forgets what reached a sink, so a test starts from nothing.
func (s *Stub) handleForgetOTLP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.otlp, chi.URLParam(r, "sink"))
	w.WriteHeader(http.StatusNoContent)
}
