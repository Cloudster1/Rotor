package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces"
)

// TraceReader reads runs' traces and senders' own as the caller (#244); *traces.Service is the real one.
type TraceReader interface {
	RunTrace(ctx context.Context, runID uuid.UUID) (traces.Trace, []traces.Span, error)
	RunSpan(ctx context.Context, runID uuid.UUID, spanID string) (traces.Span, error)
	OwnedTrace(ctx context.Context, owner uuid.UUID, traceID string) (traces.Trace, []traces.Span, error)
	OwnedSpan(ctx context.Context, owner uuid.UUID, traceID, spanID string) (traces.Span, error)
	ListOwned(ctx context.Context, f traces.OwnedFilter) (traces.OwnedPage, error)
	SearchSpans(ctx context.Context, f traces.SpanFilter) (traces.SpanPage, error)
}

var (
	traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	spanIDPattern  = regexp.MustCompile(`^[0-9a-f]{16}$`)

	errTraceNotFound = ErrNotFound("There is no such trace, or you may not see it. A run's trace is read through its run.")
	errSpanNotFound  = ErrNotFound("There is no such span in this trace, or it has expired. Read the trace for its span ids.")
	errBadSpanID     = ErrBadRequest("A span id is 16 hex characters, as the trace names it. Check the address.")
	errBadTraceID    = ErrBadRequest("A trace id is 32 hex characters, as W3C trace context writes it. Check the address.")
)

func (s *Server) traceReadsReady(w http.ResponseWriter, r *http.Request) bool {
	if s.TraceReads == nil {
		respondError(w, r, errTracesUnavailable)
		return false
	}
	return true
}

// hexParam is a trace or span id from the path, in the lowercase W3C trace context writes.
func hexParam(r *http.Request, name string, pattern *regexp.Regexp, bad *APIError) (string, *APIError) {
	id := strings.ToLower(chi.URLParam(r, name))
	if !pattern.MatchString(id) {
		return "", bad
	}
	return id, nil
}

func (s *Server) handleGetRunTrace(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "runID", "run")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.traceReadsReady(w, r) {
		return
	}
	t, spans, err := s.TraceReads.RunTrace(r.Context(), id)
	if err != nil {
		respondError(w, r, runTraceError(err))
		return
	}
	roots := traces.Tree(spans)
	// An assistant reads a tree shortened to what a model can take; the browser reads it all.
	var tree traceTree
	if inProcessCall(r.Context()) {
		tree = forModel(t, roots)
	} else {
		tree = toTraceTree(t, roots, config.TracePreviewChars)
	}
	s.traceLinks(r.Context(), &tree)
	respondJSON(w, r, http.StatusOK, traceResponse{Trace: tree})
}

func (s *Server) handleGetRunTraceSpan(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "runID", "run")
	var spanID string
	if perr == nil {
		spanID, perr = hexParam(r, "spanID", spanIDPattern, errBadSpanID)
	}
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.traceReadsReady(w, r) {
		return
	}
	sp, err := s.TraceReads.RunSpan(r.Context(), id, spanID)
	if err != nil {
		respondError(w, r, runTraceError(err))
		return
	}
	out := traceSpanResponse{Span: toSpanDetail(sp)}
	if inProcessCall(r.Context()) {
		out = fitSpan(out)
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	sender, perr := optionalSender(r)
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.traceReadsReady(w, r) {
		return
	}
	f := traces.OwnedFilter{Sender: sender, Limit: queryInt(r, "limit", config.AccessPageSize, 1, config.AccessMaxPage),
		Offset: queryInt(r, "offset", 0, 0, config.AccessMaxOffset)}
	page, err := s.TraceReads.ListOwned(r.Context(), f)
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := traceList{Traces: make([]traceSummary, 0, len(page.Traces)), Total: page.Total, Limit: f.Limit, Offset: f.Offset}
	for _, o := range page.Traces {
		out.Traces = append(out.Traces, traceSummary{TraceID: o.TraceID, Sender: personRef{ID: o.Sender.ID.String(), Name: o.Sender.Name},
			Service: o.Service, RootName: o.RootName, Spans: o.Spans, Failed: o.Failed, Cut: o.Cut, StartedAt: o.StartedAt, EndedAt: o.EndedAt})
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	owner, traceID, ok := s.ownedTraceAddress(w, r)
	if !ok {
		return
	}
	t, spans, err := s.TraceReads.OwnedTrace(r.Context(), owner, traceID)
	if err != nil {
		respondError(w, r, ownedTraceError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, traceResponse{Trace: toTraceTree(t, traces.Tree(spans), config.TracePreviewChars)})
}

func (s *Server) handleGetTraceSpan(w http.ResponseWriter, r *http.Request) {
	spanID, perr := hexParam(r, "spanID", spanIDPattern, errBadSpanID)
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	owner, traceID, ok := s.ownedTraceAddress(w, r)
	if !ok {
		return
	}
	sp, err := s.TraceReads.OwnedSpan(r.Context(), owner, traceID, spanID)
	if err != nil {
		respondError(w, r, ownedTraceError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, traceSpanResponse{Span: toSpanDetail(sp)})
}

// ownedTraceAddress is whose trace the request names, the caller's unless an administrator says, and its trace id.
func (s *Server) ownedTraceAddress(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	traceID, perr := hexParam(r, "traceID", traceIDPattern, errBadTraceID)
	var sender *uuid.UUID
	if perr == nil {
		sender, perr = optionalSender(r)
	}
	if perr != nil {
		respondError(w, r, perr)
		return uuid.Nil, "", false
	}
	if !s.traceReadsReady(w, r) {
		return uuid.Nil, "", false
	}
	if sender != nil {
		return *sender, traceID, true
	}
	p := auth.PrincipalFrom(r.Context())
	if p == nil {
		respondError(w, r, ErrUnauthorized(""))
		return uuid.Nil, "", false
	}
	return p.UserID, traceID, true
}

func optionalSender(r *http.Request) (*uuid.UUID, *APIError) {
	raw := r.URL.Query().Get("sender")
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, ErrBadRequest("That is not a valid sender id. Give the sender's user id, or leave it out.")
	}
	return &id, nil
}

// runTraceError answers a run the caller may not read exactly as the run routes answer one that does not exist.
func runTraceError(err error) error {
	switch {
	case errors.Is(err, traces.ErrNotFound):
		return errRunNotFound
	case errors.Is(err, traces.ErrNoSpan):
		return errSpanNotFound
	}
	return err
}

func ownedTraceError(err error) error {
	switch {
	case errors.Is(err, traces.ErrNotFound):
		return errTraceNotFound
	case errors.Is(err, traces.ErrNoSpan):
		return errSpanNotFound
	}
	return err
}

// cutChars cuts s to at most n characters, never inside one; n below zero keeps it whole.
func cutChars(s string, n int) (string, bool) {
	if n < 0 || utf8.RuneCountInString(s) <= n {
		return s, false
	}
	i := 0
	for at := range s {
		if i == n {
			return s[:at], true
		}
		i++
	}
	return s, false
}
