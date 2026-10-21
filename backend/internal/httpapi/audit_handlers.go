package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/keyset"
)

// The audit log's read side (#36), adapted from Exciter's audit handlers (b554258).

// AuditLog reads the log as the person on the context; *audit.Service is the real one.
type AuditLog interface {
	List(ctx context.Context, f audit.Filter, after *keyset.Cursor, limit int) ([]audit.Record, *string, error)
	Facets(ctx context.Context) (audit.Facets, error)
	Export(ctx context.Context, f audit.Filter, format string) ([]byte, int, error)
	Note(ctx context.Context, e audit.Entry) error
}

// auditDay is how a day is written in the log's date filters.
const auditDay = "2006-01-02"

var (
	errAuditOff = &APIError{Status: http.StatusServiceUnavailable, Code: "audit_unavailable",
		Message: "The audit log is not set up on this server. Ask its operator to configure it."}
	errAuditExportTooLarge = &APIError{Status: http.StatusUnprocessableEntity, Code: "export_too_large",
		Message: "That filter matches more than " + strconv.Itoa(config.AuditMaxExport) + " entries. Narrow the dates or choose an action, then export again."}
)

// auditFilter reads the log's filters. A day is the whole of that day in UTC; a time with its zone is that instant.
func auditFilter(r *http.Request) (audit.Filter, *APIError) {
	q := r.URL.Query()
	f := audit.Filter{TargetType: q.Get("targetType"), TargetID: q.Get("target")}
	problems := map[string]string{}
	if action := q.Get("action"); action != "" {
		if !slices.Contains(audit.Actions, action) {
			problems["action"] = "Choose one of the actions the log records."
		}
		f.Action = action
	}
	if raw := q.Get("actor"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			problems["actor"] = "Send a person's id, as the log's entries carry them."
		} else {
			f.ActorID = &id
		}
	}
	moment := func(name string, endOfDay bool) *time.Time {
		raw := q.Get(name)
		if raw == "" {
			return nil
		}
		if day, err := time.Parse(auditDay, raw); err == nil {
			if endOfDay {
				day = day.AddDate(0, 0, 1)
			}
			return &day
		}
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			problems[name] = "Write a day as YYYY-MM-DD, or a time with its zone such as 2026-10-01T00:00:00+02:00."
			return nil
		}
		return &at
	}
	f.From, f.To = moment("from", false), moment("to", true)
	if f.From != nil && f.To != nil && !f.From.Before(*f.To) {
		problems["to"] = "The end of the range has to come after its start."
	}
	if len(problems) > 0 {
		return f, ErrValidation(problems)
	}
	return f, nil
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	if s.Audit == nil {
		respondError(w, r, errAuditOff)
		return
	}
	f, apiErr := auditFilter(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	after, err := keyset.Decode(r.URL.Query().Get("cursor"))
	if err != nil {
		respondError(w, r, ErrValidation(map[string]string{"cursor": "Send back the cursor the window before answered, unchanged."}))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, next, err := s.Audit.List(r.Context(), f, after, limit)
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := auditWindow{Entries: make([]auditEntry, 0, len(entries)), Next: next}
	for _, e := range entries {
		out.Entries = append(out.Entries, toAuditEntry(e))
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleAuditFacets(w http.ResponseWriter, r *http.Request) {
	if s.Audit == nil {
		respondError(w, r, errAuditOff)
		return
	}
	facets, err := s.Audit.Facets(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := auditFacets{Actions: facets.Actions, TargetTypes: facets.TargetTypes, Actors: make([]personRef, 0, len(facets.Actors))}
	for _, a := range facets.Actors {
		out.Actors = append(out.Actors, personRef{ID: a.ID.String(), Name: a.Name})
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleExportAudit(w http.ResponseWriter, r *http.Request) {
	if s.Audit == nil {
		respondError(w, r, errAuditOff)
		return
	}
	f, apiErr := auditFilter(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = audit.FormatCSV
	}
	if format != audit.FormatCSV && format != audit.FormatJSON {
		respondError(w, r, ErrValidation(map[string]string{"format": "Choose csv or json."}))
		return
	}
	body, n, err := s.Audit.Export(r.Context(), f, format)
	if errors.Is(err, audit.ErrExportTooLarge) {
		respondError(w, r, errAuditExportTooLarge)
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	// The file leaves only once its leaving is recorded.
	data := f.Describe()
	data["entries"], data["format"] = n, format
	var actor uuid.UUID
	if p := auth.PrincipalFrom(r.Context()); p != nil {
		actor = p.UserID
	}
	if err := s.Audit.Note(r.Context(), audit.Entry{Action: audit.ActionAuditExported, TargetType: "audit_log", Actor: actor, Data: data}); err != nil {
		respondError(w, r, err)
		return
	}
	contentType := "text/csv; charset=utf-8"
	if format == audit.FormatJSON {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log.`+format+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func toAuditEntry(e audit.Record) auditEntry {
	out := auditEntry{ID: e.ID.String(), Action: e.Action, TargetType: e.TargetType, TargetID: e.TargetID, Data: e.Data,
		IP: e.IP, RequestID: e.RequestID, At: e.CreatedAt}
	if e.Via != "" {
		out.Via = &e.Via
	}
	if e.ActorID != nil {
		out.Actor = &personRef{ID: e.ActorID.String(), Name: e.ActorName}
	}
	return out
}
