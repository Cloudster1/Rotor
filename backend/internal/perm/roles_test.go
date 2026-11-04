package perm

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/config"
)

func TestAKeyIsMadeFromTheName(t *testing.T) {
	for name, want := range map[string]Role{
		"Reviewer":                "reviewer",
		"  Release manager  ":     "release_manager",
		"QA / Sign-off":           "qa_sign_off",
		"2nd line":                "r_2nd_line",
		"Ops: on call, weekends!": "ops_on_call_weekends",
	} {
		if got := KeyFromName(name); got != want {
			t.Errorf("KeyFromName(%q) = %q, want %q", name, got, want)
		}
	}
}

// A long name still makes a key the database takes, and never one that ends on the joining underscore.
func TestAKeyFromALongNameFitsTheDatabase(t *testing.T) {
	key := string(KeyFromName(strings.Repeat("word ", 20)))
	if len(key) > config.MaxRoleKey || strings.HasSuffix(key, "_") || !roleKey.MatchString(key) {
		t.Errorf("KeyFromName of a long name = %q, want a key of at most %d that the shape allows", key, config.MaxRoleKey)
	}
}

func TestPermissionsAreKeptInTheMatrixOrderOnceEach(t *testing.T) {
	got, err := cleanPermissions([]Permission{WorkflowRun, WorkflowRead, WorkflowRun})
	if err != nil || !slices.Equal(got, []string{"workflow.read", "workflow.run"}) {
		t.Errorf("cleanPermissions = %v, %v; want read then run, once each", got, err)
	}
	if got, err := cleanPermissions(nil); err != nil || got == nil || len(got) != 0 {
		t.Errorf("no permissions = %v, %v; want an empty list, which grants nothing", got, err)
	}
	if _, err := cleanPermissions([]Permission{"workflow.fly"}); !errors.Is(err, ErrUnknownPermission) {
		t.Errorf("a permission nobody checks for = %v, want ErrUnknownPermission", err)
	}
}
