package runnerctl

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// EventToolCalled is the runner's event for each call it made to a tool, as docs/api-contract-m2.md names it.
const EventToolCalled = "tool.called"

// toolCalled is what auditing needs of a tool.called event; subjects come with the write ledger (#127).
type toolCalled struct {
	Binding  string   `json:"binding"`
	Tool     string   `json:"tool"`
	OK       bool     `json:"ok"`
	Subjects []string `json:"subjects"`
}

// auditUnattended records a call a triggered run made with no approval because its owner agreed to it (#128), so
// an administrator can tell later what Rotor wrote on its own.
func auditUnattended(ctx context.Context, tx db.DBTX, r *run, payload []byte) error {
	if r.triggerID == nil {
		return nil
	}
	var called toolCalled
	if json.Unmarshal(payload, &called) != nil || !called.OK {
		return nil
	}
	b, ok := r.spec.Tools[called.Binding]
	if !ok || !slices.Contains(b.Unattended, called.Tool) {
		return nil
	}
	return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTriggerUnattendedWrite, TargetType: "trigger", TargetID: r.triggerID.String(),
		Data: map[string]any{"run": r.ID.String(), "binding": called.Binding, "call": called.Tool, "tool": b.Tool, "subjects": called.Subjects}})
}
