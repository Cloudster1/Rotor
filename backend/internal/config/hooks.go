package config

import "time"

// Hooks: where Armature, Exciter and Stator deliver the events that start event triggers (#35);
// docs/api-contract-m6.md describes them.
const (
	// MaxHookBody bounds one delivery's body; a larger one is refused with 413.
	MaxHookBody = 256 << 10
	// HookReplayWindow is how far a delivery's signed timestamp may be from Rotor's clock, either way.
	HookReplayWindow = 5 * time.Minute
	// HookEventRetention is how long a tool's event ids are kept to refuse a delivery received before.
	HookEventRetention = 7 * 24 * time.Hour
	// HookSecretCacheTTL is how long a tool's secret is kept in memory, so anonymous deliveries do not each read OpenBao.
	HookSecretCacheTTL = time.Minute
	// HookSecretSuffix ends the prefix of a tool's webhook secrets, after its product: exciter_whs_.
	HookSecretSuffix = "_whs_"
	// MaxHookSecretChars bounds the secret an administrator pastes.
	MaxHookSecretChars = 200
	// HookPingTopic is the topic a tool sends to test a webhook; it starts nothing.
	HookPingTopic = "ping"
)

// HookProducts maps the products Rotor takes deliveries from to the word their headers carry, as in
// X-Exciter-Signature-Timestamped-256.
var HookProducts = map[string]string{"armature": "Armature", "exciter": "Exciter", "stator": "Stator"}

// HookCloudEventTypes is the prefix of each product's CloudEvents types, for those that send version 2.
var HookCloudEventTypes = map[string]string{"exciter": "dev.exciter."}

// Event trigger filters (#35).
const (
	MaxTriggerConditions  = 10
	MaxConditionPathDepth = 10
	MaxTriggerTopics      = 20
	MaxTriggerProjects    = 50
)
