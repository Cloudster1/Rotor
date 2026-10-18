package metrics

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheRegistryWritesPrometheusTextFormat(t *testing.T) {
	r := &Registry{}
	live := r.Gauge("rotor_sandboxes_live", "Sandboxes alive now.", "")
	reaped := r.Counter("rotor_sandboxes_reaped_total", "Sandboxes destroyed, by why.", "reason")
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

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	want := strings.Join([]string{
		"# HELP rotor_sandboxes_live Sandboxes alive now.",
		"# TYPE rotor_sandboxes_live gauge",
		"rotor_sandboxes_live 3",
		"# HELP rotor_sandboxes_reaped_total Sandboxes destroyed, by why.",
		"# TYPE rotor_sandboxes_reaped_total counter",
		`rotor_sandboxes_reaped_total{reason="odd\"one"} 1`,
		`rotor_sandboxes_reaped_total{reason="waiting"} 2`,
		"# HELP rotor_runs Runs by status.",
		"# TYPE rotor_runs gauge",
		`rotor_runs{status="running"} 2`,
		`rotor_runs{status="waiting"} 1`,
		"# HELP rotor_broken A family whose source fails.",
		"# TYPE rotor_broken gauge",
		"",
	}, "\n")
	if got := rec.Body.String(); got != want {
		t.Errorf("the exposition is\n%s\nwant\n%s", got, want)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("content type %q", ct)
	}
	var none *Family
	none.Add("x", 1)
	none.Set("x", 1)
}
