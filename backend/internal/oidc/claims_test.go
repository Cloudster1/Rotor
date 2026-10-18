package oidc

import (
	"errors"
	"slices"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/auth"
)

func TestAnIdentityIsReadFromTheClaims(t *testing.T) {
	got, err := identityFromClaims("https://id.test/realms/dynamo", "sub-1", map[string]any{
		"email": " Alice@Suite.TEST ", "email_verified": true, "preferred_username": "alice",
		"groups": []any{"/rotor-administrators", "engineering", "engineering", " "}, "sid": "s-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "alice@suite.test" || got.Name != "alice" || got.SID != "s-1" || got.Subject != "sub-1" {
		t.Errorf("identity = %+v", got)
	}
	if !slices.Equal(got.Groups, []string{"rotor-administrators", "engineering"}) {
		t.Errorf("groups = %q", got.Groups)
	}
}

func TestANameFallsBackToTheAddress(t *testing.T) {
	got, err := identityFromClaims("i", "s", map[string]any{"email": "bob@suite.test", "groups": "developers"})
	if err != nil || got.Name != "bob" || !slices.Equal(got.Groups, []string{"developers"}) {
		t.Fatalf("identity = %+v, %v", got, err)
	}
}

func TestAnAddressIsNeededAndMustBeVerified(t *testing.T) {
	if _, err := identityFromClaims("i", "s", map[string]any{"name": "Nobody"}); !errors.Is(err, ErrNoEmail) {
		t.Errorf("no address = %v", err)
	}
	for _, unverified := range []any{false, "false"} {
		_, err := identityFromClaims("i", "s", map[string]any{"email": "x@suite.test", "email_verified": unverified})
		if !errors.Is(err, ErrEmailUnverified) {
			t.Errorf("email_verified %v = %v", unverified, err)
		}
	}
}

func TestRotorAdministratorsAreAdministratorsAndEveryoneElseAMember(t *testing.T) {
	if RoleFor([]string{"engineering", AdminGroup}) != auth.RoleAdmin {
		t.Error("an administrator of Rotor is not an administrator")
	}
	for _, groups := range [][]string{nil, {"exciter-administrators", "readers"}} {
		if RoleFor(groups) != auth.RoleMember {
			t.Errorf("groups %q do not make a member", groups)
		}
	}
}

func TestRewritesAreParsed(t *testing.T) {
	got := ParseRewrites(" http://localhost:30042/ = http://keycloak:8080 ,broken, =x")
	if len(got) != 1 || got["http://localhost:30042"] != "http://keycloak:8080" {
		t.Fatalf("rewrites = %v", got)
	}
}
