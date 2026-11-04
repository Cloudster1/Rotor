//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// waitingLanding is where the callback sends somebody the provider knows and nobody has let in yet (#204).
const waitingLanding = "/login?error=waiting"

// stranger is a person of the realm who has never signed in to Rotor, removed from both after the test.
type stranger struct {
	username string
	email    string
}

// newStranger makes a realm person, in the realm groups named, whose password is "<username> password".
func newStranger(t *testing.T, h *harness, prefix string, groups ...string) stranger {
	t.Helper()
	kc := newKeycloakAdmin(t)
	name := prefix + "-" + uuid.NewString()[:8]
	s := stranger{username: name, email: name + "@suite.test"}
	kc.send(http.MethodPost, "/users", map[string]any{
		"username": name, "email": s.email, "firstName": "Stranger", "lastName": prefix,
		"enabled": true, "emailVerified": true,
		"credentials": []map[string]any{{"type": "password", "value": name + " password", "temporary": false}},
	}, nil)
	id := kc.id("users", name)
	t.Cleanup(func() {
		newKeycloakAdmin(t).call(http.MethodDelete, "/users/"+id, nil)
		if _, err := h.super.Exec(context.Background(), `DELETE FROM app_user WHERE email = $1`, s.email); err != nil {
			t.Errorf("remove the stranger from Rotor: %v", err)
		}
	})
	for _, group := range groups {
		kc.call(http.MethodPut, "/users/"+id+"/groups/"+kc.id("groups", group), nil)
	}
	return s
}

// newRealmGroup makes a realm group, removed after the test with what Rotor made of it.
func newRealmGroup(t *testing.T, h *harness, prefix string) string {
	t.Helper()
	kc := newKeycloakAdmin(t)
	name := prefix + "-" + uuid.NewString()[:8]
	kc.send(http.MethodPost, "/groups", map[string]any{"name": name}, nil)
	id := kc.id("groups", name)
	t.Cleanup(func() {
		newKeycloakAdmin(t).call(http.MethodDelete, "/groups/"+id, nil)
		if _, err := h.super.Exec(context.Background(), `DELETE FROM user_group WHERE external_ref = $1`, name); err != nil {
			t.Errorf("remove the realm group from Rotor: %v", err)
		}
	})
	return name
}

// personByEmail is the id Rotor knows the address by, and whether they are let in and waiting.
func (h *harness) personByEmail(t *testing.T, email string) (id uuid.UUID, letIn, waiting bool) {
	t.Helper()
	if err := h.super.QueryRow(context.Background(), `
		SELECT u.id, u.standing IS NOT NULL, EXISTS (SELECT 1 FROM join_request r WHERE r.user_id = u.id)
		FROM app_user u WHERE u.email = $1`, email).Scan(&id, &letIn, &waiting); err != nil {
		t.Fatalf("find %s: %v", email, err)
	}
	return id, letIn, waiting
}

// inEveryone says whether a person is in the built-in Everyone.
func (h *harness) inEveryone(t *testing.T, person uuid.UUID) bool {
	t.Helper()
	var in bool
	if err := h.super.QueryRow(context.Background(), `
		SELECT EXISTS (SELECT 1 FROM group_member m JOIN user_group g ON g.id = m.group_id
		               WHERE g.builtin = 'everyone' AND m.user_id = $1)`, person).Scan(&in); err != nil {
		t.Fatal(err)
	}
	return in
}

// requested is the addresses Admin's Users page lists as waiting.
func requested(t *testing.T, admin *browser) []string {
	t.Helper()
	status, answer := admin.api(http.MethodGet, "/users/requests", nil)
	if status != http.StatusOK {
		t.Fatalf("the join requests = %d %v", status, answer)
	}
	return listed(answer, "requests", "email")
}

// providerSnapshot keeps single sign-on as it was and puts it back after the test, past every cache by its newer row.
func (h *harness) providerSnapshot(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var row map[string]any
	if err := h.super.QueryRow(ctx, `SELECT to_jsonb(p) - 'updated_at' - 'created_at' FROM oidc_provider p`).Scan(&row); err != nil {
		t.Fatalf("keep single sign-on as it is: %v", err)
	}
	t.Cleanup(func() {
		if _, err := h.super.Exec(ctx, `
			UPDATE oidc_provider p SET (issuer, client_id, groups_claim, admit_by_group, enabled, secret_set_at, changed_by) =
				(SELECT r.issuer, r.client_id, r.groups_claim, r.admit_by_group, r.enabled, r.secret_set_at, r.changed_by
				 FROM jsonb_populate_record(NULL::oidc_provider, $1) r)`, row); err != nil {
			t.Errorf("put single sign-on back: %v", err)
		}
	})
}

// providerBody is the PUT that keeps single sign-on as it is, with changes.
func providerBody(t *testing.T, admin *browser, change map[string]any) map[string]any {
	t.Helper()
	status, answer := admin.api(http.MethodGet, "/oidc-provider", nil)
	provider, _ := answer["provider"].(map[string]any)
	if status != http.StatusOK || provider == nil {
		t.Fatalf("single sign-on = %d %v", status, answer)
	}
	body := map[string]any{"issuer": provider["issuer"], "clientId": provider["clientId"], "groupsClaim": provider["groupsClaim"],
		"admitByGroup": provider["admitByGroup"]}
	for key, value := range change {
		body[key] = value
	}
	return body
}
