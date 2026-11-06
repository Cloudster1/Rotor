package tracedest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
)

// target is an enabled destination as last read, ready to send to: its headers, if it names any, are set.
type target struct {
	id       uuid.UUID
	name     string
	endpoint string
	headers  bool
	content  bool
	link     *string
	updated  time.Time
}

// exporter sends to one destination as it was when made; a change to the row makes another.
type exporter struct {
	updated time.Time
	export  sdktrace.SpanExporter
}

// sending is the enabled destinations, read again after config.TraceSettingTTL or a change on this replica; one that
// cannot be read keeps what was read last.
func (s *Service) sending(ctx context.Context) []target {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.read.IsZero() && s.now().Sub(s.read) < config.TraceSettingTTL {
		return s.targets
	}
	var out []target
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		out = out[:0]
		rows, err := tx.Query(ctx, `SELECT id, name, endpoint, cardinality(header_names) > 0, content, trace_link, updated_at
			FROM trace_destination WHERE enabled AND (cardinality(header_names) = 0 OR headers_set_at IS NOT NULL) ORDER BY lower(name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t target
			if err := rows.Scan(&t.id, &t.name, &t.endpoint, &t.headers, &t.content, &t.link, &t.updated); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if err != nil {
		if s.log != nil {
			s.log.Debug("could not read the trace destinations", "error", err)
		}
		return s.targets
	}
	// An exporter of a destination removed or changed is done with; one being used finishes its batch first.
	keep := make(map[uuid.UUID]time.Time, len(out))
	for _, t := range out {
		keep[t.id] = t.updated
	}
	for id, e := range s.exporters {
		if updated, ok := keep[id]; !ok || !updated.Equal(e.updated) {
			delete(s.exporters, id)
			go func() { _ = e.export.Shutdown(context.WithoutCancel(ctx)) }()
		}
	}
	s.targets, s.read = out, s.now()
	return out
}

// exporter is the one sending to t, made with its headers from OpenBao on first use.
func (s *Service) exporter(ctx context.Context, t target) (sdktrace.SpanExporter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.exporters[t.id]; ok && e.updated.Equal(t.updated) {
		return e.export, nil
	}
	var headers map[string]string
	if t.headers {
		h, err := s.headers.DestinationHeaders(ctx, t.id)
		if err != nil {
			return nil, err
		}
		headers = h.Headers
	}
	export, err := observability.NewExporter(ctx, t.endpoint, headers)
	if err != nil {
		return nil, err
	}
	if old, ok := s.exporters[t.id]; ok {
		go func() { _ = old.export.Shutdown(context.WithoutCancel(ctx)) }()
	}
	s.exporters[t.id] = &exporter{updated: t.updated, export: export}
	return export, nil
}

// forget drops what was read, so the next spans go as the destinations now say.
func (s *Service) forget() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.read = time.Time{}
}

// Link opens a run's trace at a destination.
type Link struct {
	Destination string
	URL         string
}

// Links are the enabled destinations' links to a run's trace, by name; senders' own traces go nowhere, so have none.
func (s *Service) Links(ctx context.Context, traceID string, runID uuid.UUID) []Link {
	out := []Link{}
	for _, t := range s.sending(ctx) {
		if t.link != nil {
			out = append(out, Link{Destination: t.name, URL: fillLink(*t.link, traceID, runID.String())})
		}
	}
	return out
}

// Forwarder sends a run's checked spans on: to the process's collector without their text, and to every enabled
// destination, with text where an administrator allowed it. It is the runner protocol's runnerctl.Telemetry.
type Forwarder struct {
	Tracing      *observability.Tracing
	Destinations *Service
	Log          *slog.Logger
}

var _ runnerctl.Telemetry = (*Forwarder)(nil)

// Content says whether a destination the run's trace goes to may get its spans' text, so the runner sends it.
func (f *Forwarder) Content(ctx context.Context, choice observability.RunTracing, traceID trace.TraceID) bool {
	if f.Destinations == nil || choice.NoText {
		return false
	}
	for _, t := range f.Destinations.sending(ctx) {
		if t.content && choice.SendsTo(traceID, t.id) {
			return true
		}
	}
	return false
}

// Spans sends a run's spans on, each place side by side within ctx's deadline; one that fails loses this batch alone.
// The destinations are those the run's workflow chose, without text where it chose none (#259).
func (f *Forwarder) Spans(ctx context.Context, runID uuid.UUID, choice observability.RunTracing, spans []observability.RunnerSpan) {
	if len(spans) == 0 {
		return
	}
	env := f.Tracing.Env()
	bare := observability.WithoutText(spans)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Go(func() {
		// Every run is traced since #242; the operator's collector still takes a share as the sample ratio says.
		if !f.Tracing.Keeps(spans[0].TraceID) {
			return
		}
		if err := f.Tracing.Export(ctx, observability.RunnerSnapshots(env, bare)); err != nil {
			f.Log.Debug("the collector refused a runner's spans", "run", runID, "error", err)
		}
	})
	if f.Destinations == nil {
		return
	}
	for _, t := range f.Destinations.sending(ctx) {
		if !choice.SendsTo(spans[0].TraceID, t.id) {
			continue
		}
		wg.Go(func() {
			batch := bare
			if t.content && !choice.NoText {
				batch = spans
			}
			export, err := f.Destinations.exporter(ctx, t)
			if err == nil {
				err = export.ExportSpans(ctx, observability.RunnerSnapshots(env, batch))
			}
			if err != nil {
				f.Log.Warn("a trace destination refused a run's spans", "destination", t.name, "run", runID, "error", err)
			}
		})
	}
}
