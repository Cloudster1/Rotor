package oidc

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// syncGroups makes a person's realm groups exactly what the claim names, as Armature's sign-in does: each named group
// is made on sight, since Rotor has one realm, and groups made by hand or Everyone are left alone.
func syncGroups(ctx context.Context, tx db.DBTX, userID uuid.UUID, claimed []string) error {
	if claimed == nil {
		claimed = []string{}
	}
	if _, err := tx.Exec(ctx, `SELECT realm_group(ref) FROM unnest($1::text[]) AS ref`, claimed); err != nil {
		return fmt.Errorf("make the groups the realm names: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM group_member m
		USING user_group g
		WHERE m.group_id = g.id AND m.user_id = $1
		  AND g.source = 'oidc'
		  AND COALESCE(g.external_ref, '') <> ALL($2)`,
		userID, claimed); err != nil {
		return fmt.Errorf("leave the groups the realm no longer names: %w", err)
	}
	if len(claimed) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO group_member (group_id, user_id)
		SELECT g.id, $1 FROM user_group g
		WHERE g.source = 'oidc' AND g.external_ref = ANY($2)
		ON CONFLICT DO NOTHING`,
		userID, claimed); err != nil {
		return fmt.Errorf("join the groups the realm names: %w", err)
	}
	return nil
}
