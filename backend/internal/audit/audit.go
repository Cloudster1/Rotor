// Package audit is Rotor's record of who did what, written in the same
// transaction as the act so the two cannot disagree.
package audit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/redact"
)

// Actions recorded so far; each feature adds its own as it lands, named domain.verb.
const (
	ActionSignedIn               = "user.signed_in"
	ActionSignedOut              = "user.signed_out"
	ActionRoleChanged            = "user.role_changed"
	ActionSwitchedOff            = "user.switched_off"
	ActionSessionEndedByProvider = "session.ended_by_provider"
	ActionToolAdded              = "tool.added"
	ActionToolRemoved            = "tool.removed"
	ActionConnectionGranted      = "connection.granted"
	ActionConnectionEnded        = "connection.ended"
	ActionTokenMinted            = "token.minted"
	ActionTokenRevoked           = "token.revoked"
	ActionWorkflowCreated        = "workflow.created"
	ActionWorkflowChanged        = "workflow.changed"
	ActionWorkflowShared         = "workflow.shared"
	ActionWorkflowArchived       = "workflow.archived"
	ActionProviderChanged        = "provider.changed"
	ActionRunStarted             = "run.started"
	ActionRunCancelled           = "run.cancelled"
	ActionApprovalDecided        = "approval.decided"
	ActionTriggerCreated         = "trigger.created"
	ActionTriggerChanged         = "trigger.changed"
	ActionTriggerRemoved         = "trigger.removed"
	ActionTriggerEnabled         = "trigger.enabled"
	ActionTriggerDisabled        = "trigger.disabled"
	ActionSandboxSpawned         = "sandbox.spawned"
	ActionSandboxDestroyed       = "sandbox.destroyed"
	ActionRunEnded               = "run.ended"
	ActionApprovalRequested      = "approval.requested"
	ActionAuditExported          = "audit.exported"
	ActionLimitsChanged          = "limits.changed"
	ActionHookSecretChanged      = "hook.secret_changed"
	ActionLangSmithChanged       = "langsmith.changed"
)

// Actions is every action the log may hold, for a filter to offer and a client to name.
var Actions = []string{ActionSignedIn, ActionSignedOut, ActionRoleChanged, ActionSwitchedOff, ActionSessionEndedByProvider,
	ActionToolAdded, ActionToolRemoved, ActionConnectionGranted, ActionConnectionEnded, ActionTokenMinted, ActionTokenRevoked, ActionProviderChanged,
	ActionWorkflowCreated, ActionWorkflowChanged, ActionWorkflowShared, ActionWorkflowArchived, ActionRunStarted, ActionRunCancelled,
	ActionApprovalDecided, ActionTriggerCreated, ActionTriggerChanged, ActionTriggerRemoved, ActionTriggerEnabled, ActionTriggerDisabled,
	ActionHookSecretChanged, ActionLimitsChanged, ActionLangSmithChanged,
	ActionSandboxSpawned, ActionSandboxDestroyed, ActionRunEnded, ActionApprovalRequested, ActionAuditExported}

// Redacted stands in the record for a value that looked like a credential.
var Redacted = redact.Redacted

// Entry is one act to record. A blank IP or request id takes the one on the context.
type Entry struct {
	Action     string
	TargetType string
	TargetID   string
	Actor      uuid.UUID
	Data       map[string]any
	IP         string
	RequestID  string
}

type (
	ipKey        struct{}
	requestIDKey struct{}
)

// WithIP carries the caller's address to every entry written for them.
func WithIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ipKey{}, ip)
}

// WithRequestID carries the request's id to every entry written while serving it.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func fromContext(ctx context.Context, key any, given string) string {
	if given != "" {
		return given
	}
	v, _ := ctx.Value(key).(string)
	return v
}

// Write records an entry inside the caller's transaction.
func Write(ctx context.Context, tx db.DBTX, e Entry) error {
	if strings.TrimSpace(e.Action) == "" {
		return errors.New("an audit entry needs an action")
	}
	data := []byte("{}")
	if e.Data != nil {
		var err error
		if data, err = Scrub(e.Data); err != nil {
			return fmt.Errorf("%s: %w", e.Action, err)
		}
	}
	var actor *uuid.UUID
	if e.Actor != uuid.Nil {
		actor = &e.Actor
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor_user_id, action, target_type, target_id, data, ip, request_id)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, '')::inet, NULLIF($7, ''))`,
		actor, e.Action, e.TargetType, e.TargetID, data,
		fromContext(ctx, ipKey{}, e.IP), fromContext(ctx, requestIDKey{}, e.RequestID))
	return err
}

// Scrub writes an entry's data as JSON with every value that looks like a
// credential replaced by Redacted, so an act never fails over its record.
func Scrub(data map[string]any) ([]byte, error) {
	return redact.Data(data)
}
