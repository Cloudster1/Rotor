package oidc

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

// Browser is what the callback knows about the browser it signs in.
type Browser struct {
	UserAgent string
	IP        string
}

// Finish completes a sign-in: it redeems the callback, signs the person in and returns the session token and next.
func (s *Service) Finish(ctx context.Context, state, code string, browser Browser) (token, next string, err error) {
	identity, next, err := s.Exchange(ctx, state, code)
	if err != nil {
		return "", "", err
	}
	token, err = s.SignIn(ctx, identity, browser)
	return token, next, err
}

// SignIn makes or updates the person a verified identity names, follows their realm groups, and opens a session
// holding the provider's session id. A switched-off person is refused, and one not let in waits with ErrNotAMember.
func (s *Service) SignIn(ctx context.Context, identity *Identity, browser Browser) (string, error) {
	st, err := s.current(ctx)
	if err != nil {
		return "", err
	}
	var token string
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		user, set, err := adopt(ctx, tx, identity, st.admitByGroup)
		if err != nil {
			return err
		}
		token, err = s.sessions.Open(ctx, tx, auth.SessionInput{UserID: user.id, OIDCSID: identity.SID, UserAgent: browser.UserAgent, IP: browser.IP})
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSignedIn, TargetType: "user", TargetID: user.id.String(),
			Actor: user.id, IP: browser.IP, Data: map[string]any{"role": auth.RoleFor(set), "groups": identity.Groups, "new": user.created}})
	})
	if errors.Is(err, ErrNotAMember) {
		// The refusal rolled the sign-in back, so the request is kept in a transaction of its own, as in Armature.
		if err := s.noteJoinRequest(ctx, identity); err != nil {
			return "", err
		}
		return "", ErrNotAMember
	}
	if err != nil {
		return "", err
	}
	return token, nil
}

// noteJoinRequest keeps the person and their request to be let in, which Admin's Users page shows.
func (s *Service) noteJoinRequest(ctx context.Context, identity *Identity) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		user, err := upsertUser(ctx, tx, identity)
		if err != nil {
			return err
		}
		if user.letIn {
			return nil
		}
		return perm.NoteJoinRequest(ctx, tx, user.id)
	})
}

// adopt makes or updates the person a verified identity names, lets them in when a realm group of theirs holds a grant
// that does, makes their groups the realm's exactly, and returns what their grants let them do. Somebody switched off
// is refused, and somebody not let in waits.
func adopt(ctx context.Context, tx db.DBTX, identity *Identity, admitByGroup bool) (knownUser, perm.Set, error) {
	user, err := upsertUser(ctx, tx, identity)
	if err != nil {
		return user, perm.Set{}, err
	}
	if !user.active {
		return user, perm.Set{}, auth.ErrSwitchedOff
	}
	if !user.letIn {
		// A group holding Administrator lets its members in whatever the setting, so an installation nobody has been let
		// into yet has somebody to let the rest in, and the realm's service accounts keep working.
		groups, err := perm.MappedGroups(ctx, tx, identity.Groups, !admitByGroup)
		if err != nil {
			return user, perm.Set{}, err
		}
		admitted, err := perm.AdmitByGroup(ctx, tx, user.id, groups)
		if err != nil {
			return user, perm.Set{}, err
		}
		if !admitted {
			return user, perm.Set{}, ErrNotAMember
		}
	}
	if err := syncGroups(ctx, tx, user.id, identity.Groups); err != nil {
		return user, perm.Set{}, err
	}
	set, err := perm.Resolve(ctx, tx, user.id)
	if err != nil {
		return user, perm.Set{}, err
	}
	// The realm groups stay on the person too: approvals are decided by them, as the approval node names them.
	if _, err := tx.Exec(ctx, `UPDATE app_user SET groups = $2 WHERE id = $1`, user.id, identity.Groups); err != nil {
		return user, perm.Set{}, err
	}
	return user, set, nil
}

// knownUser is a person as sign-in finds them, before it copies the realm's groups onto them.
type knownUser struct {
	id      uuid.UUID
	active  bool
	letIn   bool
	created bool
}

// upsertUser finds the person by the provider's name for them, else by address, else makes them.
func upsertUser(ctx context.Context, tx db.DBTX, identity *Identity) (knownUser, error) {
	var u knownUser
	err := tx.QueryRow(ctx, `
		UPDATE app_user SET email = $3, name = $4
		FROM user_identity i
		WHERE i.issuer = $1 AND i.subject = $2 AND app_user.id = i.user_id
		RETURNING app_user.id, app_user.is_active, app_user.standing IS NOT NULL`,
		identity.Issuer, identity.Subject, identity.Email, identity.Name).Scan(&u.id, &u.active, &u.letIn)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return u, fmt.Errorf("find the person: %w", err)
	}
	// xmax = 0 is a row this statement inserted rather than one it updated.
	err = tx.QueryRow(ctx, `
		INSERT INTO app_user (email, name) VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id, is_active, standing IS NOT NULL, xmax = 0`, identity.Email, identity.Name).Scan(&u.id, &u.active, &u.letIn, &u.created)
	if err != nil {
		return u, fmt.Errorf("make the person: %w", err)
	}
	// Two first sign-ins of one person at once both get here; the second waits for the first and keeps its row (#96).
	_, err = tx.Exec(ctx, `INSERT INTO user_identity (issuer, subject, user_id) VALUES ($1, $2, $3)
		ON CONFLICT (issuer, subject) DO NOTHING`, identity.Issuer, identity.Subject, u.id)
	if err != nil {
		return u, fmt.Errorf("remember the provider's name for the person: %w", err)
	}
	err = tx.QueryRow(ctx, `
		SELECT a.id, a.is_active, a.standing IS NOT NULL FROM user_identity i JOIN app_user a ON a.id = i.user_id
		WHERE i.issuer = $1 AND i.subject = $2`, identity.Issuer, identity.Subject).Scan(&u.id, &u.active, &u.letIn)
	if err != nil {
		return u, fmt.Errorf("find the person: %w", err)
	}
	return u, nil
}
