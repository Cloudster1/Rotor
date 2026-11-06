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
	ActionSignedIn  = "user.signed_in"
	ActionSignedOut = "user.signed_out"
	// ActionRoleChanged and ActionWorkflowShared are written no more since #205; the log keeps the entries made before.
	ActionRoleChanged            = "user.role_changed"
	ActionSwitchedOff            = "user.switched_off"
	ActionSessionEndedByProvider = "session.ended_by_provider"
	ActionToolAdded              = "tool.added"
	ActionToolRemoved            = "tool.removed"
	ActionToolChecked            = "tool.checked"
	ActionToolAllowed            = "tool.allowed"
	// ActionToolRenamed is an administrator changing the name workflows bind a tool by (#267).
	ActionToolRenamed       = "tool.renamed"
	ActionConnectionGranted = "connection.granted"
	ActionConnectionEnded   = "connection.ended"
	ActionTokenMinted       = "token.minted"
	ActionTokenRevoked      = "token.revoked"
	ActionWorkflowCreated   = "workflow.created"
	ActionWorkflowChanged   = "workflow.changed"
	ActionWorkflowShared    = "workflow.shared"
	ActionWorkflowArchived  = "workflow.archived"
	// ActionWorkflowRestored is a workflow taken out of the archive, which is how archiving is undone (#205).
	ActionWorkflowRestored  = "workflow.restored"
	ActionProviderChanged   = "provider.changed"
	ActionRunStarted        = "run.started"
	ActionRunCancelled      = "run.cancelled"
	ActionApprovalDecided   = "approval.decided"
	ActionTriggerCreated    = "trigger.created"
	ActionTriggerChanged    = "trigger.changed"
	ActionTriggerRemoved    = "trigger.removed"
	ActionTriggerEnabled    = "trigger.enabled"
	ActionTriggerDisabled   = "trigger.disabled"
	ActionSandboxSpawned    = "sandbox.spawned"
	ActionSandboxDestroyed  = "sandbox.destroyed"
	ActionRunEnded          = "run.ended"
	ActionApprovalRequested = "approval.requested"
	ActionAuditExported     = "audit.exported"
	ActionLimitsChanged     = "limits.changed"
	ActionHookSecretChanged = "hook.secret_changed"
	ActionLangSmithChanged  = "langsmith.changed"
	ActionRetentionChanged  = "retention.changed"
	ActionRunDataDeleted    = "run.data_deleted"
	ActionOAuthGranted      = "oauth.granted"
	ActionOAuthRevoked      = "oauth.revoked"
	ActionOAuthSwitched     = "oauth.switched"
	// ActionUnattendedSwitched and ActionTriggerUnattendedWrite are M12's (#128): the administrators' switch, and a
	// write a triggered run made with no approval through a call its owner agreed to.
	ActionUnattendedSwitched     = "unattended.switched"
	ActionTriggerUnattendedWrite = "trigger.unattended_write"
	// ActionWorkspacesChanged is an administrator changing what workspaces may use (#136).
	ActionWorkspacesChanged = "workspaces.changed"
	// ActionRoleGranted, ActionRoleRevoked and ActionRoleRemoved are grants given and taken away, and a role deleted (#202).
	ActionRoleGranted = "role.granted"
	ActionRoleRevoked = "role.revoked"
	ActionRoleRemoved = "role.removed"
	// ActionRoleDefined and ActionRoleRedefined are a role added and a role renamed or given other permissions (#203).
	ActionRoleDefined   = "role.defined"
	ActionRoleRedefined = "role.redefined"
	// ActionGroupCreated, ActionGroupDeleted, ActionGroupMemberAdded and ActionGroupMemberRemoved are groups kept by
	// hand (#203).
	ActionGroupCreated       = "group.created"
	ActionGroupDeleted       = "group.deleted"
	ActionGroupMemberAdded   = "group.member_added"
	ActionGroupMemberRemoved = "group.member_removed"
	// ActionSwitchedOn is a switched-off person let in again (#203).
	ActionSwitchedOn = "user.switched_on"
	// ActionMemberAdmitted and ActionMemberDeclined are a waiting person let in or turned away (#204), and
	// ActionOIDCSaved single sign-on set up or changed; its secret is never in the entry.
	ActionMemberAdmitted = "member.admitted"
	ActionMemberDeclined = "member.declined"
	ActionOIDCSaved      = "oidc.saved"
	// ActionSkillCreated, ActionSkillChanged, ActionSkillArchived and ActionSkillRestored are the skill library's (#219);
	// sharing a skill is role.granted and role.revoked naming it.
	ActionSkillCreated  = "skill.created"
	ActionSkillChanged  = "skill.changed"
	ActionSkillArchived = "skill.archived"
	ActionSkillRestored = "skill.restored"
	// ActionSkillImported is a zip that made a skill or saved its next version (#220), beside skill.created or skill.changed.
	ActionSkillImported = "skill.imported"
	// ActionSkillInstalled is a version of a skill Rotor ships, which the api added at its start (#225).
	ActionSkillInstalled = "skill.installed"
	// ActionTracesChanged is an administrator changing how long traces are kept and whether their text is (#242).
	ActionTracesChanged = "traces.changed"
	// ActionTraceDestinationAdded, ActionTraceDestinationChanged and ActionTraceDestinationRemoved are an administrator
	// keeping the OTLP destinations runs' traces go to (#246), never a header's value.
	ActionTraceDestinationAdded   = "trace_destination.added"
	ActionTraceDestinationChanged = "trace_destination.changed"
	ActionTraceDestinationRemoved = "trace_destination.removed"
	// ActionTraceDestinationCarriedOver is the api's start making the LangSmith setting a destination (#246).
	ActionTraceDestinationCarriedOver = "trace_destination.carried_over"
	// ActionWorkflowTracingChanged is a workflow's administrator choosing what its runs' traces keep and where they go (#259).
	ActionWorkflowTracingChanged = "workflow.tracing_changed"
	// ActionIngestKeyCreated, ActionIngestKeyRotated and ActionIngestKeyRevoked are an administrator keeping the keys
	// services send spans with (#258), never a key or its hash.
	ActionIngestKeyCreated = "ingest_key.created"
	ActionIngestKeyRotated = "ingest_key.rotated"
	ActionIngestKeyRevoked = "ingest_key.revoked"
)

// Actions is every action the log may hold, for a filter to offer and a client to name.
var Actions = []string{ActionSignedIn, ActionSignedOut, ActionRoleChanged, ActionSwitchedOff, ActionSessionEndedByProvider,
	ActionToolAdded, ActionToolRemoved, ActionToolChecked, ActionToolAllowed, ActionToolRenamed, ActionConnectionGranted, ActionConnectionEnded, ActionTokenMinted, ActionTokenRevoked, ActionProviderChanged,
	ActionWorkflowCreated, ActionWorkflowChanged, ActionWorkflowShared, ActionWorkflowArchived, ActionWorkflowRestored, ActionRunStarted, ActionRunCancelled,
	ActionApprovalDecided, ActionTriggerCreated, ActionTriggerChanged, ActionTriggerRemoved, ActionTriggerEnabled, ActionTriggerDisabled,
	ActionHookSecretChanged, ActionLimitsChanged, ActionLangSmithChanged,
	ActionSandboxSpawned, ActionSandboxDestroyed, ActionRunEnded, ActionApprovalRequested, ActionAuditExported,
	ActionRetentionChanged, ActionRunDataDeleted, ActionOAuthGranted, ActionOAuthRevoked, ActionOAuthSwitched,
	ActionUnattendedSwitched, ActionTriggerUnattendedWrite, ActionWorkspacesChanged, ActionRoleGranted, ActionRoleRevoked,
	ActionRoleRemoved, ActionRoleDefined, ActionRoleRedefined, ActionGroupCreated, ActionGroupDeleted, ActionGroupMemberAdded,
	ActionGroupMemberRemoved, ActionSwitchedOn, ActionMemberAdmitted, ActionMemberDeclined, ActionOIDCSaved,
	ActionSkillCreated, ActionSkillChanged, ActionSkillArchived, ActionSkillRestored, ActionSkillImported,
	ActionSkillInstalled, ActionTracesChanged, ActionTraceDestinationAdded, ActionTraceDestinationChanged,
	ActionTraceDestinationRemoved, ActionTraceDestinationCarriedOver, ActionWorkflowTracingChanged, ActionIngestKeyCreated,
	ActionIngestKeyRotated, ActionIngestKeyRevoked}

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
