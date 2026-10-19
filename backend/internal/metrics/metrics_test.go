package metrics

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheRegistryServesWhatPrometheusScrapes(t *testing.T) {
	r := &Registry{}
	live := r.Gauge("rotor_sandboxes_live", "Sandboxes alive now.", "")
	reaped := r.Counter("rotor_sandboxes_reaped_total", "Sandboxes destroyed, by why.", "reason")
	took := r.Histogram("rotor_run_duration_seconds", "How long runs took, by how they ended.", "status", []float64{1, 10})
	r.GaugeFunc("rotor_runs", "Runs by status.", "status", func(context.Context) (map[string]float64, error) {
		return map[string]float64{"running": 2, "waiting": 1}, nil
	})
	r.GaugeFunc("rotor_broken", "A family whose source fails.", "", func(context.Context) (map[string]float64, error) {
		return nil, errors.New("down")
	})
	live.Set("", 3)
	reaped.Add("waiting", 1)
	reaped.Add("waiting", 1)
	reaped.Add(`odd"one`, 1)
	took.Observe("succeeded", 5)

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	got := rec.Body.String()
	for _, want := range []string{
		"# TYPE rotor_sandboxes_live gauge",
		"rotor_sandboxes_live 3",
		"# TYPE rotor_sandboxes_reaped_total counter",
		`rotor_sandboxes_reaped_total{reason="odd\"one"} 1`,
		`rotor_sandboxes_reaped_total{reason="waiting"} 2`,
		`rotor_runs{status="running"} 2`,
		`rotor_runs{status="waiting"} 1`,
		`rotor_run_duration_seconds_bucket{status="succeeded",le="10"} 1`,
		`rotor_run_duration_seconds_count{status="succeeded"} 1`,
		"# TYPE go_goroutines gauge",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the exposition lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "rotor_broken ") {
		t.Errorf("a family whose source failed has a value:\n%s", got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content type %q", ct)
	}
	var none *Family
	none.Add("x", 1)
	none.Set("x", 1)
	none.Observe("x", 1)
}
