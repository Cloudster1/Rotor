//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Destinations are administrators' alone, at most config.MaxTraceDestinations of them, and text leaves for one only
// once acknowledged: the api refuses each, and so does the database, straight through SQL as rotor_app.
func TestTraceDestinationsAreAdministratorsAloneAndBounded(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	stub := newToolStub(t, map[string]any{})
	alice := newBrowser(t)
	alice.signIn("alice")
	bob := newBrowser(t)
	bob.signIn("bob")
	_, endpoint := newSink(t, stub)
	suffix := uuid.NewString()[:8]

	if status, _ := bob.api(http.MethodGet, "/settings/trace-destinations", nil); status != http.StatusForbidden {
		t.Errorf("a member listing destinations = %d, want 403", status)
	}
	if status, _ := bob.api(http.MethodPost, "/settings/trace-destinations", map[string]any{"name": "Mine " + suffix,
		"endpoint": endpoint}); status != http.StatusForbidden {
		t.Errorf("a member adding a destination = %d, want 403", status)
	}

	refusals := []struct {
		what  string
		body  map[string]any
		code  string
		field string
	}{
		{"text unacknowledged", map[string]any{"name": "Text " + suffix, "endpoint": endpoint, "content": true}, "content_leaves_unacknowledged", ""},
		{"a header Rotor sets", map[string]any{"name": "Host " + suffix, "endpoint": endpoint,
			"headers": map[string]string{"Host": "elsewhere"}}, "validation_failed", "headers"},
		{"an endpoint with a user", map[string]any{"name": "User " + suffix, "endpoint": "http://me:pw@jaeger:4318/v1/traces"},
			"validation_failed", "endpoint"},
		{"a link without the trace", map[string]any{"name": "Link " + suffix, "endpoint": endpoint, "traceLink": "http://jaeger/search"},
			"validation_failed", "traceLink"},
	}
	for _, r := range refusals {
		status, answer := alice.api(http.MethodPost, "/settings/trace-destinations", r.body)
		fields, _ := answer["error"].(map[string]any)["fields"].(map[string]any)
		if status != http.StatusUnprocessableEntity || errorCode(answer) != r.code || (r.field != "" && fields[r.field] == nil) {
			t.Errorf("%s = %d %v", r.what, status, answer)
		}
	}

	first := addDestination(t, alice, map[string]any{"name": "First " + suffix, "endpoint": endpoint,
		"headers": map[string]string{"x-api-key": "held"}})
	if status, answer := alice.api(http.MethodPost, "/settings/trace-destinations", map[string]any{"name": "first " + suffix,
		"endpoint": endpoint}); status != http.StatusConflict || errorCode(answer) != "trace_destination_taken" {
		t.Errorf("a name taken, in another case = %d %v", status, answer)
	}

	admin, member := h.addPerson(t, "admin"), h.addPerson(t, "member")
	insert := `INSERT INTO trace_destination (name, endpoint) VALUES ($1, $2)`
	// A member is refused by the policy before the cap would refuse anyone.
	err := asApp(ctx, h.super, member, func(tx pgx.Tx) error {
		var seen int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM trace_destination`).Scan(&seen); err != nil || seen != 0 {
			t.Errorf("a member sees %d destinations through SQL (%v)", seen, err)
		}
		if tag, err := tx.Exec(ctx, `UPDATE trace_destination SET enabled = true`); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("a member changed %d destinations through SQL (%v)", tag.RowsAffected(), err)
		}
		if tag, err := tx.Exec(ctx, `DELETE FROM trace_destination`); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("a member removed %d destinations through SQL (%v)", tag.RowsAffected(), err)
		}
		_, err := tx.Exec(ctx, insert, "Member "+suffix, endpoint)
		return err
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Errorf("a member adding a destination through SQL = %v, want the policy to refuse it", err)
	}

	// The api fills up to the cap and refuses the next.
	_, list := alice.api(http.MethodGet, "/settings/trace-destinations", nil)
	for i := len(list["destinations"].([]any)); i < config.MaxTraceDestinations; i++ {
		addDestination(t, alice, map[string]any{"name": "Fill " + strconv.Itoa(i) + " " + suffix, "endpoint": endpoint})
	}
	if status, answer := alice.api(http.MethodPost, "/settings/trace-destinations", map[string]any{"name": "Ninth " + suffix,
		"endpoint": endpoint}); status != http.StatusUnprocessableEntity || errorCode(answer) != "trace_destinations_full" {
		t.Errorf("a destination past the cap = %d %v", status, answer)
	}

	err = asApp(ctx, h.super, admin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, insert, "Straight "+suffix, endpoint)
		return err
	})
	refusedBy(t, "a destination past the cap through SQL", err, "23514", "trace_destination_cap")
	err = asApp(ctx, h.super, admin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE trace_destination SET content = true WHERE id = $1`, first["id"])
		return err
	})
	refusedBy(t, "text unacknowledged through SQL", err, "23514", "trace_destination_content_acknowledged")

	// Removing one forgets its headers in OpenBao too.
	secrets, _ := newVault(t, os.Getenv(envOpenBao), config.DefaultOpenBaoRole)
	id := uuid.MustParse(first["id"].(string))
	if held, err := secrets.DestinationHeaders(ctx, id); err != nil || held.Headers["x-api-key"] != "held" {
		t.Fatalf("the destination's headers read %v, %v", held, err)
	}
	if status, _ := alice.api(http.MethodDelete, "/settings/trace-destinations/"+id.String(), nil); status != http.StatusNoContent {
		t.Fatalf("removing a destination = %d", status)
	}
	if status, _ := alice.api(http.MethodGet, "/settings/trace-destinations/"+id.String(), nil); status != http.StatusNotFound {
		t.Errorf("a removed destination reads %d", status)
	}
	if _, err := secrets.DestinationHeaders(ctx, id); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("a removed destination's headers read %v", err)
	}
}
