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

// SignIn makes or updates the person a verified identity names, copies their role and groups from the
// realm, and opens a session holding the provider's session id. A switched-off person is refused.
func (s *Service) SignIn(ctx context.Context, identity *Identity, browser Browser) (string, error) {
	var token string
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		user, role, err := adopt(ctx, tx, identity)
		if err != nil {
			return err
		}
		token, err = s.sessions.Open(ctx, tx, auth.SessionInput{UserID: user.id, OIDCSID: identity.SID, UserAgent: browser.UserAgent, IP: browser.IP})
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSignedIn, TargetType: "user", TargetID: user.id.String(),
			Actor: user.id, IP: browser.IP, Data: map[string]any{"role": role, "groups": identity.Groups, "new": user.created}})
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// adopt makes or updates the person a verified identity names and copies their role and groups from the realm; a
// switched-off person is refused.
func adopt(ctx context.Context, tx db.DBTX, identity *Identity) (knownUser, string, error) {
	user, err := upsertUser(ctx, tx, identity)
	if err != nil {
		return user, "", err
	}
	if !user.active {
		return user, "", auth.ErrSwitchedOff
	}
	role := RoleFor(identity.Groups)
	if _, err := tx.Exec(ctx, `UPDATE app_user SET role = $2, groups = $3 WHERE id = $1`, user.id, role, identity.Groups); err != nil {
		return user, "", err
	}
	if user.role != role {
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRoleChanged, TargetType: "user", TargetID: user.id.String(),
			Data: map[string]any{"from": user.role, "to": role, "source": "identity provider"}}); err != nil {
			return user, "", err
		}
	}
	return user, role, nil
}

// knownUser is a person as sign-in finds them, before it copies the realm's word onto them.
type knownUser struct {
	id      uuid.UUID
	role    string
	active  bool
	created bool
}

// upsertUser finds the person by the provider's name for them, else by address, else makes them.
func upsertUser(ctx context.Context, tx db.DBTX, identity *Identity) (knownUser, error) {
	var u knownUser
	err := tx.QueryRow(ctx, `
		UPDATE app_user SET email = $3, name = $4
		FROM user_identity i
		WHERE i.issuer = $1 AND i.subject = $2 AND app_user.id = i.user_id
		RETURNING app_user.id, app_user.role, app_user.is_active`,
		identity.Issuer, identity.Subject, identity.Email, identity.Name).Scan(&u.id, &u.role, &u.active)
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
		RETURNING id, role, is_active, xmax = 0`, identity.Email, identity.Name).Scan(&u.id, &u.role, &u.active, &u.created)
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
		SELECT a.id, a.role, a.is_active FROM user_identity i JOIN app_user a ON a.id = i.user_id
		WHERE i.issuer = $1 AND i.subject = $2`, identity.Issuer, identity.Subject).Scan(&u.id, &u.role, &u.active)
	if err != nil {
		return u, fmt.Errorf("find the person: %w", err)
	}
	return u, nil
}
