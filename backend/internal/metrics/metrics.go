// Package metrics serves a process's counters, gauges and histograms for Prometheus to scrape, on client_golang
// (#37); families keep at most one label, which is all Rotor's need.
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Registry is the families one endpoint serves; its zero value is ready to use.
type Registry struct {
	once sync.Once
	reg  *prometheus.Registry
}

func (r *Registry) registry() *prometheus.Registry {
	r.once.Do(func() { r.reg = prometheus.NewRegistry() })
	return r.reg
}

// Family is one metric with at most one label; a value under "" is the metric without its label.
type Family struct {
	label     string
	counter   *prometheus.CounterVec
	gauge     *prometheus.GaugeVec
	histogram *prometheus.HistogramVec
}

func labels(label string) []string {
	if label == "" {
		return nil
	}
	return []string{label}
}

func (f *Family) values(labelValue string) []string {
	if f.label == "" {
		return nil
	}
	return []string{labelValue}
}

// Add adds to a counter or gauge under one label value; a nil family counts nothing.
func (f *Family) Add(labelValue string, delta float64) {
	switch {
	case f == nil:
	case f.counter != nil:
		f.counter.WithLabelValues(f.values(labelValue)...).Add(delta)
	case f.gauge != nil:
		f.gauge.WithLabelValues(f.values(labelValue)...).Add(delta)
	}
}

// Set sets a gauge under one label value; a nil family keeps nothing.
func (f *Family) Set(labelValue string, value float64) {
	if f != nil && f.gauge != nil {
		f.gauge.WithLabelValues(f.values(labelValue)...).Set(value)
	}
}

// Observe records one value of a histogram under one label value; a nil family records nothing.
func (f *Family) Observe(labelValue string, value float64) {
	if f != nil && f.histogram != nil {
		f.histogram.WithLabelValues(f.values(labelValue)...).Observe(value)
	}
}

// Counter registers a family that only goes up.
func (r *Registry) Counter(name, help, label string) *Family {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels(label))
	r.registry().MustRegister(c)
	return &Family{label: label, counter: c}
}

// Gauge registers a family whose values the process sets.
func (r *Registry) Gauge(name, help, label string) *Family {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels(label))
	r.registry().MustRegister(g)
	return &Family{label: label, gauge: g}
}

// Histogram registers a family of observations counted into buckets, such as how long runs take.
func (r *Registry) Histogram(name, help, label string, buckets []float64) *Family {
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets}, labels(label))
	r.registry().MustRegister(h)
	return &Family{label: label, histogram: h}
}

// GaugeFunc registers a gauge read from collect at every scrape, such as a count the database keeps; a collect
// that fails leaves the family empty for that scrape.
func (r *Registry) GaugeFunc(name, help, label string, collect func(ctx context.Context) (map[string]float64, error)) {
	r.registry().MustRegister(&funcGauge{desc: prometheus.NewDesc(name, help, labels(label), nil), label: label, collect: collect})
}

type funcGauge struct {
	desc    *prometheus.Desc
	label   string
	collect func(ctx context.Context) (map[string]float64, error)
}

func (g *funcGauge) Describe(ch chan<- *prometheus.Desc) { ch <- g.desc }

func (g *funcGauge) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), config.MetricsCollectTimeout)
	defer cancel()
	values, err := g.collect(ctx)
	if err != nil {
		return
	}
	for k, v := range values {
		var lvs []string
		if g.label != "" {
			lvs = []string{k}
		}
		ch <- prometheus.MustNewConstMetric(g.desc, prometheus.GaugeValue, v, lvs...)
	}
}

// Handler serves the registry at any path it is mounted on.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry(), promhttp.HandlerOpts{})
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
