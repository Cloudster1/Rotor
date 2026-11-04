// Package preferences keeps what a person chooses for themselves (#206): today the time zone dates are written in.
package preferences

import (
	"context"
	"errors"
	"strings"
	"time"
	// The zones a person chooses from, for test containers whose image carries none, as triggers does.
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Preferences are a person's own; an empty TimeZone follows their browser.
type Preferences struct {
	TimeZone string
}

// Invalid is a choice refused, with a sentence for each field that is wrong.
type Invalid struct{ Fields map[string]string }

func (e *Invalid) Error() string { return "the preferences are not valid" }

// ErrNotInPerson is a change row level security refused, such as one an assistant asks for.
var ErrNotInPerson = errors.New("a person changes their preferences themselves")

// Check refuses a time zone the IANA database does not name; Local would be the server's, never the person's.
func (p Preferences) Check() error {
	zone := p.TimeZone
	if zone == "" {
		return nil
	}
	if _, err := time.LoadLocation(zone); err != nil || zone == "Local" || len(zone) > config.MaxTimeZone || strings.TrimSpace(zone) != zone {
		return &Invalid{Fields: map[string]string{"timeZone": "Choose a time zone from the list, or follow your browser's."}}
	}
	return nil
}

// Service reads and changes the signed-in person's preferences, on their own row of app_user.
type Service struct{ db *db.Cluster }

// New returns the preferences' service.
func New(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectOwn = `SELECT coalesce(time_zone, '') FROM app_user WHERE id = current_actor_id()`

// Get is the signed-in person's preferences.
func (s *Service) Get(ctx context.Context) (Preferences, error) {
	if auth.PrincipalFrom(ctx) == nil {
		return Preferences{}, auth.ErrNoSession
	}
	var out Preferences
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, selectOwn).Scan(&out.TimeZone)
	})
	return out, err
}

// Set replaces the signed-in person's preferences; the policy lets nobody else's row be touched.
func (s *Service) Set(ctx context.Context, in Preferences) (Preferences, error) {
	if auth.PrincipalFrom(ctx) == nil {
		return Preferences{}, auth.ErrNoSession
	}
	if err := in.Check(); err != nil {
		return Preferences{}, err
	}
	var out Preferences
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `UPDATE app_user SET time_zone = NULLIF($1, '') WHERE id = current_actor_id()
			RETURNING coalesce(time_zone, '')`, in.TimeZone).Scan(&out.TimeZone)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotInPerson
		}
		return err
	})
	return out, err
}
