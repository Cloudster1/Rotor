package runs

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// AgentUsage is the model one agent node of a run ran on, and what its calls used at that model's prices (#133).
type AgentUsage struct {
	Node                      string
	Provider                  string
	Model                     string
	InputTokens, OutputTokens int64
	Cost                      float64
}

// agentUsage reads a run's agents in node order, as whoever sees the run.
func agentUsage(ctx context.Context, tx db.DBTX, runID uuid.UUID) ([]AgentUsage, error) {
	rows, err := tx.Query(ctx, `SELECT node_id, provider_name, model, input_tokens, output_tokens, cost::float8
		FROM run_usage_agent WHERE run_id = $1 ORDER BY node_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentUsage{}
	for rows.Next() {
		var a AgentUsage
		if err := rows.Scan(&a.Node, &a.Provider, &a.Model, &a.InputTokens, &a.OutputTokens, &a.Cost); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
