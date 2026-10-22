package tools

import "testing"

// The clone address is Armature's git proxy path, with an owner's slashes kept in one segment (#137).
func TestTheCloneAddressIsTheToolsGitProxy(t *testing.T) {
	armature := Tool{Name: "armature", BaseURL: "https://armature.example.com/", Capabilities: []string{"mcp@1", CapabilityGitProxy}}
	for name, want := range map[string]string{
		"acme/portal":         "https://armature.example.com/api/v1/git/CP/acme/portal.git",
		"acme/web/team/front": "https://armature.example.com/api/v1/git/CP/acme%2Fweb%2Fteam/front.git",
	} {
		if got := armature.CloneURL("CP", name); got != want {
			t.Errorf("CloneURL(CP, %s) = %s, want %s", name, got, want)
		}
	}
	if !armature.OffersGit() {
		t.Error("an Armature listing git-proxy@1 offers no git")
	}
	for _, tool := range []Tool{
		{Name: "armature", Capabilities: []string{"mcp@1"}},
		{Name: "armature"},
		{Name: "exciter", Capabilities: []string{"mcp@1", CapabilityGitProxy}},
	} {
		if tool.OffersGit() {
			t.Errorf("%s with %v offers git", tool.Name, tool.Capabilities)
		}
	}
}
