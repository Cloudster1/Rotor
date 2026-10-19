package httpapi

import (
	"net/http"
	"time"
)

// The usage rows of M7 (#38); docs/api-contract-m7-usage.md says what they mean.
func init() {
	operations = append(operations, m7UsageOperations...)
}

var m7UsageOperations = []operation{
	{method: "GET", path: "/usage", handler: "handleMyUsage", tag: "usage",
		summary:   "What your runs used today, this month and in the last days, and the limits that bind you.",
		responses: ok(usageResponse{})},
	{method: "GET", path: "/usage/people", handler: "handlePeopleUsage", tag: "usage", admin: true,
		summary: "What each person's runs used today and this month, most first.", responses: ok(env{"people": []personUsage{}})},
	{method: "GET", path: "/settings/limits", handler: "handleGetLimits", tag: "usage", admin: true,
		summary: "The limits on one person: tokens and money a day, and runs going at once.", responses: ok(limitsBody{})},
	{method: "PUT", path: "/settings/limits", handler: "handleSetLimits", tag: "usage", admin: true,
		summary: "Change the limits on one person; null is no limit.", request: limitsBody{},
		responses: map[int]any{http.StatusOK: limitsBody{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/providers/{providerID}/prices", handler: "handleGetPrices", tag: "usage", admin: true,
		summary:   "What each model of a provider costs per million tokens, in US dollars.",
		responses: map[int]any{http.StatusOK: env{"prices": []modelPrice{}}, http.StatusNotFound: notFound}},
	{method: "PUT", path: "/providers/{providerID}/prices", handler: "handleSetPrices", tag: "usage", admin: true,
		summary:     "Replace a provider's prices; a model left out counts tokens only from then on.",
		description: "A run keeps the prices its model had when it began.",
		request:     env{"prices": []modelPrice{}},
		responses:   map[int]any{http.StatusOK: env{"prices": []modelPrice{}}, http.StatusNotFound: notFound, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

// usageDay is what someone used in one UTC day, or a span of days, the cost in US dollars.
type usageDay struct {
	Day          string  `json:"day"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	Cost         float64 `json:"cost"`
}

// limitsBody is the limits on one person; null is no limit.
type limitsBody struct {
	DailyTokens    *int64   `json:"dailyTokens"`
	DailyCost      *float64 `json:"dailyCost"`
	ConcurrentRuns *int     `json:"concurrentRuns"`
}

// usageResponse is the caller's usage and their limits.
type usageResponse struct {
	Today  usageDay   `json:"today"`
	Month  usageDay   `json:"month"`
	Days   []usageDay `json:"days"`
	Limits limitsBody `json:"limits"`
}

// personUsage is one person's usage, for administrators.
type personUsage struct {
	Person personRef `json:"person"`
	Today  usageDay  `json:"today"`
	Month  usageDay  `json:"month"`
}

// modelPrice is what one model costs per million tokens read and written, in US dollars.
type modelPrice struct {
	Model            string  `json:"model"`
	InputPerMillion  float64 `json:"inputPerMillion"`
	OutputPerMillion float64 `json:"outputPerMillion"`
}

// usageDate writes a day as the API names it.
func usageDate(t time.Time) string { return t.UTC().Format(time.DateOnly) }
