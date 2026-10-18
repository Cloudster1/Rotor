package runs

import (
	"context"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/metrics"
)

// Why the worker destroyed a sandbox, as rotor_sandboxes_reaped_total counts it.
const (
	ReapWaiting   = "waiting"
	ReapEnded     = "ended"
	ReapOrphan    = "orphan"
	ReapUnstarted = "unstarted"
	ReapLost      = "lost"
	ReapDeadline  = "deadline"
)

// WorkerMetrics is what the worker counts: the sandboxes alive and those it reaped, by why.
type WorkerMetrics struct {
	live   *metrics.Family
	reaped *metrics.Family
}

// NewWorkerMetrics registers the worker's families, and the runs by status, read from the database at each scrape.
func NewWorkerMetrics(r *metrics.Registry, cluster *db.Cluster) *WorkerMetrics {
	m := &WorkerMetrics{
		live:   r.Gauge("rotor_sandboxes_live", "Sandboxes of this installation alive at the worker's last pass.", ""),
		reaped: r.Counter("rotor_sandboxes_reaped_total", "Sandboxes the worker destroyed, by why.", "reason"),
	}
	r.GaugeFunc("rotor_runs", "Runs by status.", "status", func(ctx context.Context) (map[string]float64, error) {
		out := map[string]float64{}
		err := cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			rows, err := tx.Query(ctx, `SELECT status, count(*) FROM run GROUP BY status`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var status string
				var n int64
				if err := rows.Scan(&status, &n); err != nil {
					return err
				}
				out[status] = float64(n)
			}
			return rows.Err()
		})
		return out, err
	})
	return m
}

func (m *WorkerMetrics) sawLive(n int) {
	if m != nil {
		m.live.Set("", float64(n))
	}
}

func (m *WorkerMetrics) reap(reason string) {
	if m != nil {
		m.reaped.Add(reason, 1)
	}
}
