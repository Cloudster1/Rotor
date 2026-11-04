package httpapi

// The rows of a person's own settings (#206), beside /auth/me as Armature keeps a person's time zone on them.
func init() {
	operations = append(operations, preferenceOperations...)
}

var preferenceOperations = []operation{
	{method: "GET", path: "/auth/me/preferences", handler: "handleGetPreferences", tag: "auth",
		summary: "What you chose for yourself, such as the time zone dates are written in.", responses: ok(preferencesBody{})},
	{method: "PUT", path: "/auth/me/preferences", handler: "handleSetPreferences", tag: "auth",
		summary:     "Choose for yourself, such as the time zone dates are written in.",
		description: "A time zone the IANA database names, or null to follow the browser's. Only you change yours, in person.",
		request:     preferencesBody{}, responses: ok(preferencesBody{})},
}

// preferencesBody is what a person chose for themselves.
type preferencesBody struct {
	// TimeZone is an IANA name such as Europe/Berlin, or null to follow the browser's.
	TimeZone *string `json:"timeZone"`
}
