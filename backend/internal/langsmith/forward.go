package langsmith

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
)

// destination is LangSmith as last read: whether it is on, and the exporter to it, until the setting changes.
type destination struct {
	enabled  bool
	exporter sdktrace.SpanExporter
	read     time.Time
}

// current is LangSmith's exporter while it is on, read again after config.LangSmithSettingTTL or a change here.
func (s *Service) current(ctx context.Context) (*destination, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && time.Since(s.cached.read) < config.LangSmithSettingTTL {
		return s.cached, nil
	}
	var enabled bool
	var project string
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT enabled, project FROM langsmith_setting`).Scan(&enabled, &project)
	})
	if err != nil {
		return nil, err
	}
	d := &destination{enabled: enabled, read: time.Now()}
	if enabled {
		key, _, err := s.keys.LangSmithKey(ctx)
		if err != nil {
			return nil, err
		}
		d.exporter, err = observability.NewExporter(ctx, s.endpoint, map[string]string{"x-api-key": key.Key, "Langsmith-Project": project})
		if err != nil {
			return nil, err
		}
	}
	if s.cached != nil && s.cached.exporter != nil {
		_ = s.cached.exporter.Shutdown(context.WithoutCancel(ctx))
	}
	s.cached = d
	return d, nil
}

// forget drops what was read, so the next spans go as the setting now says.
func (s *Service) forget() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = nil
}

// Enabled says whether runs' spans go to LangSmith now.
func (s *Service) Enabled(ctx context.Context) bool {
	d, err := s.current(ctx)
	return err == nil && d.enabled
}

// Forwarder sends a run's checked spans on: to the process's collector without the prompts and answers, and to
// LangSmith with them while an administrator allows it. It is the runner protocol's runnerctl.Telemetry.
type Forwarder struct {
	Tracing   *observability.Tracing
	LangSmith *Service
	Log       *slog.Logger
}

var _ runnerctl.Telemetry = (*Forwarder)(nil)

// Content says whether runs' LLM spans may carry the prompt and the answer: only while LangSmith is on.
func (f *Forwarder) Content(ctx context.Context) bool {
	return f.LangSmith != nil && f.LangSmith.Enabled(ctx)
}

// Spans sends a run's spans on; a destination that fails loses this batch alone, and says so at debug level.
func (f *Forwarder) Spans(ctx context.Context, runID uuid.UUID, spans []observability.RunnerSpan) {
	env := f.Tracing.Env()
	if err := f.Tracing.Export(ctx, observability.RunnerSnapshots(env, runnerctl.WithoutContent(spans))); err != nil {
		f.Log.Debug("the collector refused a runner's spans", "run", runID, "error", err)
	}
	if f.LangSmith == nil {
		return
	}
	d, err := f.LangSmith.current(ctx)
	if err != nil || !d.enabled {
		return
	}
	if err := d.exporter.ExportSpans(ctx, observability.RunnerSnapshots(env, spans)); err != nil {
		f.Log.Warn("LangSmith refused a run's spans", "run", runID, "error", err)
	}
}
