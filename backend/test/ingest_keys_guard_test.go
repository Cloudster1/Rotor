//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Keys are administrators' alone: a member is refused by the api and, straight through SQL as rotor_app, by the
// database, which also holds the cap whoever inserts.
func TestOnlyAdministratorsManageIngestKeys(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	adminID, memberID := h.addPerson(t, "admin"), h.addPerson(t, "member")
	admin, member := h.browserFor(t, adminID), h.browserFor(t, memberID)
	suffix := uuid.NewString()[:8]
	key := makeKey(t, h, admin, map[string]any{"name": "Guarded " + suffix})

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/settings/ingest-keys"},
		{http.MethodPost, "/settings/ingest-keys"},
		{http.MethodPost, "/settings/ingest-keys/" + key.id + "/rotate"},
		{http.MethodDelete, "/settings/ingest-keys/" + key.id},
	} {
		if status, _ := member.api(c.method, c.path, map[string]any{"name": "Mine " + suffix}); status != http.StatusForbidden {
			t.Errorf("a member at %s %s = %d, want 403", c.method, c.path, status)
		}
	}

	insert := `INSERT INTO ingest_key (name, account_id, secret_hash, hint) VALUES ($1, $2, $3, 'abcd')`
	err := asApp(ctx, h.super, memberID, func(tx pgx.Tx) error {
		var seen int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ingest_key`).Scan(&seen); err != nil || seen != 0 {
			t.Errorf("a member sees %d keys through SQL (%v)", seen, err)
		}
		if tag, err := tx.Exec(ctx, `UPDATE ingest_key SET account_id = $1`, memberID); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("a member changed %d keys through SQL (%v)", tag.RowsAffected(), err)
		}
		if tag, err := tx.Exec(ctx, `DELETE FROM ingest_key`); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("a member revoked %d keys through SQL (%v)", tag.RowsAffected(), err)
		}
		_, err := tx.Exec(ctx, insert, "Member "+suffix, memberID, randomHash())
		return err
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Errorf("a member making a key through SQL = %v, want the policy to refuse it", err)
	}

	// An administrator fills the keys to the cap and is refused the next, in a transaction rolled back after.
	err = asApp(ctx, h.super, adminID, func(tx pgx.Tx) error {
		var held int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ingest_key`).Scan(&held); err != nil {
			return err
		}
		for i := held; i <= config.MaxIngestKeys; i++ {
			if _, err := tx.Exec(ctx, insert, "Fill "+strconv.Itoa(i)+" "+suffix, adminID, randomHash()); err != nil {
				return err
			}
		}
		return nil
	})
	refusedBy(t, "a key past the cap through SQL", err, "23514", "ingest_key_cap")

	if status, answer := admin.api(http.MethodPost, "/settings/ingest-keys", map[string]any{"name": "guarded " + suffix}); status != http.StatusConflict ||
		errorCode(answer) != "ingest_key_taken" {
		t.Errorf("a name taken, in another case = %d %v", status, answer)
	}
	if status, answer := admin.api(http.MethodPost, "/settings/ingest-keys", map[string]any{"name": "Nobody " + suffix,
		"accountEmail": "nobody-" + suffix + "@suite.test"}); status != http.StatusUnprocessableEntity || errorCode(answer) != "validation_failed" {
		t.Errorf("a key for an account Rotor never let in = %d %v", status, answer)
	}
}

// Only the key's SHA-256 is kept, its last characters to tell it apart; neither a list, the audit log nor the row
// ever holds the key again.
func TestAnIngestKeyIsStoredOnlyHashed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	adminID := h.addPerson(t, "admin")
	admin := h.browserFor(t, adminID)
	key := makeKey(t, h, admin, map[string]any{"name": "Hashed " + uuid.NewString()[:8]})
	if !strings.HasPrefix(key.secret, "rotor_ik_") || key.key["hint"] != key.secret[len(key.secret)-config.IngestKeyHintChars:] {
		t.Fatalf("the key %q is not shown with its hint %v", key.secret, key.key["hint"])
	}
	if account, _ := key.key["account"].(map[string]any); account["id"] != adminID.String() {
		t.Errorf("a key made without an account sends as %v, want the administrator", key.key["account"])
	}

	var row string
	var digest []byte
	if err := h.super.QueryRow(ctx, `SELECT k::text, secret_hash FROM ingest_key k WHERE id = $1`, key.id).Scan(&row, &digest); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(key.secret))
	if !bytes.Equal(digest, sum[:]) || strings.Contains(row, key.secret) || strings.Contains(row, key.secret[len("rotor_ik_"):]) {
		t.Errorf("the row %q does not hold the key's SHA-256 alone", row)
	}

	status, rotated := admin.api(http.MethodPost, "/settings/ingest-keys/"+key.id+"/rotate", nil)
	fresh, _ := rotated["secret"].(string)
	if status != http.StatusOK || fresh == "" {
		t.Fatalf("rotating = %d %v", status, rotated)
	}
	_, list := admin.api(http.MethodGet, "/settings/ingest-keys", nil)
	listed, _ := json.Marshal(list)
	var audited string
	if err := h.super.QueryRow(ctx, `SELECT string_agg(action || ' ' || data::text, ' ') FROM audit_log WHERE target_id = $1`,
		key.id).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{key.secret, fresh} {
		if strings.Contains(audited, secret) || strings.Contains(string(listed), secret) {
			t.Errorf("a key reached the audit log %q or the list", audited)
		}
	}
	if !strings.Contains(audited, "ingest_key.created") || !strings.Contains(audited, "ingest_key.rotated") {
		t.Errorf("the audit log holds %q, want the key made and rotated", audited)
	}
}

func randomHash() []byte {
	sum := sha256.Sum256([]byte(uuid.NewString()))
	return sum[:]
}
