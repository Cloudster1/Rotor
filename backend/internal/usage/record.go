// Package usage totals what runs use, per run, person and UTC day, prices it at the run's model's prices, and says
// when a run or a person passed a limit (#38). docs/api-contract-m7-usage.md describes it.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// Reasons a run ends for when it passes a limit, as the run's reason says them.
const (
	ReasonMaxTokens   = "max_tokens"
	ReasonMaxCost     = "max_cost"
	ReasonDailyTokens = "daily_tokens"
	ReasonDailyCost   = "daily_cost"
)

// EventUsage is the runner's event for one model call's usage.
const EventUsage = "llm.usage"

// reasonSentences say each reason to whoever reads the run.
var reasonSentences = map[string]string{
	ReasonMaxTokens:   "The run's model calls used more tokens than its limits allow, counted across every sandbox it used. Raise limits.maxTokens if it needs more.",
	ReasonMaxCost:     "The run's model calls cost more than its limits allow. Raise limits.maxCost if it needs more.",
	ReasonDailyTokens: "The person who started the run used up today's tokens. The quota starts again at midnight UTC, or an administrator can raise it.",
	ReasonDailyCost:   "The person who started the run used up today's budget. The quota starts again at midnight UTC, or an administrator can raise it.",
}

// Sentence says a reason to whoever reads the run.
func Sentence(reason string) string { return reasonSentences[reason] }

// Totals are a run's usage so far and its person's today, after a model call was added.
type Totals struct {
	RunTokens, DayTokens int64
	RunCost, DayCost     float64
}

// Limit is the installation's limits on one person; nil is no limit.
type Limit struct {
	DailyTokens    *int64
	DailyCost      *float64
	ConcurrentRuns *int
}

// Open keeps the prices the run's model has now with the run's usage, once: a price changed during a run leaves
// its accounting as it was. A run without a model, or a model without prices, counts tokens only.
func Open(ctx context.Context, tx db.DBTX, runID uuid.UUID, providerID *uuid.UUID, model string) error {
	_, err := tx.Exec(ctx, `INSERT INTO run_usage (run_id, user_id, price_in, price_out, priced)
		SELECT r.id, r.user_id, COALESCE(p.input_per_million, 0), COALESCE(p.output_per_million, 0), p.provider_id IS NOT NULL
		FROM run r LEFT JOIN model_price p ON p.provider_id = $2 AND p.model = $3
		WHERE r.id = $1
		ON CONFLICT (run_id) DO NOTHING`, runID, providerID, model)
	return err
}

// Agent is the model one agent node of a run uses, resolved to its provider (#133).
type Agent struct {
	Node         string
	ProviderID   uuid.UUID
	ProviderName string
	Model        string
}

// OpenAgents keeps, once, the model each agent uses and the prices it has now, so each call is counted at its own
// model's price.
func OpenAgents(ctx context.Context, tx db.DBTX, runID uuid.UUID, agents []Agent) error {
	for _, a := range agents {
		_, err := tx.Exec(ctx, `INSERT INTO run_usage_agent (run_id, node_id, provider_id, provider_name, model, price_in, price_out, priced)
			SELECT $1, $2, $3, $4, $5, COALESCE(p.input_per_million, 0), COALESCE(p.output_per_million, 0), p.provider_id IS NOT NULL
			FROM (SELECT 1) one LEFT JOIN model_price p ON p.provider_id = $3 AND p.model = $5
			ON CONFLICT (run_id, node_id) DO NOTHING`, runID, a.Node, a.ProviderID, a.ProviderName, a.Model)
		if err != nil {
			return err
		}
	}
	return nil
}

// call is one model call's usage, as the runner reports it.
type call struct {
	Model        string `json:"model"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
}

// Add counts one model call's usage toward its run, its agent and its person's day, in the event's transaction so a
// resent event counts once; it is priced at the model kept for its agent node, else at the run's (#133).
func Add(ctx context.Context, tx db.DBTX, runID uuid.UUID, node string, payload []byte) (Totals, error) {
	var c call
	_ = json.Unmarshal(payload, &c)
	in, out := clamp(c.InputTokens), clamp(c.OutputTokens)
	var t Totals
	var user uuid.UUID
	var delta float64
	if _, err := tx.Exec(ctx, `INSERT INTO run_usage (run_id, user_id) SELECT id, user_id FROM run WHERE id = $1
		ON CONFLICT (run_id) DO NOTHING`, runID); err != nil {
		return t, err
	}
	// The agent's cost goes on as text, so the run's total adds the very amount its agent's did.
	var agentCost *string
	err := tx.QueryRow(ctx, `UPDATE run_usage_agent SET input_tokens = input_tokens + $3, output_tokens = output_tokens + $4,
			cost = cost + ($3 * price_in + $4 * price_out) / $5, updated_at = now()
		WHERE run_id = $1 AND node_id = $2
		RETURNING (($3 * price_in + $4 * price_out) / $5)::text`, runID, node, in, out, config.TokensPerPriceUnit).Scan(&agentCost)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return t, err
	}
	err = tx.QueryRow(ctx, `UPDATE run_usage SET input_tokens = input_tokens + $2, output_tokens = output_tokens + $3,
			cost = cost + COALESCE($5::text::numeric, ($2 * price_in + $3 * price_out) / $4), updated_at = now()
		WHERE run_id = $1
		RETURNING user_id, input_tokens + output_tokens, cost::float8, COALESCE($5::text::numeric, ($2 * price_in + $3 * price_out) / $4)::float8`,
		runID, in, out, config.TokensPerPriceUnit, agentCost).Scan(&user, &t.RunTokens, &t.RunCost, &delta)
	if err != nil {
		return t, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO usage_daily (user_id, day, input_tokens, output_tokens, cost)
		VALUES ($1, (now() AT TIME ZONE 'UTC')::date, $2, $3, $4)
		ON CONFLICT (user_id, day) DO UPDATE SET input_tokens = usage_daily.input_tokens + EXCLUDED.input_tokens,
			output_tokens = usage_daily.output_tokens + EXCLUDED.output_tokens, cost = usage_daily.cost + EXCLUDED.cost
		RETURNING input_tokens + output_tokens, cost::float8`, user, in, out, delta).Scan(&t.DayTokens, &t.DayCost)
	return t, err
}

func clamp(n int64) int64 {
	return min(max(n, 0), config.MaxUsageTokensPerCall)
}

// Limits reads the installation's limits on one person.
func Limits(ctx context.Context, tx db.DBTX) (Limit, error) {
	var l Limit
	err := tx.QueryRow(ctx, `SELECT daily_tokens, daily_cost::float8, concurrent_runs FROM usage_limit`).Scan(
		&l.DailyTokens, &l.DailyCost, &l.ConcurrentRuns)
	return l, err
}

// Over names the first limit the totals passed, or nothing: the run's own, then its person's day.
func Over(t Totals, run spec.Limits, person Limit) string {
	switch {
	case run.MaxTokens > 0 && t.RunTokens > int64(run.MaxTokens):
		return ReasonMaxTokens
	case run.MaxCost > 0 && t.RunCost > run.MaxCost:
		return ReasonMaxCost
	case person.DailyTokens != nil && t.DayTokens > *person.DailyTokens:
		return ReasonDailyTokens
	case person.DailyCost != nil && t.DayCost > *person.DailyCost:
		return ReasonDailyCost
	}
	return ""
}

// Detail says what a run's totals were when it passed a limit, for its run.finished event.
func Detail(t Totals) map[string]any {
	return map[string]any{"tokens": t.RunTokens, "cost": fmt.Sprintf("%.6f", t.RunCost)}
}
