package connections

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Disconnect ends the signed-in person's connection to a tool: revoked at the tool where it answers, then forgotten.
func (s *Service) Disconnect(ctx context.Context, toolID uuid.UUID) error {
	p := auth.PrincipalFrom(ctx)
	if p == nil || p.ThroughAssistant() {
		return ErrNoSession
	}
	var exists bool
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM connection WHERE user_id = current_actor_id() AND tool_id = $1)`, toolID).Scan(&exists)
	}); err != nil {
		return err
	}
	if !exists {
		return ErrNotConnected
	}
	tool, err := s.tools.Lookup(ctx, toolID)
	if err != nil {
		return err
	}
	s.revokeRunsOf(ctx, p.UserID, tool.ID)
	revoked := s.revokeAndForget(ctx, p.UserID, tool)
	// Deleted as the person, so the database refuses to end anyone else's connection.
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM connection WHERE user_id = current_actor_id() AND tool_id = $1`, toolID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, ended(p.UserID, p.UserID, tool, EndedBySelf, revoked))
	})
}

// revokeRunsOf revokes the tokens the person's runs hold at the tool; one the tool does not confirm is tried again
// when the run ends, and lapses on its own before long.
func (s *Service) revokeRunsOf(ctx context.Context, userID, toolID uuid.UUID) {
	if err := s.RevokeFor(ctx, userID, toolID); err != nil {
		s.log.Warn("a run's token was not revoked yet", "tool", toolID, "error", err)
	}
}

// revokeAndForget revokes the refresh token at the tool, which ends the grant there, and deletes it from OpenBao.
// It reports whether the tool confirmed; the connection ends in Rotor either way.
func (s *Service) revokeAndForget(ctx context.Context, userID uuid.UUID, tool tools.Tool) bool {
	revoked := false
	secret, _, err := s.vault.Connection(ctx, userID, tool.ID)
	switch {
	case err == nil:
		client, cerr := s.tools.Client(ctx, tool.ID)
		if cerr == nil {
			cerr = s.revoke(ctx, tool, client, secret.RefreshToken, "refresh_token")
		}
		revoked = cerr == nil
		if cerr != nil {
			s.log.Warn("the tool did not confirm the revocation", "tool", tool.Name, "error", cerr)
		}
	case !errors.Is(err, vault.ErrNotFound):
		s.log.Warn("cannot read the connection to revoke it", "tool", tool.Name, "error", err)
	}
	if err := s.vault.DeleteConnection(context.WithoutCancel(ctx), userID, tool.ID); err != nil {
		s.log.Error("cannot forget a connection's refresh token", "tool", tool.Name, "error", err)
	}
	return revoked
}

func ended(actor, userID uuid.UUID, tool tools.Tool, by string, revoked bool) audit.Entry {
	return audit.Entry{Action: audit.ActionConnectionEnded, TargetType: "tool", TargetID: tool.ID.String(), Actor: actor,
		Data: map[string]any{"tool": tool.Name, "user": userID.String(), "by": by, "revoked_at_tool": revoked}}
}

// EndToolConnections ends everyone's connection to a tool that is being removed; the rows go with the tool.
func (s *Service) EndToolConnections(ctx context.Context, tool tools.Tool) (int, error) {
	people, err := s.connected(ctx, `SELECT user_id FROM connection WHERE tool_id = $1`, tool.ID)
	if err != nil {
		return 0, err
	}
	actor := uuid.Nil
	if p := auth.PrincipalFrom(ctx); p != nil {
		actor = p.UserID
	}
	for _, userID := range people {
		s.revokeRunsOf(ctx, userID, tool.ID)
		revoked := s.revokeAndForget(ctx, userID, tool)
		if err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			return audit.Write(ctx, tx, ended(actor, userID, tool, EndedByToolRemoved, revoked))
		}); err != nil {
			return 0, err
		}
	}
	return len(people), nil
}

// EndPersonConnections ends every connection of a person who was switched off.
func (s *Service) EndPersonConnections(ctx context.Context, userID uuid.UUID) error {
	toolIDs, err := s.connected(ctx, `SELECT tool_id FROM connection WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}
	for _, toolID := range toolIDs {
		tool, err := s.tools.Lookup(ctx, toolID)
		if errors.Is(err, tools.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		s.revokeRunsOf(ctx, userID, tool.ID)
		revoked := s.revokeAndForget(ctx, userID, tool)
		if err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			if _, err := tx.Exec(ctx, `DELETE FROM connection WHERE user_id = $1 AND tool_id = $2`, userID, toolID); err != nil {
				return err
			}
			return audit.Write(ctx, tx, ended(uuid.Nil, userID, tool, EndedBySwitchOff, revoked))
		}); err != nil {
			return fmt.Errorf("end %s's connection to %s: %w", userID, tool.Name, err)
		}
	}
	return nil
}

func (s *Service) connected(ctx context.Context, query string, arg uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, query, arg)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	return out, err
}
