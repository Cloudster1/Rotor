//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
)

// backchannelWait bounds how long Keycloak's logout takes to reach the api and end the session.
const backchannelWait = 10 * time.Second

func TestAliceSignsInAsAnAdministratorAndBobAsAMember(t *testing.T) {
	h := newHarness(t)
	for _, person := range []struct{ username, role string }{{"alice", auth.RoleAdmin}, {"bob", auth.RoleMember}} {
		b := newBrowser(t)
		if landed := b.signIn(person.username); landed != "/runs" {
			t.Errorf("%s landed on %q, want /runs", person.username, landed)
		}
		status, me := b.me()
		user, _ := me["user"].(map[string]any)
		if status != http.StatusOK || me["role"] != person.role || user["email"] != person.username+"@suite.test" {
			t.Errorf("%s's me = %d %v", person.username, status, me)
		}
		var signedIn int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM audit_log a JOIN app_user u ON u.id = a.actor_user_id
			WHERE u.email = $1 AND a.action = $2 AND a.created_at > now() - interval '1 minute'`,
			person.username+"@suite.test", audit.ActionSignedIn).Scan(&signedIn); err != nil {
			t.Fatal(err)
		}
		if signedIn == 0 {
			t.Errorf("no %s entry for %s", audit.ActionSignedIn, person.username)
		}
	}
}

func TestARoleFollowsTheRealmAtTheNextSignIn(t *testing.T) {
	newHarness(t)
	kc := newKeycloakAdmin(t)
	carol, admins := kc.id("users", "carol"), kc.id("groups", "rotor-administrators")
	kc.call(http.MethodPut, "/users/"+carol+"/groups/"+admins, nil)
	t.Cleanup(func() { kc.call(http.MethodDelete, "/users/"+carol+"/groups/"+admins, nil) })

	b := newBrowser(t)
	b.signIn("carol")
	if _, me := b.me(); me["role"] != auth.RoleAdmin {
		t.Fatalf("carol in rotor-administrators is %v", me["role"])
	}
	kc.call(http.MethodDelete, "/users/"+carol+"/groups/"+admins, nil)
	b = newBrowser(t)
	b.signIn("carol")
	if _, me := b.me(); me["role"] != auth.RoleMember {
		t.Errorf("carol out of rotor-administrators is %v", me["role"])
	}
}

// Ending the person's sessions in Keycloak reaches the api through back-channel logout and ends Rotor's too.
func TestAKeycloakLogoutEndsTheSessionInRotor(t *testing.T) {
	newHarness(t)
	b := newBrowser(t)
	b.signIn("alice")
	if status, _ := b.me(); status != http.StatusOK {
		t.Fatalf("alice is not signed in: %d", status)
	}
	kc := newKeycloakAdmin(t)
	kc.call(http.MethodPost, "/users/"+kc.id("users", "alice")+"/logout", nil)

	deadline := time.Now().Add(backchannelWait)
	for {
		status, _ := b.me()
		if status == http.StatusUnauthorized {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("alice's Rotor session survived Keycloak's logout for %s (me answers %d)", backchannelWait, status)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestASwitchedOffPersonLosesTheirSessionsAndCannotSignInAgain(t *testing.T) {
	h := newHarness(t)
	b := newBrowser(t)
	b.signIn("dave")
	var dave uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = 'dave@suite.test'`).Scan(&dave); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET is_active = true WHERE id = $1`, dave); err != nil {
			t.Errorf("switch dave on again: %v", err)
		}
	})

	if err := auth.NewService(h.cluster, config.DefaultSessionTTL).SwitchOff(context.Background(), uuid.Nil, dave); err != nil {
		t.Fatal(err)
	}
	if status, _ := b.me(); status != http.StatusUnauthorized {
		t.Errorf("dave's session survived being switched off: %d", status)
	}
	if landed := newBrowser(t).signIn("dave"); landed != "/login?error=switched_off" {
		t.Errorf("a switched-off dave signing in landed on %q", landed)
	}
}
