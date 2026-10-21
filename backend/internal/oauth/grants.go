package oauth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Who ended a grant, as the audit log names it.
const (
	EndedByPerson        = "person"
	EndedByAdministrator = "administrator"
	EndedByClient        = "client"
	EndedByReusedToken   = "reused_token"
)

// ErrNoSuchGrant is a grant that is not the caller's to end, or is gone already.
var ErrNoSuchGrant = errors.New("that assistant was not found")

const grantColumns = `g.id, c.id, c.name, c.uri, c.kind, g.redirect_uri, g.read_only, g.created_at, g.last_used_at`

// The lists are by last use, newest first, then those never used by when they were made.
const grantOrder = ` ORDER BY g.last_used_at DESC NULLS LAST, g.created_at DESC, g.id`

func scanGrant(row pgx.Row, extra ...any) (Grant, error) {
	var (
		g        Grant
		redirect string
	)
	err := row.Scan(append([]any{&g.ID, &g.Client.ID, &g.Client.Name, &g.Client.URI, &g.Client.Kind, &redirect, &g.ReadOnly, &g.CreatedAt,
		&g.LastUsedAt}, extra...)...)
	g.RedirectHost = RedirectHost(redirect)
	return g, err
}

// Grants are the assistants a person connected, read under row level security.
func (s *Service) Grants(ctx context.Context, person uuid.UUID) ([]Grant, error) {
	out := []Grant{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+grantColumns+` FROM oauth_grant g JOIN oauth_client c ON c.id = g.client_id
			WHERE g.user_id = $1`+grantOrder, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			g, err := scanGrant(rows)
			if err != nil {
				return err
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	return out, err
}

// EveryonesGrants are everybody's connected assistants with whose each is; row level security shows them to
// administrators alone, and app_user's to them through the admin role.
func (s *Service) EveryonesGrants(ctx context.Context) ([]OwnedGrant, error) {
	out := []OwnedGrant{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+grantColumns+`, g.user_id
			FROM oauth_grant g JOIN oauth_client c ON c.id = g.client_id`+grantOrder)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var owner Owner
			g, err := scanGrant(rows, &owner.ID)
			if err != nil {
				return err
			}
			out = append(out, OwnedGrant{Grant: g, Owner: owner})
		}
		return rows.Err()
	})
	if err != nil || len(out) == 0 {
		return out, err
	}
	return out, s.nameOwners(ctx, out)
}

// nameOwners fills in who each grant is theirs; the app role reads only its own person, so the names are read as
// the admin role, for grants the administrator could already read.
func (s *Service) nameOwners(ctx context.Context, grants []OwnedGrant) error {
	ids := make([]uuid.UUID, len(grants))
	for i, g := range grants {
		ids[i] = g.Owner.ID
	}
	names := map[uuid.UUID]Owner{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id, name, email FROM app_user WHERE id = ANY ($1)`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o Owner
			if err := rows.Scan(&o.ID, &o.Name, &o.Email); err != nil {
				return err
			}
			names[o.ID] = o
		}
		return rows.Err()
	})
	for i := range grants {
		if o, ok := names[grants[i].Owner.ID]; ok {
			grants[i].Owner = o
		}
	}
	return err
}

// EndGrant ends one of the person's own grants and every token it holds, at once.
func (s *Service) EndGrant(ctx context.Context, person, id uuid.UUID) error {
	return s.end(ctx, &person, person, id)
}

// EndAnybodysGrant ends anybody's grant, for an administrator.
func (s *Service) EndAnybodysGrant(ctx context.Context, actor, id uuid.UUID) error {
	return s.end(ctx, nil, actor, id)
}

func (s *Service) end(ctx context.Context, owner *uuid.UUID, actor, id uuid.UUID) error {
	by := EndedByAdministrator
	if owner != nil {
		by = EndedByPerson
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var holder uuid.UUID
		var client string
		// The tokens go with the grant by its key, so nothing it handed out works after this commits.
		err := tx.QueryRow(ctx, `
			DELETE FROM oauth_grant g USING oauth_client c
			WHERE g.id = $1 AND c.id = g.client_id AND ($2::uuid IS NULL OR g.user_id = $2)
			RETURNING g.user_id, c.name`, id, owner).Scan(&holder, &client)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchGrant
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, endedEntry(id, actor, holder, client, by))
	})
}

// deleteGrant ends a grant nobody signed in ended, and records who did.
func deleteGrant(ctx context.Context, tx db.DBTX, grant uuid.UUID, by string) error {
	var owner uuid.UUID
	var client string
	err := tx.QueryRow(ctx, `DELETE FROM oauth_grant g USING oauth_client c WHERE g.id = $1 AND c.id = g.client_id
		RETURNING g.user_id, c.name`, grant).Scan(&owner, &client)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return audit.Write(ctx, tx, endedEntry(grant, uuid.Nil, owner, client, by))
}

// endedEntry is the record of a grant ended; actor is nobody when the client or a reused token ended it.
func endedEntry(grant, actor, owner uuid.UUID, client, by string) audit.Entry {
	return audit.Entry{Action: audit.ActionOAuthRevoked, TargetType: "oauth_grant", TargetID: grant.String(), Actor: actor,
		Data: map[string]any{"owner": owner.String(), "client": client, "by": by}}
}
