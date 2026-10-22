package runnerctl

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// EventToolCalled is the runner's event for each call it made to a tool, as docs/api-contract-m2.md names it.
const EventToolCalled = "tool.called"

// toolCalled is what the ledger and the audit need of a tool.called event.
type toolCalled struct {
	Binding  string   `json:"binding"`
	Tool     string   `json:"tool"`
	OK       bool     `json:"ok"`
	Write    bool     `json:"write"`
	Subjects []string `json:"subjects"`
}

// recordWrites keeps what a write wrote in the run's ledger (#127), so an event about it does not start the same
// workflow again; at most config.MaxWriteSubjects of one call.
func recordWrites(ctx context.Context, tx db.DBTX, r *run, bindings map[string]binding, payload []byte) error {
	var c toolCalled
	if json.Unmarshal(payload, &c) != nil || !c.OK || !c.Write {
		return nil
	}
	b, ok := bindings[c.Binding]
	if !ok {
		return nil
	}
	for _, subject := range c.Subjects[:min(len(c.Subjects), config.MaxWriteSubjects)] {
		if subject == "" || len(subject) > config.MaxWriteSubject {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO run_write (run_id, tool_id, call, subject) VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING`, r.ID, b.toolID, c.Tool, subject); err != nil {
			return err
		}
	}
	return nil
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
