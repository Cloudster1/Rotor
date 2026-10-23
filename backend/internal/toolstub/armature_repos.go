package toolstub

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Armature's tools for a project's repositories (Cloudster1/Armature#332), answered from the tenant's
// repositories in the shapes of Armature's document, with its refusals in its words.

// The repository tools the stub answers.
const (
	ListRepositoriesTool    = "list_repositories"
	GetIssueDevelopmentTool = "get_issue_development"
	CreateIssueBranchTool   = "create_issue_branch"
	OpenPullRequestTool     = "open_pull_request"
)

// Armature's words for what it refuses a branch or a pull request, from its httpapi errors.
const (
	projectNotFound      = "That project was not found."
	repositoryNotFound   = "That repository was not found."
	noToken              = "This repository has no access token, so Armature cannot write to it. A project administrator gives it one on the project's Repositories page."
	defaultBranchRefused = "That is the repository's default branch, which Armature never writes to. Work on the issue's own branch, and merge it through a pull request."
	notIssueBranch       = "That branch was not made for this issue. Make the issue's branch with create_issue_branch, push to it, and open the pull request from it."
	branchExists         = "The host already has a branch of that name. Pick another, or push to the one that is there."
	badBranchName        = "That is not a name git accepts for a branch. Use letters, digits, dots, dashes and slashes, with no spaces."
	pullRequestExists    = "The host already has an open pull request from that branch. Push to the branch to update it, or find it in the issue's development."
	pullRequestTitle     = "A pull request needs a title. Say in a few words what it changes."
	// maxSlug is how much of an issue's summary its branch name keeps, as Armature's BranchName.
	maxSlug          = 40
	maxBranchName    = 200
	stubGitHost      = "gitea"
	stubGitWebOrigin = "https://gitea.suite.test/"
)

// callRepositories answers the repository tools; ok is false for any other tool.
func (s *Stub) callRepositories(c *call, who *caller, name string, args map[string]json.RawMessage) (any, bool) {
	switch name {
	case ListRepositoriesTool:
		return s.listRepositories(c, who, stringArg(args, "projectKey")), true
	case GetIssueDevelopmentTool, CreateIssueBranchTool, OpenPullRequestTool:
	default:
		return nil, false
	}
	key := stringArg(args, "issueKey")
	issue, ok := c.t.opts.Issues[key]
	if !ok || !slices.Contains(issue.Readers, who.username) {
		return refusal(issueNotFound), true
	}
	if name == GetIssueDevelopmentTool {
		return answered(s.development(c.t, key, issue)), true
	}
	project, _, _ := strings.Cut(key, "-")
	repo := c.t.repositoryOf(project, stringArg(args, "repository"), stringArg(args, "repositoryId"), who)
	switch {
	case repo == nil:
		return refusal(repositoryNotFound), true
	case repo.NoToken:
		return refusal(noToken), true
	case name == CreateIssueBranchTool:
		return s.createBranch(c.t, repo, key, issue, args), true
	}
	return s.openPullRequest(c, who, repo, key, args), true
}

// repositoryOf is a repository of the project the person sees, named by owner/name or by id.
func (t *tenant) repositoryOf(project, name, id string, who *caller) *gitRepo {
	for _, r := range t.opts.Repositories {
		if !strings.EqualFold(r.Project, project) || !slices.Contains(r.Readers, who.username) {
			continue
		}
		if g := t.repository(r.Project, r.Name); (name != "" && r.Name == name) || (id != "" && g.id == id) {
			return g
		}
	}
	return nil
}

func (s *Stub) listRepositories(c *call, who *caller, project string) any {
	list := []map[string]any{}
	for _, r := range c.t.opts.Repositories {
		if strings.EqualFold(r.Project, project) && slices.Contains(r.Readers, who.username) {
			list = append(list, c.t.repository(r.Project, r.Name).answer())
		}
	}
	if len(list) == 0 {
		return refusal(projectNotFound)
	}
	return answered(map[string]any{"repositories": list})
}

// answer is the repository in Armature's Repository shape, without its webhook secret, as the tool lists it.
func (g *gitRepo) answer() map[string]any {
	at := stubTime.Format(time.RFC3339)
	open := 0
	for _, pr := range g.pulls {
		if pr["state"] == "open" {
			open++
		}
	}
	return map[string]any{"id": g.id, "projectId": stubID("project", g.Project), "projectKey": g.Project, "host": stubGitHost,
		"name": g.Name, "url": stubGitWebOrigin + g.Name, "apiBaseUrl": stubGitWebOrigin + "api/v1", "defaultBranch": g.DefaultBranch,
		"hasToken": !g.NoToken, "commitCount": 0, "openPullRequestCount": open, "createdAt": at, "updatedAt": at}
}

// branch is a branch made for an issue, in Armature's Branch shape.
func (g *gitRepo) branch(tenant, name string) map[string]any {
	return map[string]any{"id": stubID(tenant, "branch", g.Name, name), "repository": g.Name, "name": name,
		"url": stubGitWebOrigin + g.Name + "/src/branch/" + name, "headSha": g.refs["refs/heads/"+name], "commitCount": 0,
		"createdAt": stubTime.Format(time.RFC3339)}
}

func (s *Stub) development(t *tenant, key string, issue Issue) map[string]any {
	branches, pulls := []map[string]any{}, []map[string]any{}
	for _, r := range t.opts.Repositories {
		g := t.repos[r.Project+"/"+r.Name]
		if g == nil {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(g.branches)) {
			if g.branches[name] == key {
				branches = append(branches, g.branch(t.name, name))
			}
		}
		for _, pr := range g.pulls {
			if pr["issueKey"] == key {
				pulls = append(pulls, pullAnswer(pr))
			}
		}
	}
	return map[string]any{"branchName": BranchName(key, issue.Summary), "branches": branches, "pullRequests": pulls,
		"commits": []any{}, "runs": []any{}}
}

// createBranch makes the issue's branch off from, or the default branch, as Armature's CreateBranch does.
func (s *Stub) createBranch(t *tenant, g *gitRepo, key string, issue Issue, args map[string]json.RawMessage) any {
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		name = BranchName(key, issue.Summary)
	}
	from := strings.TrimSpace(stringArg(args, "from"))
	if from == "" {
		from = g.DefaultBranch
	}
	head, ok := g.refs["refs/heads/"+from]
	switch {
	case !validBranchName(name):
		return refusal(badBranchName)
	case name == g.DefaultBranch:
		return refusal(defaultBranchRefused)
	case g.refs["refs/heads/"+name] != "":
		return refusal(branchExists)
	case !ok:
		return refusal("The host refused: it has no branch " + from + " to start from. Leave from out to start from the default branch.")
	}
	g.refs["refs/heads/"+name], g.branches[name] = head, key
	return answered(map[string]any{"branch": g.branch(t.name, name)})
}

func (s *Stub) openPullRequest(c *call, who *caller, g *gitRepo, key string, args map[string]json.RawMessage) any {
	branch, title := strings.TrimSpace(stringArg(args, "branch")), strings.TrimSpace(stringArg(args, "title"))
	var draft bool
	_ = json.Unmarshal(args["draft"], &draft)
	switch {
	case title == "":
		return refusal(pullRequestTitle)
	case branch == g.DefaultBranch:
		return refusal(defaultBranchRefused)
	case g.branches[branch] != key:
		return refusal(notIssueBranch)
	}
	for _, pr := range g.pulls {
		if pr["sourceBranch"] == branch && pr["state"] == "open" {
			return refusal(pullRequestExists)
		}
	}
	number := len(g.pulls) + 1
	pr := map[string]any{"id": stubID(c.t.name, "pull-request", g.Name, branch), "repository": g.Name, "number": number,
		"title": title, "state": "open", "sourceBranch": branch, "targetBranch": g.DefaultBranch,
		"url": stubGitWebOrigin + g.Name + "/pulls/" + strconv.Itoa(number), "headSha": g.refs["refs/heads/"+branch],
		"authorName": who.username, "openedAt": s.stubNow(),
		// Kept for the control API, never answered: Armature's PullRequest has no such fields.
		"issueKey": key, "body": stringArg(args, "body"), "draft": draft}
	g.pulls = append(g.pulls, pr)
	return answered(map[string]any{"pullRequest": pullAnswer(pr)})
}

// pullAnswer is a pull request as Armature answers it, without what only the control API shows.
func pullAnswer(pr map[string]any) map[string]any {
	out := maps.Clone(pr)
	delete(out, "issueKey")
	delete(out, "body")
	delete(out, "draft")
	return out
}

// BranchName is the branch Armature suggests for an issue: its key and a slug of its summary. Adapted from
// Armature's backend/internal/git/keys.go (see NOTICE).
func BranchName(issueKey, summary string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(summary) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > maxSlug {
		cut := strings.LastIndex(slug[:maxSlug+1], "-")
		if cut < maxSlug/2 {
			cut = maxSlug
		}
		slug = strings.TrimRight(slug[:cut], "-")
	}
	if slug == "" {
		return issueKey
	}
	return issueKey + "-" + slug
}

// validBranchName says whether git takes a name as a branch, as Armature's ValidBranchName.
func validBranchName(name string) bool {
	if name == "" || len(name) > maxBranchName || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") ||
		strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") ||
		strings.Contains(name, "..") || strings.Contains(name, "//") || strings.Contains(name, "@{") || name == "@" {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\", r) {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}
