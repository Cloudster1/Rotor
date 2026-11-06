//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// timeZoneOf reads a person's time zone behind the policies' back, as the database holds it.
func timeZoneOf(t *testing.T, h *harness, person uuid.UUID) *string {
	t.Helper()
	var zone *string
	if err := h.super.QueryRow(context.Background(), `SELECT time_zone FROM app_user WHERE id = $1`, person).Scan(&zone); err != nil {
		t.Fatal(err)
	}
	return zone
}

// A person chooses the time zone dates are written in for themselves (#206): the API reads and writes their own,
// refuses a zone the IANA database does not name, and nobody else's changes with it.
func TestAPersonChoosesTheirOwnTimeZone(t *testing.T) {
	h := newHarness(t)
	bob, carol := signedIn(t, "bob"), signedIn(t, "carol")
	bobID, carolID := signedInID(t, bob), signedInID(t, carol)
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `UPDATE app_user SET time_zone = NULL WHERE id = ANY($1)`, []uuid.UUID{bobID, carolID})
	})

	if status, got := bob.api(http.MethodPut, "/auth/me/preferences", map[string]any{"timeZone": nil}); status != http.StatusOK || got["timeZone"] != nil {
		t.Fatalf("following the browser = %d %v", status, got)
	}
	if status, got := bob.api(http.MethodPut, "/auth/me/preferences", map[string]any{"timeZone": "Pacific/Auckland"}); status != http.StatusOK || got["timeZone"] != "Pacific/Auckland" {
		t.Fatalf("choosing Auckland = %d %v", status, got)
	}
	if status, got := bob.api(http.MethodGet, "/auth/me/preferences", nil); status != http.StatusOK || got["timeZone"] != "Pacific/Auckland" {
		t.Errorf("bob reads back %d %v", status, got)
	}
	if status, got := carol.api(http.MethodGet, "/auth/me/preferences", nil); status != http.StatusOK || got["timeZone"] != nil {
		t.Errorf("carol, who chose nothing, reads %d %v", status, got)
	}
	if zone := timeZoneOf(t, h, carolID); zone != nil {
		t.Errorf("bob's choice reached carol's row: %q", *zone)
	}

	status, got := bob.api(http.MethodPut, "/auth/me/preferences", map[string]any{"timeZone": "Mars/Olympus"})
	fields, _ := got["error"].(map[string]any)["fields"].(map[string]any)
	if status != http.StatusUnprocessableEntity || fields["timeZone"] == nil {
		t.Errorf("an unknown zone = %d %v", status, got)
	}
	if zone := timeZoneOf(t, h, bobID); zone == nil || *zone != "Pacific/Auckland" {
		t.Errorf("a refused zone changed bob's to %v", zone)
	}

	if status, got := bob.api(http.MethodPut, "/auth/me/preferences", map[string]any{"timeZone": nil}); status != http.StatusOK || got["timeZone"] != nil {
		t.Errorf("following the browser again = %d %v", status, got)
	}
	if zone := timeZoneOf(t, h, bobID); zone != nil {
		t.Errorf("following the browser left %q", *zone)
	}
}

// The guard is the database's: as rotor_app a person changes their own time zone, never another person's, never
// anything else of their row, never through an assistant, and never to a name no zone has.
func TestTheDatabaseLetsAPersonChangeOnlyTheirOwnTimeZone(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	bob, carol := h.addPerson(t, "member"), h.addPerson(t, "member")

	update := func(person uuid.UUID, sql string, args ...any) (int64, error) {
		var changed int64
		err := asApp(ctx, h.super, person, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, sql, args...)
			changed = tag.RowsAffected()
			return err
		})
		return changed, err
	}

	if changed, err := update(bob, `UPDATE app_user SET time_zone = 'Europe/Berlin' WHERE id = $1`, bob); err != nil || changed != 1 {
		t.Errorf("bob's own time zone = %d rows, %v", changed, err)
	}
	if changed, err := update(bob, `UPDATE app_user SET time_zone = 'Europe/Berlin' WHERE id = $1`, carol); err != nil || changed != 0 {
		t.Errorf("carol's time zone, changed by bob = %d rows, %v; want none", changed, err)
	}
	if changed, err := update(bob, `UPDATE app_user SET time_zone = 'Europe/Berlin'`); err != nil || changed != 1 {
		t.Errorf("every time zone, changed by bob = %d rows, %v; want his own alone", changed, err)
	}
	_, err := update(bob, `UPDATE app_user SET name = 'Somebody else' WHERE id = $1`, bob)
	permissionDenied(t, "bob renaming himself", err)
	_, err = update(bob, `UPDATE app_user SET is_active = false WHERE id = $1`, carol)
	permissionDenied(t, "bob switching carol off", err)
	_, err = update(bob, `UPDATE app_user SET time_zone = '' WHERE id = $1`, bob)
	refusedBy(t, "a blank time zone", err, "23514", "app_user_time_zone_bounded")

	err = asApp(ctx, h.super, bob, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.via', $1, true)`, uuid.NewString()); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE app_user SET time_zone = 'Asia/Tokyo' WHERE id = $1`, bob)
		if err == nil && tag.RowsAffected() != 0 {
			t.Errorf("an assistant changed bob's time zone")
		}
		return err
	})
	if err != nil {
		t.Errorf("an assistant's change = %v, want no rows", err)
	}
}
