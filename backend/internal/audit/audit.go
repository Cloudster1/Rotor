// Package audit is Rotor's record of who did what, written in the same
// transaction as the act so the two cannot disagree.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
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
	ActionWorkflowCreated        = "workflow.created"
	ActionWorkflowChanged        = "workflow.changed"
	ActionWorkflowShared         = "workflow.shared"
	ActionWorkflowArchived       = "workflow.archived"
	ActionProviderChanged        = "provider.changed"
)

// Actions is every action the log may hold, for a filter to offer and a client to name.
var Actions = []string{ActionSignedIn, ActionSignedOut, ActionRoleChanged, ActionSwitchedOff, ActionSessionEndedByProvider,
	ActionToolAdded, ActionToolRemoved, ActionConnectionGranted, ActionConnectionEnded, ActionTokenMinted, ActionProviderChanged,
	ActionWorkflowCreated, ActionWorkflowChanged, ActionWorkflowShared, ActionWorkflowArchived}

// Redacted stands in the record for a value that looked like a credential.
const Redacted = "[redacted]"

// SecretPrefixes start the credentials Rotor and the tools issue, and every JWT,
// so such a value never reaches the record whatever key it hides under.
var SecretPrefixes = []string{
	"rotor_pat_", "exciter_pat_", "exciter_whs_", "exciter_oat_", "exciter_ort_", "exciter_ocs_", "exciter_oac_", "armature_pat_", "armature_whs_", "stator_pat_", "stator_whs_", "eyJ",
}

// secretWords mark a key whose value would be a credential if it were text.
var secretWords = []string{"secret", "password", "token", "credential"}

// secretStates are the words a secret's key may carry instead of the secret.
var secretStates = map[string]bool{"set": true, "kept": true, "removed": true}

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
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(scrub("", v))
}

func scrub(key string, v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			v[k] = scrub(k, inner)
		}
		return v
	case []any:
		for i, inner := range v {
			v[i] = scrub(key, inner)
		}
		return v
	case string:
		for _, prefix := range SecretPrefixes {
			if strings.HasPrefix(strings.TrimSpace(v), prefix) {
				return Redacted
			}
		}
		if secretKey(key) && v != "" && !secretStates[v] {
			return Redacted
		}
	}
	return v
}

func secretKey(key string) bool {
	k := strings.ToLower(key)
	for _, word := range secretWords {
		if strings.Contains(k, word) {
			return true
		}
	}
	return false
}
