package connections

import (
	"errors"
	"testing"
)

func TestATokenIsNarrowedAtTheToolOnlyWhenItCarriesWhatWasAsked(t *testing.T) {
	for _, tc := range []struct {
		asked, carried []string
		want           bool
	}{
		{[]string{"mcp", "mcp:read"}, []string{"mcp:read", "mcp"}, true},
		{[]string{"mcp:read"}, []string{"mcp", "mcp:read"}, false},
		{[]string{"mcp", "mcp"}, []string{"mcp"}, true},
	} {
		if got := sameScopes(tc.asked, tc.carried); got != tc.want {
			t.Errorf("sameScopes(%v, %v) = %v, want %v", tc.asked, tc.carried, got, tc.want)
		}
	}
}

func TestAScopeRefusalNamesWhatIsMissing(t *testing.T) {
	err := error(&ScopeError{Missing: []string{"evidence"}})
	if !errors.Is(err, ErrScopeNotConsented) || err.Error() != "the person did not consent to evidence" {
		t.Errorf("a scope refusal reads %q", err)
	}
}
