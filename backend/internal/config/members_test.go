package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestBootstrapMembersAreReadByAddressWithAnOptionalRole(t *testing.T) {
	clean(t)
	t.Setenv(BootstrapMembersVar, " Alice@Suite.test=admin, bob@suite.test\ncarol@suite.test=member dave@suite.test=owner,,bob@suite.test ")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []Member{{Email: "alice@suite.test", Admin: true}, {Email: "bob@suite.test"}, {Email: "carol@suite.test"},
		{Email: "dave@suite.test", Admin: true}}
	if !reflect.DeepEqual(c.BootstrapMembers, want) {
		t.Errorf("members = %+v, want %+v", c.BootstrapMembers, want)
	}
}

func TestNoBootstrapMembersIsNobody(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil || len(c.BootstrapMembers) != 0 {
		t.Errorf("members = %+v, %v", c.BootstrapMembers, err)
	}
}

func TestAMalformedBootstrapMemberStopsTheStartNamingTheEntry(t *testing.T) {
	for _, entry := range []string{"alice", "@suite.test", "alice@", "alice@suite.test=viewer", "a@b@c", "<alice@suite.test>"} {
		t.Run(entry, func(t *testing.T) {
			clean(t)
			t.Setenv(BootstrapMembersVar, "bob@suite.test,"+entry)
			_, err := Load()
			var problems *Error
			if !errors.As(err, &problems) || len(problems.Problems) != 1 {
				t.Fatalf("Load = %v, want one problem", err)
			}
			got := problems.Problems[0]
			if !strings.Contains(got, BootstrapMembersVar) || !strings.Contains(got, `"`+entry+`"`) || !strings.HasSuffix(got, ".") {
				t.Errorf("the problem %q does not name %q in a sentence", got, entry)
			}
		})
	}
}

func TestTheGroupsClaimDefaultsToTheRealmsMapper(t *testing.T) {
	clean(t)
	c, err := Load()
	if err != nil || c.OIDC.GroupsClaim != DefaultGroupsClaim {
		t.Errorf("groups claim = %q, %v", c.OIDC.GroupsClaim, err)
	}
	t.Setenv("ROTOR_OIDC_GROUPS_CLAIM", "roles")
	if c, err = Load(); err != nil || c.OIDC.GroupsClaim != "roles" {
		t.Errorf("groups claim = %q, %v", c.OIDC.GroupsClaim, err)
	}
}
