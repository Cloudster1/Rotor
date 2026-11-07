package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/tracedest"
)

// TraceDestinations are the OTLP destinations runs' traces are sent to (#246); *tracedest.Service is the real one.
type TraceDestinations interface {
	List(ctx context.Context) ([]tracedest.Destination, error)
	Get(ctx context.Context, id uuid.UUID) (tracedest.Destination, error)
	Add(ctx context.Context, in tracedest.Input) (tracedest.Destination, error)
	Change(ctx context.Context, id uuid.UUID, in tracedest.Input) (tracedest.Destination, error)
	Remove(ctx context.Context, id uuid.UUID) error
	Test(ctx context.Context, id uuid.UUID) (tracedest.TestResult, error)
	Links(ctx context.Context, traceID string, runID uuid.UUID) []tracedest.Link
}

var (
	errTraceDestinationsUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "trace_destinations_unavailable",
		Message: "Trace destinations cannot be set up on this installation, since Rotor has no OpenBao to keep their headers in."}
	errTraceDestinationNotFound = ErrNotFound("There is no such trace destination. List them for their ids.")
	errContentLeaves            = &APIError{Status: http.StatusUnprocessableEntity, Code: "content_leaves_unacknowledged",
		Message: "Sending text sends runs' prompts, tool calls and answers to this destination, outside Rotor. " +
			"Confirm that this is allowed, then save again.",
		Fields: map[string]string{"acknowledgeContentLeaves": "Confirm that prompt, tool and answer text may leave Rotor for this destination."}}
	errTraceDestinationTaken = &APIError{Status: http.StatusConflict, Code: "trace_destination_taken",
		Message: "Another trace destination has this name. Choose another.", Fields: map[string]string{"name": "Choose a name no other destination has."}}
	errTraceDestinationsFull = &APIError{Status: http.StatusUnprocessableEntity, Code: "trace_destinations_full",
		Message: "Rotor sends to at most " + strconv.Itoa(config.MaxTraceDestinations) + " trace destinations. Remove one before adding another."}
)

func traceDestinationError(err error) error {
	var invalid *tracedest.Invalid
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(invalid.Fields)
	case errors.Is(err, tracedest.ErrNotFound):
		return errTraceDestinationNotFound
	case errors.Is(err, tracedest.ErrNotAcknowledged):
		return errContentLeaves
	case errors.Is(err, tracedest.ErrTaken):
		return errTraceDestinationTaken
	case errors.Is(err, tracedest.ErrFull):
		return errTraceDestinationsFull
	}
	return err
}

func (s *Server) traceDestinationsReady(w http.ResponseWriter, r *http.Request) bool {
	if s.TraceDestinations == nil {
		respondError(w, r, errTraceDestinationsUnavailable)
		return false
	}
	return true
}

func (s *Server) handleListTraceDestinations(w http.ResponseWriter, r *http.Request) {
	if !s.traceDestinationsReady(w, r) {
		return
	}
	list, err := s.TraceDestinations.List(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := traceDestinationList{Destinations: make([]traceDestination, 0, len(list))}
	for _, d := range list {
		out.Destinations = append(out.Destinations, toTraceDestination(d))
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleGetTraceDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := s.destinationID(w, r)
	if !ok {
		return
	}
	d, err := s.TraceDestinations.Get(r.Context(), id)
	answerTraceDestination(w, r, http.StatusOK, d, err)
}

func (s *Server) handleAddTraceDestination(w http.ResponseWriter, r *http.Request) {
	var req traceDestinationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.traceDestinationsReady(w, r) {
		return
	}
	d, err := s.TraceDestinations.Add(r.Context(), req.input())
	answerTraceDestination(w, r, http.StatusCreated, d, err)
}

func (s *Server) handleChangeTraceDestination(w http.ResponseWriter, r *http.Request) {
	var req traceDestinationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	id, ok := s.destinationID(w, r)
	if !ok {
		return
	}
	d, err := s.TraceDestinations.Change(r.Context(), id, req.input())
	answerTraceDestination(w, r, http.StatusOK, d, err)
}

func (s *Server) handleRemoveTraceDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := s.destinationID(w, r)
	if !ok {
		return
	}
	if err := s.TraceDestinations.Remove(r.Context(), id); err != nil {
		respondError(w, r, traceDestinationError(err))
		return
	}
	respondNoContent(w)
}

func (s *Server) handleTestTraceDestination(w http.ResponseWriter, r *http.Request) {
	id, ok := s.destinationID(w, r)
	if !ok {
		return
	}
	result, err := s.TraceDestinations.Test(r.Context(), id)
	if err != nil {
		respondError(w, r, traceDestinationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, traceDestinationTest{Delivered: result.Delivered, Status: result.Status, Message: result.Message})
}

// destinationID is the path's destination, once destinations can be kept at all.
func (s *Server) destinationID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, perr := pathUUID(r, "destinationID", "trace destination")
	if perr != nil {
		respondError(w, r, perr)
		return uuid.Nil, false
	}
	return id, s.traceDestinationsReady(w, r)
}

// traceLinks are the destinations' links to a run's trace; a trace Rotor holds nothing of was sent nowhere either.
func (s *Server) traceLinks(ctx context.Context, tree *traceTree) {
	if s.TraceDestinations == nil || tree.RunID == nil || tree.State == "none" {
		return
	}
	runID, err := uuid.Parse(*tree.RunID)
	if err != nil {
		return
	}
	for _, l := range s.TraceDestinations.Links(ctx, tree.TraceID, runID) {
		tree.Links = append(tree.Links, traceLink{Destination: l.Destination, URL: l.URL})
	}
}

func (req traceDestinationRequest) input() tracedest.Input {
	return tracedest.Input{Name: req.Name, Endpoint: req.Endpoint, Headers: req.Headers, Enabled: req.Enabled,
		Content: req.Content, AcknowledgeContentLeaves: req.AcknowledgeContentLeaves, TraceLink: req.TraceLink}
}

func answerTraceDestination(w http.ResponseWriter, r *http.Request, status int, d tracedest.Destination, err error) {
	if err != nil {
		respondError(w, r, traceDestinationError(err))
		return
	}
	respondJSON(w, r, status, traceDestinationResponse{Destination: toTraceDestination(d)})
}

func toTraceDestination(d tracedest.Destination) traceDestination {
	return traceDestination{ID: d.ID.String(), Name: d.Name, Endpoint: d.Endpoint, HeaderNames: d.HeaderNames,
		HeadersSetAt: d.HeadersSetAt, Enabled: d.Enabled, Content: d.Content, ContentAcknowledgedBy: d.ContentAcknowledgedBy,
		ContentAcknowledgedAt: d.ContentAcknowledgedAt, TraceLink: d.TraceLink, CarriedOver: d.CarriedOver,
		ChangedBy: d.ChangedBy, UpdatedAt: d.UpdatedAt}
}
