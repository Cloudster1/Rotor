package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/usage"
)

// Usage reads usage, limits and prices (#38); *usage.Service is the real one.
type Usage interface {
	Mine(ctx context.Context) (usage.Summary, error)
	People(ctx context.Context) ([]usage.Summary, error)
	SetLimits(ctx context.Context, l usage.Limit) (usage.Limit, error)
	Prices(ctx context.Context, providerID uuid.UUID) ([]usage.Price, error)
	SetPrices(ctx context.Context, providerID uuid.UUID, prices []usage.Price) ([]usage.Price, error)
}

var errUsageUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "usage_unavailable",
	Message: "Usage is not set up on this installation. An operator connects Rotor to OpenBao and starts its worker."}

func usageError(err error) error {
	var invalid *usage.Invalid
	switch {
	case errors.As(err, &invalid):
		return ErrValidation(invalid.Fields)
	case errors.Is(err, usage.ErrNotFound):
		return errNoSuchProvider
	}
	return err
}

func (s *Server) usageReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Usage == nil {
		respondError(w, r, errUsageUnavailable)
		return false
	}
	return true
}

func (s *Server) handleMyUsage(w http.ResponseWriter, r *http.Request) {
	if !s.usageReady(w, r) {
		return
	}
	mine, err := s.Usage.Mine(r.Context())
	if err != nil {
		respondError(w, r, usageError(err))
		return
	}
	out := usageResponse{Today: toUsageDay(mine.Today), Month: toUsageDay(mine.Month), Days: []usageDay{}, Limits: toLimits(mine.Limit)}
	for _, d := range mine.Days {
		out.Days = append(out.Days, toUsageDay(d))
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handlePeopleUsage(w http.ResponseWriter, r *http.Request) {
	if !s.usageReady(w, r) {
		return
	}
	people, err := s.Usage.People(r.Context())
	if err != nil {
		respondError(w, r, usageError(err))
		return
	}
	out := make([]personUsage, 0, len(people))
	for _, p := range people {
		out = append(out, personUsage{Person: personRef{ID: p.UserID.String(), Name: p.Name}, Today: toUsageDay(p.Today), Month: toUsageDay(p.Month)})
	}
	respondJSON(w, r, http.StatusOK, env{"people": out})
}

func (s *Server) handleGetLimits(w http.ResponseWriter, r *http.Request) {
	if !s.usageReady(w, r) {
		return
	}
	mine, err := s.Usage.Mine(r.Context())
	if err != nil {
		respondError(w, r, usageError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, toLimits(mine.Limit))
}

func (s *Server) handleSetLimits(w http.ResponseWriter, r *http.Request) {
	var req limitsBody
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.usageReady(w, r) {
		return
	}
	set, err := s.Usage.SetLimits(r.Context(), usage.Limit{DailyTokens: req.DailyTokens, DailyCost: req.DailyCost, ConcurrentRuns: req.ConcurrentRuns})
	if err != nil {
		respondError(w, r, usageError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, toLimits(set))
}

func (s *Server) handleGetPrices(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "providerID", "provider")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.usageReady(w, r) {
		return
	}
	prices, err := s.Usage.Prices(r.Context(), id)
	s.answerPrices(w, r, prices, err)
}

func (s *Server) handleSetPrices(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "providerID", "provider")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req struct {
		Prices []modelPrice `json:"prices"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.usageReady(w, r) {
		return
	}
	in := make([]usage.Price, 0, len(req.Prices))
	for _, p := range req.Prices {
		in = append(in, usage.Price{Model: p.Model, InputPerMillion: p.InputPerMillion, OutputPerMillion: p.OutputPerMillion})
	}
	prices, err := s.Usage.SetPrices(r.Context(), id, in)
	s.answerPrices(w, r, prices, err)
}

func (s *Server) answerPrices(w http.ResponseWriter, r *http.Request, prices []usage.Price, err error) {
	if err != nil {
		respondError(w, r, usageError(err))
		return
	}
	out := make([]modelPrice, 0, len(prices))
	for _, p := range prices {
		out = append(out, modelPrice{Model: p.Model, InputPerMillion: p.InputPerMillion, OutputPerMillion: p.OutputPerMillion})
	}
	respondJSON(w, r, http.StatusOK, env{"prices": out})
}

func toUsageDay(d usage.Day) usageDay {
	return usageDay{Day: usageDate(d.Day), InputTokens: d.InputTokens, OutputTokens: d.OutputTokens, Cost: d.Cost}
}

func toLimits(l usage.Limit) limitsBody {
	return limitsBody{DailyTokens: l.DailyTokens, DailyCost: l.DailyCost, ConcurrentRuns: l.ConcurrentRuns}
}
