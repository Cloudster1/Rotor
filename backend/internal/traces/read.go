package traces

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
)

// What Rotor holds of a trace, as Trace.State says it.
const (
	StateKept    = "kept"
	StateExpired = "expired"
	StateNone    = "none"
)

var (
	// ErrNotFound is a run or an own trace the caller may not read, answered as one that does not exist.
	ErrNotFound = errors.New("no such trace")
	// ErrNoSpan is a span the trace does not hold, or no longer does.
	ErrNoSpan = errors.New("no such span")
)

// Trace is what Rotor holds of one trace, read as the caller; Owner is set for a sender's own trace, RunID for a run's.
type Trace struct {
	TraceID       string
	RunID         *uuid.UUID
	Owner         *Sender
	State         string
	Content, Cut  bool
	Dropped       int
	Spans         int
	Bytes         int64
	StartedAt     *time.Time
	EndedAt       *time.Time
	ExpiresAt     *time.Time
	row           uuid.UUID
	key           string
	sealedForRun  bool
	lastSpanAt    time.Time
	retentionDays int
}

// Span is one kept span; Text holds its text attributes, opened, and is nil when none was kept.
type Span struct {
	SpanID, ParentSpanID string
	Name, Kind           string
	Service, Source      string
	Sender               *Sender
	StartedAt, EndedAt   time.Time
	Status               string
	StatusMessage        string
	Attributes           map[string]string
	Events               []Event
	Text                 map[string]string
}

// traceColumns are a trace row and the retention its expiry is reckoned with.
const traceColumns = `t.id, t.trace_id, coalesce(t.data_key, ''), t.spans, t.bytes, t.dropped, t.cut_at IS NOT NULL, t.content,
	t.started_at, t.last_span_at, t.expired_at, (SELECT retention_days FROM trace_setting)`

const spanColumns = `span_id, parent_span_id, name, kind, service, source, sender_id, coalesce(sender_name, ''), started_at, ended_at,
	status, status_message, attributes, events, sealed_content`

// RunTrace is a run's trace and its spans, read as the caller, so row level security decides; a run the caller may
// not read is ErrNotFound, as one that does not exist.
func (s *Service) RunTrace(ctx context.Context, runID uuid.UUID) (Trace, []Span, error) {
	return s.read(ctx, func(ctx context.Context, tx db.DBTX) (Trace, bool, error) { return runTrace(ctx, tx, runID) }, "")
}

// RunSpan is one span of a run's trace, whole.
func (s *Service) RunSpan(ctx context.Context, runID uuid.UUID, spanID string) (Span, error) {
	_, spans, err := s.read(ctx, func(ctx context.Context, tx db.DBTX) (Trace, bool, error) { return runTrace(ctx, tx, runID) }, spanID)
	return oneSpan(spans, err)
}

// OwnedTrace is owner's own trace of traceID, read as the caller: the sender, or an administrator.
func (s *Service) OwnedTrace(ctx context.Context, owner uuid.UUID, traceID string) (Trace, []Span, error) {
	return s.read(ctx, func(ctx context.Context, tx db.DBTX) (Trace, bool, error) { return ownedTrace(ctx, tx, owner, traceID) }, "")
}

// OwnedSpan is one span of owner's own trace, whole.
func (s *Service) OwnedSpan(ctx context.Context, owner uuid.UUID, traceID, spanID string) (Span, error) {
	_, spans, err := s.read(ctx, func(ctx context.Context, tx db.DBTX) (Trace, bool, error) {
		return ownedTrace(ctx, tx, owner, traceID)
	}, spanID)
	return oneSpan(spans, err)
}

func oneSpan(spans []Span, err error) (Span, error) {
	if err != nil {
		return Span{}, err
	}
	if len(spans) == 0 {
		return Span{}, ErrNoSpan
	}
	return spans[0], nil
}

// read finds the trace and its spans, all of them or the one spanID names, in one transaction, then opens their text.
func (s *Service) read(ctx context.Context, find func(context.Context, db.DBTX) (Trace, bool, error), spanID string) (Trace, []Span, error) {
	var t Trace
	var spans []Span
	var sealed [][]byte
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var found bool
		var err error
		if t, found, err = find(ctx, tx); err != nil || !found || t.State == StateExpired {
			return err
		}
		spans, sealed, err = readSpans(ctx, tx, t.row, spanID)
		return err
	})
	if err != nil {
		return Trace{}, nil, err
	}
	finish(&t, spans)
	return t, spans, s.open(ctx, t, spans, sealed)
}

func runTrace(ctx context.Context, tx db.DBTX, runID uuid.UUID) (Trace, bool, error) {
	var runTraceID, runKey string
	var row *uuid.UUID
	t := Trace{RunID: &runID, sealedForRun: true}
	var traceID *string
	var spans, dropped, days *int
	var bytes *int64
	var cut, content *bool
	var started, last, expired *time.Time
	err := tx.QueryRow(ctx, `SELECT coalesce(r.trace_id, ''), coalesce(r.data_key, ''), t.id, t.trace_id, t.spans,
			t.bytes, t.dropped, t.cut_at IS NOT NULL, t.content, t.started_at, t.last_span_at, t.expired_at,
			(SELECT retention_days FROM trace_setting)
		FROM run r LEFT JOIN trace t ON t.run_id = r.id WHERE r.id = $1`, runID).
		Scan(&runTraceID, &runKey, &row, &traceID, &spans, &bytes, &dropped, &cut, &content, &started, &last, &expired, &days)
	if errors.Is(err, pgx.ErrNoRows) {
		return Trace{}, false, ErrNotFound
	}
	if err != nil {
		return Trace{}, false, err
	}
	t.TraceID, t.key, t.State = runTraceID, runKey, StateNone
	if row == nil {
		return t, false, nil
	}
	t.row, t.TraceID, t.Spans, t.Bytes, t.Dropped, t.Cut, t.Content = *row, *traceID, *spans, *bytes, *dropped, *cut, *content
	t.StartedAt, t.lastSpanAt, t.retentionDays = started, *last, *days
	t.State = state(expired != nil, t.Spans)
	return t, true, nil
}

func ownedTrace(ctx context.Context, tx db.DBTX, owner uuid.UUID, traceID string) (Trace, bool, error) {
	t := Trace{Owner: &Sender{ID: owner}}
	var started time.Time
	var expired *time.Time
	err := tx.QueryRow(ctx, `SELECT `+traceColumns+`, `+ownerName+` FROM trace t WHERE t.owner_id = $1 AND t.trace_id = $2`,
		owner, traceID).Scan(&t.row, &t.TraceID, &t.key, &t.Spans, &t.Bytes, &t.Dropped, &t.Cut, &t.Content, &started,
		&t.lastSpanAt, &expired, &t.retentionDays, &t.Owner.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Trace{}, false, ErrNotFound
	}
	if err != nil {
		return Trace{}, false, err
	}
	t.StartedAt, t.State = &started, state(expired != nil, t.Spans)
	return t, true, nil
}

// ownerName names an own trace's sender as its spans recorded them, since rotor_app reads no one else's account.
const ownerName = `coalesce((SELECT s.sender_name FROM trace_span s WHERE s.trace = t.id AND s.sender_name IS NOT NULL
	ORDER BY s.received_at DESC LIMIT 1), (SELECT u.name FROM app_user u WHERE u.id = t.owner_id), '')`

func state(expired bool, spans int) string {
	switch {
	case expired:
		return StateExpired
	case spans == 0:
		return StateNone
	}
	return StateKept
}

func readSpans(ctx context.Context, tx db.DBTX, trace uuid.UUID, spanID string) ([]Span, [][]byte, error) {
	rows, err := tx.Query(ctx, `SELECT `+spanColumns+` FROM trace_span WHERE trace = $1 AND ($2 = '' OR span_id = $2)
		ORDER BY started_at, span_id`, trace, spanID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var spans []Span
	var sealed [][]byte
	for rows.Next() {
		var sp Span
		var senderID *uuid.UUID
		var senderName string
		var attrs, events, content []byte
		if err := rows.Scan(&sp.SpanID, &sp.ParentSpanID, &sp.Name, &sp.Kind, &sp.Service, &sp.Source, &senderID, &senderName,
			&sp.StartedAt, &sp.EndedAt, &sp.Status, &sp.StatusMessage, &attrs, &events, &content); err != nil {
			return nil, nil, err
		}
		if senderID != nil {
			sp.Sender = &Sender{ID: *senderID, Name: senderName}
		}
		if err := json.Unmarshal(attrs, &sp.Attributes); err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(events, &sp.Events); err != nil {
			return nil, nil, err
		}
		spans, sealed = append(spans, sp), append(sealed, content)
	}
	return spans, sealed, rows.Err()
}

// finish says when the trace began, ended and expires, from its spans where it holds any.
func finish(t *Trace, spans []Span) {
	if t.row == uuid.Nil {
		return
	}
	expires := t.lastSpanAt.AddDate(0, 0, t.retentionDays)
	t.ExpiresAt = &expires
	for i, sp := range spans {
		if i == 0 || sp.StartedAt.Before(*t.StartedAt) {
			start := sp.StartedAt
			t.StartedAt = &start
		}
		if t.EndedAt == nil || sp.EndedAt.After(*t.EndedAt) {
			end := sp.EndedAt
			t.EndedAt = &end
		}
	}
}

// open unseals the spans' text with the run's key, or the own trace's; nothing is asked of OpenBao when none was kept.
func (s *Service) open(ctx context.Context, t Trace, spans []Span, sealed [][]byte) error {
	var box *envelope.Box
	for i := range spans {
		if sealed[i] == nil || s.Keys == nil || t.key == "" {
			continue
		}
		if box == nil {
			var err error
			owner := t.row
			if t.sealedForRun {
				owner = *t.RunID
			}
			if box, err = s.Keys.For(ctx, owner, t.key); err != nil {
				return err
			}
		}
		plain, err := box.Open(envelope.ColumnTraceContent, envelope.TraceSpanRow(t.row, spans[i].SpanID), sealed[i])
		if err != nil {
			return err
		}
		if err := json.Unmarshal(plain, &spans[i].Text); err != nil {
			return err
		}
	}
	return nil
}
