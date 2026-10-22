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

// A repository binding that writes pushes, so its token and the person's consent take git.write; one that reads
// never does, and a tool without git keeps the binding's scopes as written (#137).
func TestARepositoryBindingTakesGitWriteOnlyToWrite(t *testing.T) {
	armature, exciter := For("armature"), For("exciter")
	if got := armature.Requested([]string{"read", "issue.write", "git.write", "webhooks.partner"}); !slices.Contains(got, "git.write") {
		t.Errorf("Armature's consent asks %v, without git.write", got)
	}
	for _, c := range []struct {
		scopes            []string
		write, repository bool
		want              []string
	}{
		{[]string{"read", "issue.write"}, true, true, []string{"read", "issue.write", "git.write"}},
		{[]string{"read", "git.write", "issue.write"}, true, true, []string{"read", "issue.write", "git.write"}},
		{[]string{"read", "git.write"}, false, true, []string{"read"}},
		{[]string{"read", "issue.write"}, true, false, []string{"read", "issue.write"}},
	} {
		if got := armature.BindingScopes(c.scopes, c.write, c.repository); !slices.Equal(got, c.want) {
			t.Errorf("Armature's %v (write %v, repository %v) asks %v, want %v", c.scopes, c.write, c.repository, got, c.want)
		}
	}
	if got := exciter.BindingScopes([]string{"mcp"}, true, true); !slices.Equal(got, []string{"mcp"}) {
		t.Errorf("Exciter, without git, asks %v", got)
	}
	if !armature.Covers([]string{"read", "issue.write"}, []string{"read"}) || armature.Covers([]string{"read", "issue.write"},
		armature.BindingScopes([]string{"read"}, true, true)) {
		t.Error("a consent without git.write covers a writing repository binding")
	}
}
