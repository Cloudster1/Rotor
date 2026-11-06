//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

// A name on ROTOR_BOOTSTRAP_MEMBERS is let in ahead of their first sign-in, which then needs nobody (#171); the api
// runs the same at every start.
func TestABootstrapMemberSignsInWithoutAJoinRequest(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	named, later := newStranger(t, h, "listed"), newStranger(t, h, "later")
	if added, err := perm.LetInMembers(ctx, h.cluster, []config.Member{{Email: named.email}}); err != nil || added != 1 {
		t.Fatalf("letting the list in = %d, %v", added, err)
	}
	if added, err := perm.LetInMembers(ctx, h.cluster, []config.Member{{Email: named.email}}); err != nil || added != 0 {
		t.Errorf("the same list again let in %d (%v)", added, err)
	}
	// A later start with another list adds its names and lets nobody out.
	if added, err := perm.LetInMembers(ctx, h.cluster, []config.Member{{Email: later.email, Admin: true}}); err != nil || added != 1 {
		t.Fatalf("a later list = %d, %v", added, err)
	}
	if _, letIn, _ := h.personByEmail(t, named.email); !letIn {
		t.Error("a later list let the first name out")
	}
	b := newBrowser(t)
	if landed := b.signIn(named.username); landed != "/runs" {
		t.Fatalf("a bootstrap member landed on %q", landed)
	}
	id, letIn, waiting := h.personByEmail(t, named.email)
	if !letIn || waiting || !h.inEveryone(t, id) {
		t.Errorf("a bootstrap member is let in %v, waiting %v", letIn, waiting)
	}
	var people int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM app_user WHERE email = $1`, named.email).Scan(&people); err != nil || people != 1 {
		t.Errorf("the first sign-in made %d people of the name (%v)", people, err)
	}
	// admin on the list grants Administrator to whom it lets in.
	b = newBrowser(t)
	b.signIn(later.username)
	if _, me := b.me(); me["role"] != "admin" {
		t.Errorf("a member listed as admin is %v", me["role"])
	}
}

// Somebody already waiting when their name is added is let in at the next start, and their request goes.
func TestABootstrapMemberWhoWaitedIsLetInAtTheNextStart(t *testing.T) {
	h := newHarness(t)
	s := newStranger(t, h, "waited")
	if landed := newBrowser(t).signIn(s.username); landed != waitingLanding {
		t.Fatalf("a stranger landed on %q", landed)
	}
	if added, err := perm.LetInMembers(context.Background(), h.cluster, []config.Member{{Email: s.email}}); err != nil || added != 1 {
		t.Fatalf("letting the waiting name in = %d, %v", added, err)
	}
	if _, letIn, waiting := h.personByEmail(t, s.email); !letIn || waiting {
		t.Errorf("after the start the name is let in %v and waiting %v", letIn, waiting)
	}
	if landed := newBrowser(t).signIn(s.username); landed != "/runs" {
		t.Errorf("the name let in at the start landed on %q", landed)
	}
}
