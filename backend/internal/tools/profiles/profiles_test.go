package profiles

import (
	"slices"
	"testing"
)

// Each tool names its scopes its own way; asking by another tool's names would be refused or ask too much.
func TestEachToolsScopesAreReadItsOwnWay(t *testing.T) {
	exciter, armature, jira := For("exciter"), For("armature"), For("jira")
	if got := exciter.Requested([]string{"mcp", "mcp:read", "evidence"}); !slices.Equal(got, []string{"mcp", "mcp:read"}) {
		t.Errorf("Exciter requests %v", got)
	}
	if got := armature.Requested([]string{"read", "issue.write", "webhooks.partner"}); !slices.Equal(got, []string{"read", "issue.write"}) {
		t.Errorf("Armature requests %v", got)
	}
	if !exciter.Covers([]string{"mcp"}, []string{"mcp:read"}) || armature.Covers([]string{"issue.write"}, []string{"issue.write:read"}) {
		t.Error("only Exciter reads a consent to mcp as one to mcp:read")
	}
	if got := armature.Uncovered([]string{"read"}, []string{"read", "comment.write"}); !slices.Equal(got, []string{"comment.write"}) {
		t.Errorf("Armature's uncovered = %v", got)
	}
	if got := armature.WithImplied([]string{"issue.write"}); !slices.Equal(got, []string{"issue.write", "read"}) {
		t.Errorf("Armature asks %v, want read beside", got)
	}
	if got := exciter.WithImplied([]string{"mcp"}); !slices.Equal(got, []string{"mcp"}) {
		t.Errorf("Exciter asks %v", got)
	}
	if jira.Product != "jira" || !slices.Equal(jira.ReadScopes, Generic.ReadScopes) || jira.SharedGrantToken {
		t.Errorf("an unknown tool = %+v, want the generic profile", jira)
	}
	if !armature.SharedGrantToken || !armature.PendingRegistration || exciter.SharedGrantToken {
		t.Error("only Armature hands out one token per grant and holds new clients pending")
	}
}
