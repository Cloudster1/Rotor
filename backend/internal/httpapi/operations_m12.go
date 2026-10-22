package httpapi

import (
	"time"
)

// The rows of M12: the switch for unattended writes (#128); docs/api-contract-m12.md says what they mean, and which
// fields the trigger and workflow operations gain.
func init() {
	operations = append(operations, m12Operations...)
}

var m12Operations = []operation{
	{method: "GET", path: "/settings/unattended-writes", handler: "handleGetUnattendedWrites", tag: "triggers", admin: true,
		summary:   "Whether a trigger's owner may let named writes happen without an approval.",
		responses: ok(unattendedSetting{})},
	{method: "PUT", path: "/settings/unattended-writes", handler: "handleSetUnattendedWrites", tag: "triggers", admin: true,
		summary:     "Allow or forbid unattended writes for everyone.",
		description: "Forbidding them turns off at once every trigger whose workflow names an unattended call.",
		request:     unattendedSettingInput{}, responses: ok(unattendedSetting{})},
}

// unattendedSetting is whether triggers may write through named calls with no approval, as administrators set it.
type unattendedSetting struct {
	Allowed   bool      `json:"allowed"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// unattendedSettingInput allows or forbids unattended writes for the installation.
type unattendedSettingInput struct {
	Allowed bool `json:"allowed"`
}
