package config

// Usage, cost and rate (#38); docs/api-contract-m7-usage.md describes them.
const (
	// TokensPerPriceUnit is what a model's prices are given per: a million tokens.
	TokensPerPriceUnit = 1_000_000
	// MaxUsageTokensPerCall bounds what one model call may report, so a broken runner cannot overflow a total.
	MaxUsageTokensPerCall = 10_000_000
	// MaxModelPrice bounds a price per million tokens, in US dollars.
	MaxModelPrice = 10_000
	// MaxDailyCost bounds a person's daily cost quota, in US dollars.
	MaxDailyCost = 1_000_000
	// MaxDailyTokens bounds a person's daily token quota.
	MaxDailyTokens = 10_000_000_000
	// MaxConcurrentRuns bounds how many runs one person may have going at once.
	MaxConcurrentRuns = 1000
	// UsagePeopleLimit bounds the people one usage list answers.
	UsagePeopleLimit = 500
	// UsageHistoryDays is how many days of a person's usage the usage page shows.
	UsageHistoryDays = 31
)
