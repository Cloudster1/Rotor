package usage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/providers"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// Refusal codes for a run that may not start.
const (
	CodeQuotaReached  = "quota_reached"
	CodeUnpricedModel = "unpriced_model"
)

// ErrNotFound is a provider that does not exist.
var ErrNotFound = errors.New("no such provider")

// Refused is a run that may not start for its person's quota or its model's prices, said in a sentence.
type Refused struct{ Code, Message string }

func (e *Refused) Error() string { return e.Message }

// Invalid is a change to the limits or prices that does not fit, field by field.
type Invalid struct{ Fields map[string]string }

func (e *Invalid) Error() string { return "the limits do not fit" }

// Models resolves a workflow's model; *providers.Service is the real one.
type Models interface {
	Resolve(ctx context.Context, wanted *spec.Model) (providers.Resolution, error)
}

// Service reads usage and limits as the person on the context, and checks whether a run may start.
type Service struct {
	db     *db.Cluster
	models Models
}

// NewService returns the usage; models may be nil, which skips the check that a maxCost has prices to go by.
func NewService(cluster *db.Cluster, models Models) *Service {
	return &Service{db: cluster, models: models}
}

// CheckStart refuses a run its person may not start: today's quota is used up, or the workflow bounds its cost on
// a model without prices.
func (s *Service) CheckStart(ctx context.Context, userID uuid.UUID, sp *spec.Spec) error {
	var limit Limit
	var tokens int64
	var cost float64
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if limit, err = Limits(ctx, tx); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT COALESCE(sum(input_tokens + output_tokens), 0), COALESCE(sum(cost), 0)::float8
			FROM usage_daily WHERE user_id = $1 AND day = (now() AT TIME ZONE 'UTC')::date`, userID).Scan(&tokens, &cost)
	})
	if err != nil {
		return err
	}
	if limit.DailyTokens != nil && tokens >= *limit.DailyTokens {
		return &Refused{CodeQuotaReached, fmt.Sprintf("You used today's %d tokens. The quota starts again at midnight UTC, or an administrator can raise it.", *limit.DailyTokens)}
	}
	if limit.DailyCost != nil && cost >= *limit.DailyCost {
		return &Refused{CodeQuotaReached, fmt.Sprintf("You used today's budget of %.2f US dollars. The quota starts again at midnight UTC, or an administrator can raise it.", *limit.DailyCost)}
	}
	if s.models == nil {
		return nil
	}
	resolved, err := s.runModels(ctx, sp)
	if err != nil || sp.Limits.MaxCost <= 0 {
		return err
	}
	for _, r := range resolved {
		var priced bool
		err = s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM model_price WHERE provider_id = $1 AND model = $2)`,
				r.Provider.ID, r.Model).Scan(&priced)
		})
		if err != nil {
			return err
		}
		if !priced {
			return &Refused{CodeUnpricedModel, fmt.Sprintf("The workflow bounds its cost, but %s has no prices to count it by. Ask an administrator to set them, or remove limits.maxCost.", r.Model)}
		}
	}
	return nil
}

// runModels resolves every model a run's agents use, refusing an agent's own model no administrator allows (#133); a
// workflow's model that resolves to none is left out, and its agents fail with model_missing, which says more.
func (s *Service) runModels(ctx context.Context, sp *spec.Spec) ([]providers.Resolution, error) {
	var out []providers.Resolution
	agents := sp.AgentModels()
	if sp.UsesWorkflowModel() || len(agents) == 0 {
		if r, err := s.models.Resolve(ctx, sp.Model); err == nil {
			out = append(out, r)
		}
	}
	ids := make([]string, 0, len(agents))
	for id := range agents {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		wanted := agents[id]
		r, err := s.models.Resolve(ctx, &wanted)
		if err != nil {
			return nil, &Refused{spec.CodeModelNotAllowed, fmt.Sprintf("The agent %s uses %s, which no administrator allows now. Its workflow's owner must choose another model, or an administrator allows this one.", id, wanted.Describe())}
		}
		out = append(out, r)
	}
	return out, nil
}

// Day is what a person used in one UTC day, or in a span of days.
type Day struct {
	Day          time.Time
	InputTokens  int64
	OutputTokens int64
	Cost         float64
}

// Summary is a person's usage: today, this month, the days of late, and the limits that bind them.
type Summary struct {
	UserID uuid.UUID
	Name   string
	Today  Day
	Month  Day
	Days   []Day
	Limit  Limit
}

// Mine is the caller's own usage.
func (s *Service) Mine(ctx context.Context) (Summary, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Summary{}, auth.ErrNoSession
	}
	out := Summary{UserID: p.UserID, Name: p.Name, Days: []Day{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if out.Limit, err = Limits(ctx, tx); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT day, input_tokens, output_tokens, cost::float8 FROM usage_daily
			WHERE user_id = current_actor_id() AND day > (now() AT TIME ZONE 'UTC')::date - $1::int ORDER BY day DESC`,
			config.UsageHistoryDays)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Day
			if err := rows.Scan(&d.Day, &d.InputTokens, &d.OutputTokens, &d.Cost); err != nil {
				return err
			}
			out.Days = append(out.Days, d)
		}
		return rows.Err()
	})
	now := time.Now().UTC()
	for _, d := range out.Days {
		if d.Day.Equal(today(now)) {
			out.Today = d
		}
		if d.Day.Year() == now.Year() && d.Day.Month() == now.Month() {
			out.Month = add(out.Month, d)
		}
	}
	out.Today.Day, out.Month.Day = today(now), time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return out, err
}

// People is everyone's usage today and this month, most first, for administrators.
func (s *Service) People(ctx context.Context) ([]Summary, error) {
	out := []Summary{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT d.user_id, COALESCE(u.name, ''),
				COALESCE(sum(d.input_tokens) FILTER (WHERE d.day = (now() AT TIME ZONE 'UTC')::date), 0),
				COALESCE(sum(d.output_tokens) FILTER (WHERE d.day = (now() AT TIME ZONE 'UTC')::date), 0),
				COALESCE(sum(d.cost) FILTER (WHERE d.day = (now() AT TIME ZONE 'UTC')::date), 0)::float8,
				sum(d.input_tokens), sum(d.output_tokens), sum(d.cost)::float8
			FROM usage_daily d LEFT JOIN app_user u ON u.id = d.user_id
			WHERE d.day >= date_trunc('month', now() AT TIME ZONE 'UTC')::date
			GROUP BY d.user_id, u.name ORDER BY sum(d.cost) DESC, sum(d.input_tokens + d.output_tokens) DESC LIMIT $1`,
			config.UsagePeopleLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Summary
			if err := rows.Scan(&p.UserID, &p.Name, &p.Today.InputTokens, &p.Today.OutputTokens, &p.Today.Cost,
				&p.Month.InputTokens, &p.Month.OutputTokens, &p.Month.Cost); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// SetLimits changes the installation's limits on one person, for an administrator, audited.
func (s *Service) SetLimits(ctx context.Context, l Limit) (Limit, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Limit{}, auth.ErrNoSession
	}
	fields := map[string]string{}
	if l.DailyTokens != nil && (*l.DailyTokens <= 0 || *l.DailyTokens > config.MaxDailyTokens) {
		fields["dailyTokens"] = fmt.Sprintf("Give a number of tokens above 0 and at most %d, or leave it empty for no limit.", int64(config.MaxDailyTokens))
	}
	if l.DailyCost != nil && (*l.DailyCost <= 0 || *l.DailyCost > config.MaxDailyCost) {
		fields["dailyCost"] = fmt.Sprintf("Give an amount above 0 and at most %d US dollars, or leave it empty for no limit.", config.MaxDailyCost)
	}
	if l.ConcurrentRuns != nil && (*l.ConcurrentRuns <= 0 || *l.ConcurrentRuns > config.MaxConcurrentRuns) {
		fields["concurrentRuns"] = fmt.Sprintf("Give a number of runs above 0 and at most %d, or leave it empty for no limit.", config.MaxConcurrentRuns)
	}
	if len(fields) > 0 {
		return Limit{}, &Invalid{Fields: fields}
	}
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE usage_limit SET daily_tokens = $1, daily_cost = $2, concurrent_runs = $3, changed_by = $4`,
			l.DailyTokens, l.DailyCost, l.ConcurrentRuns, p.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return auth.ErrNoSession
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionLimitsChanged, TargetType: "usage_limit", Actor: p.UserID,
			Data: map[string]any{"dailyTokens": l.DailyTokens, "dailyCost": l.DailyCost, "concurrentRuns": l.ConcurrentRuns}})
	})
	return l, err
}

// Price is what one model of a provider costs per million tokens, in US dollars.
type Price struct {
	Model            string
	InputPerMillion  float64
	OutputPerMillion float64
}

// Prices are a provider's models' prices.
func (s *Service) Prices(ctx context.Context, providerID uuid.UUID) ([]Price, error) {
	out := []Price{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := models(ctx, tx, providerID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT model, input_per_million::float8, output_per_million::float8 FROM model_price
			WHERE provider_id = $1 ORDER BY model`, providerID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Price
			if err := rows.Scan(&p.Model, &p.InputPerMillion, &p.OutputPerMillion); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// SetPrices replaces a provider's prices, for an administrator; a model left out counts tokens only from then on.
func (s *Service) SetPrices(ctx context.Context, providerID uuid.UUID, prices []Price) ([]Price, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return nil, auth.ErrNoSession
	}
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		listed, err := models(ctx, tx, providerID)
		if err != nil {
			return err
		}
		fields := map[string]string{}
		for i, price := range prices {
			switch {
			case !slices.Contains(listed, price.Model):
				fields[fmt.Sprintf("prices/%d/model", i)] = price.Model + " is not one of the provider's models; add it to the provider first."
			case price.InputPerMillion < 0 || price.InputPerMillion > config.MaxModelPrice || price.OutputPerMillion < 0 || price.OutputPerMillion > config.MaxModelPrice:
				fields[fmt.Sprintf("prices/%d", i)] = fmt.Sprintf("Give prices from 0 to %d US dollars per million tokens.", config.MaxModelPrice)
			}
		}
		if len(fields) > 0 {
			return &Invalid{Fields: fields}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM model_price WHERE provider_id = $1`, providerID); err != nil {
			return err
		}
		priced := []string{}
		for _, price := range prices {
			if _, err := tx.Exec(ctx, `INSERT INTO model_price (provider_id, model, input_per_million, output_per_million) VALUES ($1, $2, $3, $4)`,
				providerID, price.Model, price.InputPerMillion, price.OutputPerMillion); err != nil {
				return err
			}
			priced = append(priced, price.Model)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionProviderChanged, TargetType: "provider", TargetID: providerID.String(),
			Actor: p.UserID, Data: map[string]any{"what": "prices", "priced": priced}})
	})
	if err != nil {
		return nil, err
	}
	return s.Prices(ctx, providerID)
}

// models are a provider's models, as the caller sees the provider.
func models(ctx context.Context, tx db.DBTX, providerID uuid.UUID) ([]string, error) {
	var listed []string
	err := tx.QueryRow(ctx, `SELECT models FROM provider WHERE id = $1`, providerID).Scan(&listed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return listed, err
}

func today(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func add(a, b Day) Day {
	return Day{Day: a.Day, InputTokens: a.InputTokens + b.InputTokens, OutputTokens: a.OutputTokens + b.OutputTokens, Cost: a.Cost + b.Cost}
}
