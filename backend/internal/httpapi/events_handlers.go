package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/runs"
)

// eventsSuffix ends the one path that streams, which the request timeout leaves alone.
const eventsSuffix = "/events"

// timeoutExceptStreams bounds every request by timeout, except a run's event stream, which lasts as long as the run.
func timeoutExceptStreams(timeout time.Duration) func(http.Handler) http.Handler {
	bounded := middleware.Timeout(timeout)
	return func(next http.Handler) http.Handler {
		limited := bounded(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, APIPrefix+"/runs/") && strings.HasSuffix(r.URL.Path, eventsSuffix) {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}

// sseStream writes a run's events as server-sent events, each flushed as it is written.
type sseStream struct {
	w       http.ResponseWriter
	control *http.ResponseController
}

func (s *sseStream) Event(e runs.Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, data); err != nil {
		return err
	}
	return s.control.Flush()
}

func (s *sseStream) Heartbeat() error {
	if _, err := fmt.Fprint(s.w, ": still here\n\n"); err != nil {
		return err
	}
	return s.control.Flush()
}

// handleRunEvents streams a run's events from the start, or after Last-Event-ID (or ?after=), until the run ends.
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "runID", "run")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	after := r.Header.Get("Last-Event-ID")
	if after == "" {
		after = r.URL.Query().Get("after")
	}
	var from int64
	if after != "" {
		n, err := strconv.ParseInt(after, 10, 64)
		if err != nil || n < 0 {
			respondError(w, r, ErrBadRequest("Last-Event-ID is not an event's number. Send the id of the last event you received."))
			return
		}
		from = n
	}
	if !s.runsReady(w, r) {
		return
	}
	// Refusals still answer as JSON: the run is checked before the stream begins.
	if _, err := s.Runs.Get(r.Context(), id); err != nil {
		respondError(w, r, runError(err))
		return
	}
	control := http.NewResponseController(w)
	// The server's write timeout would cut a stream that lasts as long as its run.
	if err := control.SetWriteDeadline(time.Time{}); err != nil {
		loggerFrom(r.Context()).Warn("cannot lift the write deadline of an event stream", "error", err)
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", config.EventRetryMillis); err != nil {
		return
	}
	if err := s.Runs.Follow(r.Context(), id, from, &sseStream{w: w, control: control}); err != nil && r.Context().Err() == nil {
		loggerFrom(r.Context()).Warn("a run's event stream ended early", "run", id, "error", err)
	}
}
