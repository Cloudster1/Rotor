//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/auth"
)

// devSecret is the dev realm's secret of the rotor client, which the stack's environment seeds.
const devSecret = "rotor-dev-secret"

// rawAPI is the body the api answers, as text, so a test can look for what must never be in it.
func rawAPI(t *testing.T, b *browser, method, path string, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, b.app+"/api/v1"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, text := b.do(req)
	return resp.StatusCode, text
}

// The client secret is kept in OpenBao: no response, row or audit entry holds it, and a blank one keeps it (#204).
func TestTheClientSecretIsKeptOutOfSight(t *testing.T) {
	h := newHarness(t)
	h.providerSnapshot(t)
	admin := h.browserFor(t, h.addPerson(t, auth.RoleAdmin))
	status, text := rawAPI(t, admin, http.MethodGet, "/oidc-provider", "")
	if status != http.StatusOK || strings.Contains(text, devSecret) || !strings.Contains(text, `"hasSecret":true`) {
		t.Fatalf("single sign-on = %d %s", status, text)
	}
	body := providerBody(t, admin, map[string]any{"clientSecret": devSecret})
	status, answer := admin.api(http.MethodPut, "/oidc-provider", body)
	if status != http.StatusOK {
		t.Fatalf("saving the secret = %d %v", status, answer)
	}
	if _, text := rawAPI(t, admin, http.MethodGet, "/oidc-provider", ""); strings.Contains(text, devSecret) {
		t.Error("single sign-on answers the secret once it is in OpenBao")
	}
	if strings.Contains(fmt.Sprint(answer), devSecret) {
		t.Error("saving answers the secret")
	}
	ctx := context.Background()
	var row, entries string
	var stored bool
	if err := h.super.QueryRow(ctx, `SELECT to_jsonb(p)::text, secret_set_at IS NOT NULL FROM oidc_provider p`).Scan(&row, &stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row, devSecret) || !stored {
		t.Errorf("the provider row %s holds the secret, or says OpenBao does not (%v)", row, stored)
	}
	if err := h.super.QueryRow(ctx, `SELECT COALESCE(string_agg(data::text, ' '), '') FROM audit_log
		WHERE action = 'oidc.saved' AND created_at > now() - interval '5 minutes'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entries, devSecret) || !strings.Contains(entries, `"secret": "set"`) {
		t.Errorf("the audit log of saving holds the secret or does not say it was set: %s", entries)
	}
	// Signing in reads the secret from OpenBao now, and a blank one on save keeps it.
	if landed := newBrowser(t).signIn("bob"); landed != "/runs" {
		t.Errorf("signing in with the secret from OpenBao landed on %q", landed)
	}
	if status, answer := admin.api(http.MethodPut, "/oidc-provider", providerBody(t, admin, nil)); status != http.StatusOK {
		t.Fatalf("saving with a blank secret = %d %v", status, answer)
	} else if provider, _ := answer["provider"].(map[string]any); provider["hasSecret"] != true {
		t.Errorf("a blank secret dropped the one held: %v", answer)
	}
	if landed := newBrowser(t).signIn("bob"); landed != "/runs" {
		t.Errorf("after keeping the secret, signing in landed on %q", landed)
	}
}

// A change saved applies at the next sign-in, without a restart: the next start sends the browser to the new client.
func TestTheProviderChangedLiveAppliesAtTheNextSignIn(t *testing.T) {
	h := newHarness(t)
	h.providerSnapshot(t)
	admin := h.browserFor(t, h.addPerson(t, auth.RoleAdmin))
	clientAt := func() string {
		resp, body := newBrowser(t).get(newBrowser(t).app + "/api/v1/auth/oidc/start")
		target, err := url.Parse(resp.Header.Get("Location"))
		if resp.StatusCode != http.StatusFound || err != nil {
			t.Fatalf("start = %d %s", resp.StatusCode, body)
		}
		return target.Query().Get("client_id")
	}
	if got := clientAt(); got != "rotor" {
		t.Fatalf("the start sends people to client %q", got)
	}
	if status, answer := admin.api(http.MethodPut, "/oidc-provider", providerBody(t, admin, map[string]any{"clientId": "rotor-elsewhere"})); status != http.StatusOK {
		t.Fatalf("changing the client = %d %v", status, answer)
	}
	if got := clientAt(); got != "rotor-elsewhere" {
		t.Errorf("after the change the start sends people to client %q", got)
	}
	// Changing it back keeps the secret the environment gave, which went to OpenBao when the client changed.
	if status, answer := admin.api(http.MethodPut, "/oidc-provider", providerBody(t, admin, map[string]any{"clientId": "rotor"})); status != http.StatusOK {
		t.Fatalf("changing the client back = %d %v", status, answer)
	}
	if landed := newBrowser(t).signIn("bob"); landed != "/runs" {
		t.Errorf("after changing back, signing in landed on %q", landed)
	}
}

// A provider that could not work is refused with a sentence for each field, and turning it off while it is the only
// way in is refused saying why.
func TestASingleSignOnThatCouldNotWorkIsRefused(t *testing.T) {
	h := newHarness(t)
	h.providerSnapshot(t)
	admin := h.browserFor(t, h.addPerson(t, auth.RoleAdmin))
	for name, c := range map[string]struct {
		change map[string]any
		status int
		field  string
	}{
		"no issuer":             {map[string]any{"issuer": " "}, http.StatusUnprocessableEntity, "issuer"},
		"no client":             {map[string]any{"clientId": ""}, http.StatusUnprocessableEntity, "clientId"},
		"not an address":        {map[string]any{"issuer": "id.example.com"}, http.StatusUnprocessableEntity, "issuer"},
		"an issuer that is not": {map[string]any{"issuer": "http://nowhere.invalid/realms/dynamo"}, http.StatusUnprocessableEntity, "issuer"},
		"turned off":            {map[string]any{"enabled": false}, http.StatusConflict, ""},
	} {
		status, answer := admin.api(http.MethodPut, "/oidc-provider", providerBody(t, admin, c.change))
		refusal, _ := answer["error"].(map[string]any)
		fields, _ := refusal["fields"].(map[string]any)
		message, _ := refusal["message"].(string)
		if status != c.status || (c.field != "" && fields[c.field] == nil) || !strings.HasSuffix(message, ".") {
			t.Errorf("%s = %d %v, want %d naming %q in a sentence", name, status, answer, c.status, c.field)
		}
	}
	member := h.browserFor(t, h.addPerson(t, auth.RoleMember))
	if status, _ := member.api(http.MethodGet, "/oidc-provider", nil); status != http.StatusForbidden {
		t.Errorf("a member reading single sign-on = %d", status)
	}
	if status, _ := member.api(http.MethodPut, "/oidc-provider", providerBody(t, admin, nil)); status != http.StatusForbidden {
		t.Errorf("a member saving single sign-on = %d", status)
	}
	if landed := newBrowser(t).signIn("bob"); landed != "/runs" {
		t.Errorf("after the refusals, signing in landed on %q", landed)
	}
}

// The same refusals straight through SQL as rotor_app: single sign-on is the administrators' alone, join requests and
// standing are nobody's to write, and somebody waiting holds nothing.
func TestLettingInIsRefusedByTheDatabaseToo(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin, member := h.addPerson(t, auth.RoleAdmin), h.addPerson(t, auth.RoleMember)
	pool := h.super

	var seen int
	if err := asApp(ctx, pool, member, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM oidc_provider`).Scan(&seen)
	}); err != nil || seen != 0 {
		t.Errorf("a member sees %d providers (%v)", seen, err)
	}
	err := asApp(ctx, pool, member, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE oidc_provider SET client_id = 'mine'`)
		if err == nil && tag.RowsAffected() != 0 {
			t.Errorf("a member changed single sign-on")
		}
		return err
	})
	if err != nil {
		t.Errorf("a member's update of single sign-on = %v, want no rows", err)
	}
	refusedBy(t, "a member adding a provider", asApp(ctx, pool, member, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO oidc_provider (one_row, issuer, client_id) VALUES (true, 'http://x', 'x')
			ON CONFLICT (one_row) DO UPDATE SET client_id = 'x'`)
		return err
	}), "42501", "")
	permissionDenied(t, "an administrator deleting single sign-on", asApp(ctx, pool, admin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM oidc_provider`)
		return err
	}))
	refusedBy(t, "an administrator blanking the issuer", asApp(ctx, pool, admin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE oidc_provider SET issuer = ' '`)
		return err
	}), "23514", "oidc_provider_issuer_not_blank")
	refusedBy(t, "an administrator adding a second provider", asApp(ctx, pool, admin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO oidc_provider (one_row, issuer, client_id) VALUES (false, 'http://x', 'x')`)
		return err
	}), "23514", "oidc_provider_one_row")
	for what, statement := range map[string]string{
		"reading join requests":    `SELECT count(*) FROM join_request`,
		"asking on their behalf":   `INSERT INTO join_request (user_id) VALUES ('` + member.String() + `')`,
		"settling a request":       `DELETE FROM join_request`,
		"letting somebody in":      `UPDATE app_user SET standing = 'member' WHERE standing IS NULL`,
		"letting nobody in at all": `UPDATE app_user SET standing = NULL`,
	} {
		permissionDenied(t, "an administrator "+what+" as rotor_app", asApp(ctx, pool, admin, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, statement)
			return err
		}))
	}

	// Somebody waiting holds nothing, even granted something directly, and never joins Everyone.
	var waiting uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ('waiting-'||gen_random_uuid()||'@suite.test', 'Waiting')
		RETURNING id`).Scan(&waiting); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, waiting) })
	if _, err := pool.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('editor', $1)`, waiting); err != nil {
		t.Fatal(err)
	}
	var can bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM group_member m JOIN user_group g ON g.id = m.group_id
		WHERE g.builtin = 'everyone' AND m.user_id = $1)`, waiting).Scan(&can); err != nil || can {
		t.Errorf("somebody waiting is in Everyone (%v)", err)
	}
	refusedBy(t, "putting somebody waiting in Everyone", func() error {
		_, err := pool.Exec(ctx, `INSERT INTO group_member (group_id, user_id) SELECT id, $1 FROM user_group WHERE builtin = 'everyone'`, waiting)
		return err
	}(), "23514", "group_member_everyone_let_in")
	if err := asApp(ctx, pool, waiting, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT actor_can('workflow.edit', NULL) OR actor_can('workflow.create', NULL)`).Scan(&can)
	}); err != nil || can {
		t.Errorf("somebody waiting holds what was granted to them (%v)", err)
	}
}
