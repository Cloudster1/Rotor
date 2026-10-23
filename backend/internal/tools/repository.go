package tools

import (
	"net/url"
	"slices"
	"strings"
)

// CapabilityGitProxy is a tool that clones and pushes its projects' repositories as the person whose token it
// is given, so no host token reaches Rotor (#137, Armature's docs/integration.md).
const CapabilityGitProxy = "git-proxy@1"

// gitPath is where the git proxy serves a project's repository under the tool's address.
const gitPath = "/api/v1/git/"

// OffersGit says whether repository bindings may clone through the tool: its profile knows the scope a push takes
// and the tool listed its git proxy when last asked.
func (t Tool) OffersGit() bool {
	return t.Profile().GitWrite != "" && slices.Contains(t.Capabilities, CapabilityGitProxy)
}

// CloneURL is the git proxy's address of a project's repository named owner/name, where an owner with slashes,
// such as a GitLab subgroup, is one segment with its slashes escaped.
func (t Tool) CloneURL(project, name string) string {
	owner, repo := "", name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		owner, repo = name[:i], name[i+1:]
	}
	return strings.TrimSuffix(t.BaseURL, "/") + gitPath + url.PathEscape(project) + "/" + url.PathEscape(owner) + "/" +
		url.PathEscape(repo) + ".git"
}
