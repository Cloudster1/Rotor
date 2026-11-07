package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces"
)

var (
	errBadCursor = ErrBadRequest("That cursor is not one a page of this search answered. Search again without it.")
	// errSearchTooBroad is 422 rather than a server error: the same search, narrowed, answers.
	errSearchTooBroad = &APIError{Status: http.StatusUnprocessableEntity, Code: "search_too_broad",
		Message: "The search took too long. Narrow it: a shorter window, a kind, a workflow or failed spans only."}
)

const timeSentence = "Give a time as RFC 3339, such as 2026-11-01T09:00:00Z, or leave it out."

// spanSearchResponse is a page of spans a search found, newest first.
type spanSearchResponse struct {
	Spans []spanHit `json:"spans"`
	// Next is the cursor of the next page, null when no span is left.
	Next  *string   `json:"next"`
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
	Limit int       `json:"limit"`
}

// spanHit is a span a search found and the run or the sender's own trace it belongs to; never its text.
type spanHit struct {
	SpanID        string     `json:"spanId"`
	Name          string     `json:"name"`
	Kind          spanKind   `json:"kind"`
	Service       string     `json:"service"`
	Source        spanSource `json:"source"`
	Status        spanStatus `json:"status"`
	StatusMessage string     `json:"statusMessage"`
	StartedAt     time.Time  `json:"startedAt"`
	DurationMs    int64      `json:"durationMs"`
	Model         string     `json:"model"`
	TraceID       string     `json:"traceId"`
	// RunID and Workflow are the run's whose trace holds the span; a workflow's name is empty where it is not read.
	RunID    *string      `json:"runId"`
	Workflow *workflowRef `json:"workflow"`
	// Owner is the sender whose own trace holds the span, null for a run's.
	Owner *personRef `json:"owner"`
}

// workflowRef names a workflow.
type workflowRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) handleSearchSpans(w http.ResponseWriter, r *http.Request) {
	f, perr := spanFilter(r, time.Now())
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.traceReadsReady(w, r) {
		return
	}
	page, err := s.TraceReads.SearchSpans(r.Context(), f)
	if errors.Is(err, traces.ErrSearchTooBroad) {
		err = errSearchTooBroad
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := spanSearchResponse{Spans: make([]spanHit, 0, len(page.Hits)), Since: f.Since, Until: f.Until, Limit: f.Limit}
	// An assistant's page stops where a model's answer would grow too large, and goes on from there.
	budget := -1
	if inProcessCall(r.Context()) {
		budget = config.MCPTraceBudgetBytes
	}
	used := 0
	for i, h := range page.Hits {
		hit := toSpanHit(h)
		if budget >= 0 {
			raw, _ := json.Marshal(hit)
			if used += len(raw); used > budget && i > 0 {
				next := page.Hits[i-1].At.String()
				out.Next = &next
				break
			}
		}
		out.Spans = append(out.Spans, hit)
	}
	if out.Next == nil && page.More && len(page.Hits) > 0 {
		next := page.Hits[len(page.Hits)-1].At.String()
		out.Next = &next
	}
	respondJSON(w, r, http.StatusOK, out)
}

// spanFilter reads a search from the query, as of now; the window is always bounded, by default the last week.
func spanFilter(r *http.Request, now time.Time) (traces.SpanFilter, *APIError) {
	q := r.URL.Query()
	fields := map[string]string{}
	at := func(name string) *time.Time {
		raw := q.Get(name)
		if raw == "" {
			return nil
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fields[name] = timeSentence
			return nil
		}
		return &t
	}
	since, until := at("since"), at("until")
	f := traces.SpanFilter{Name: strings.TrimSpace(q.Get("name")), Model: strings.TrimSpace(q.Get("model")),
		Kind: strings.ToUpper(q.Get("kind")), Status: strings.ToLower(q.Get("status")), Service: q.Get("service"),
		AttrKey: q.Get("attrKey"), AttrValue: q.Get("attrValue"),
		Limit: queryInt(r, "limit", config.SpanSearchPageSize, 1, config.SpanSearchMaxPage)}
	if inProcessCall(r.Context()) {
		f.Limit = min(f.Limit, config.MCPSpanSearchMaxPage)
	}
	f.Since, f.Until = traces.Window(since, until, now)
	if raw := q.Get("workflow"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			fields["workflow"] = "That is not a valid workflow id. Choose the workflow again, or leave it out."
		}
		f.Workflow = &id
	}
	if len(fields) == 0 {
		for k, v := range f.Problems(now) {
			fields[k] = v
		}
	}
	if len(fields) > 0 {
		return f, ErrValidation(fields)
	}
	if raw := q.Get("cursor"); raw != "" {
		c, ok := traces.ParseCursor(raw)
		if !ok {
			return f, errBadCursor
		}
		f.After = &c
	}
	return f, nil
}

func toSpanHit(h traces.SpanHit) spanHit {
	out := spanHit{SpanID: h.SpanID, Name: h.Name, Kind: spanKind(h.Kind), Service: h.Service, Source: spanSource(h.Source),
		Status: spanStatus(h.Status), StatusMessage: h.StatusMessage, StartedAt: h.StartedAt,
		DurationMs: h.EndedAt.Sub(h.StartedAt).Milliseconds(), Model: h.Model, TraceID: h.TraceID}
	if h.RunID != nil {
		id := h.RunID.String()
		out.RunID = &id
	}
	if h.Workflow != nil {
		out.Workflow = &workflowRef{ID: h.Workflow.ID.String(), Name: h.Workflow.Name}
	}
	if h.Owner != nil {
		out.Owner = &personRef{ID: h.Owner.ID.String(), Name: h.Owner.Name}
	}
	return out
}
