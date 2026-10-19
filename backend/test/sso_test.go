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
	"github.com/cloudster1/rotor/backend/internal/oidc"
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

// Ending the session in Keycloak reaches the api through back-channel logout and ends Rotor's too, found by the
// provider's session id Rotor keeps.
func TestAKeycloakLogoutEndsTheSessionInRotor(t *testing.T) {
	h := newHarness(t)
	b := newBrowser(t)
	b.signIn("alice")
	if status, _ := b.me(); status != http.StatusOK {
		t.Fatalf("alice is not signed in: %d", status)
	}
	var sid string
	if err := h.super.QueryRow(context.Background(), `
		SELECT s.oidc_sid FROM user_session s JOIN app_user u ON u.id = s.user_id
		WHERE u.email = 'alice@suite.test' ORDER BY s.created_at DESC LIMIT 1`).Scan(&sid); err != nil || sid == "" {
		t.Fatalf("alice's session keeps no provider session id: %q %v", sid, err)
	}
	newKeycloakAdmin(t).call(http.MethodDelete, "/sessions/"+sid, nil)

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

// Ending one of a person's sessions at the provider ends only the Rotor session it signed in, not their others.
func TestEndingOneProviderSessionLeavesThePersonsOthers(t *testing.T) {
	h := newHarness(t)
	first, second := newBrowser(t), newBrowser(t)
	first.signIn("bob")
	second.signIn("bob")
	var sids []string
	rows, err := h.super.Query(context.Background(), `
		SELECT s.oidc_sid FROM user_session s JOIN app_user u ON u.id = s.user_id
		WHERE u.email = 'bob@suite.test' ORDER BY s.created_at DESC LIMIT 2`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			t.Fatal(err)
		}
		sids = append(sids, sid)
	}
	rows.Close()
	if len(sids) != 2 || sids[0] == sids[1] {
		t.Fatalf("bob's two newest sessions name provider sessions %q", sids)
	}
	// Newest first, so the second entry is the first browser's.
	newKeycloakAdmin(t).call(http.MethodDelete, "/sessions/"+sids[1], nil)

	deadline := time.Now().Add(backchannelWait)
	for {
		if status, _ := first.me(); status == http.StatusUnauthorized {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the first browser's session survived its provider session for %s", backchannelWait)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if status, _ := second.me(); status != http.StatusOK {
		t.Errorf("ending bob's first provider session ended his second Rotor session too: %d", status)
	}
}

// A logout naming only the person, as the specification allows, ends every session they have.
func TestALogoutNamingOnlyThePersonEndsAllTheirSessions(t *testing.T) {
	h := newHarness(t)
	first, second := newBrowser(t), newBrowser(t)
	first.signIn("carol")
	second.signIn("carol")
	var issuer, subject string
	if err := h.super.QueryRow(context.Background(), `
		SELECT i.issuer, i.subject FROM user_identity i JOIN app_user u ON u.id = i.user_id WHERE u.email = 'carol@suite.test'`,
	).Scan(&issuer, &subject); err != nil {
		t.Fatal(err)
	}
	signIn := oidc.NewService(h.cluster, nil, h.cfg.OIDC)
	if err := signIn.EndSessions(context.Background(), &oidc.Logout{Issuer: issuer, Subject: subject, JTI: uuid.NewString(),
		Expires: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	for i, b := range []*browser{first, second} {
		if status, _ := b.me(); status != http.StatusUnauthorized {
			t.Errorf("carol's session %d survived a logout naming her: %d", i+1, status)
		}
	}
}

// Signing a person out of Keycloak, as an administrator does when an account is compromised, ends every Rotor
// session they have, not only the one Keycloak's logout token names (#53).
func TestKeycloaksSignOutOfAPersonEndsAllTheirSessions(t *testing.T) {
	newHarness(t)
	first, second := newBrowser(t), newBrowser(t)
	first.signIn("carol")
	second.signIn("carol")
	admin := newKeycloakAdmin(t)
	admin.call(http.MethodPost, "/users/"+admin.id("users", "carol")+"/logout", nil)

	deadline := time.Now().Add(backchannelWait)
	for i, b := range []*browser{first, second} {
		for {
			status, _ := b.me()
			if status == http.StatusUnauthorized {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("carol's session %d survived Keycloak signing her out for %s (me answers %d)", i+1, backchannelWait, status)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}
