// Package metrics serves a process's counters and gauges in Prometheus's text format, written by hand: Rotor needs
// a handful of families, and a library comes with M7's observability.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Kinds of family, as the format names them.
const (
	kindCounter = "counter"
	kindGauge   = "gauge"
)

// contentType is the text format's media type, version 0.0.4.
const contentType = "text/plain; version=0.0.4; charset=utf-8"

// Family is one metric with at most one label; a value under "" is the metric without its label.
type Family struct {
	name, help, kind, label string
	mu                      sync.Mutex
	values                  map[string]float64
	// collect answers the values when they are read, for a gauge the process does not keep itself.
	collect func(ctx context.Context) (map[string]float64, error)
}

// Add adds to the value under one label value; a nil family counts nothing.
func (f *Family) Add(labelValue string, delta float64) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[labelValue] += delta
}

// Set sets the value under one label value; a nil family keeps nothing.
func (f *Family) Set(labelValue string, value float64) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[labelValue] = value
}

func (f *Family) snapshot(ctx context.Context) map[string]float64 {
	if f.collect != nil {
		collected, err := f.collect(ctx)
		if err != nil {
			return map[string]float64{}
		}
		return collected
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]float64, len(f.values))
	for k, v := range f.values {
		out[k] = v
	}
	return out
}

// Registry is the families one endpoint serves, in the order registered.
type Registry struct {
	mu       sync.Mutex
	families []*Family
}

func (r *Registry) add(f *Family) *Family {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.families = append(r.families, f)
	return f
}

// Counter registers a family that only goes up.
func (r *Registry) Counter(name, help, label string) *Family {
	return r.add(&Family{name: name, help: help, kind: kindCounter, label: label, values: map[string]float64{}})
}

// Gauge registers a family whose values the process sets.
func (r *Registry) Gauge(name, help, label string) *Family {
	return r.add(&Family{name: name, help: help, kind: kindGauge, label: label, values: map[string]float64{}})
}

// GaugeFunc registers a gauge read from collect at every scrape, such as a count the database keeps.
func (r *Registry) GaugeFunc(name, help, label string, collect func(ctx context.Context) (map[string]float64, error)) *Family {
	return r.add(&Family{name: name, help: help, kind: kindGauge, label: label, values: map[string]float64{}, collect: collect})
}

// Write writes every family in the text format, label values sorted; a collect that fails leaves its family empty.
func (r *Registry) Write(ctx context.Context, w io.Writer) error {
	r.mu.Lock()
	families := append([]*Family(nil), r.families...)
	r.mu.Unlock()
	var b strings.Builder
	for _, f := range families {
		values := f.snapshot(ctx)
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", f.name, f.help, f.name, f.kind)
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "" || f.label == "" {
				fmt.Fprintf(&b, "%s %v\n", f.name, values[k])
				continue
			}
			fmt.Fprintf(&b, "%s{%s=\"%s\"} %v\n", f.name, f.label, escape(k), values[k])
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// escape writes a label value as the format quotes it.
func escape(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

// Handler serves the registry at any path it is mounted on.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_ = r.Write(req.Context(), w)
	})
}

// Serve serves the registry at /metrics on addr until ctx ends; an empty addr serves nothing.
func Serve(ctx context.Context, addr string, r *Registry, log *slog.Logger) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", r.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: config.ReadHeaderTimeout}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.ShutdownGrace)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("metrics listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("the metrics endpoint stopped", "error", err)
	}
}
