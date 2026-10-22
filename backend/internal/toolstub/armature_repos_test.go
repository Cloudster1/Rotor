package toolstub

import (
	"net/http"
	"strings"
	"testing"
)

// repoTenant makes an Armature tenant whose project CP connects acme/portal, seen by erin, and a repository
// without a token; erin may read and edit CP-12.
func repoTenant(c *contract, name string, oauth bool) {
	c.t.Helper()
	c.controlled("POST", "/control/tenants", map[string]any{"name": name, "oauth": oauth, "product": "armature",
		"issues": map[string]any{"CP-12": map[string]any{"summary": "Sign in with a passkey", "readers": []string{"erin"}}},
		"repositories": []map[string]any{
			{"project": "CP", "name": "acme/portal", "readers": []string{"erin"}},
			{"project": "CP", "name": "acme/tokenless", "readers": []string{"erin"}, "noToken": true},
		}}, http.StatusCreated)
}

// The repository tools answer in the shapes of Armature's document: a branch for the issue, then a pull request
// from it, both on the issue's development (#137, Cloudster1/Armature#332).
func TestArmatureRepositoryToolsFitArmaturesDocument(t *testing.T) {
	c := newContract(t)
	const tenant = "repos"
	repoTenant(c, tenant, false)
	erin := PATPrefix + "erin"
	answer := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result := c.tool(tenant, erin, name, args)
		if result["isError"] == true {
			t.Fatalf("%s %v refused: %v", name, args, result)
		}
		structured, _ := result["structuredContent"].(map[string]any)
		fits(t, armatureAnswer(t, name), name, structured)
		return structured
	}
	listed := answer(ListRepositoriesTool, map[string]any{"projectKey": "CP"})["repositories"].([]any)
	if len(listed) != 2 || listed[0].(map[string]any)["name"] != "acme/portal" || listed[0].(map[string]any)["defaultBranch"] != "main" {
		t.Errorf("list_repositories = %v", listed)
	}
	issue := map[string]any{"issueKey": "CP-12"}
	if got := answer(GetIssueDevelopmentTool, issue); got["branchName"] != "CP-12-sign-in-with-a-passkey" || len(got["branches"].([]any)) != 0 {
		t.Errorf("get_issue_development before = %v", got)
	}
	branch := answer(CreateIssueBranchTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal"})["branch"].(map[string]any)
	if branch["name"] != "CP-12-sign-in-with-a-passkey" || branch["headSha"] == "" {
		t.Errorf("create_issue_branch = %v", branch)
	}
	pr := answer(OpenPullRequestTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal", "branch": branch["name"],
		"title": "Sign in with a passkey", "draft": true})["pullRequest"].(map[string]any)
	if pr["number"] != float64(1) || pr["sourceBranch"] != branch["name"] || pr["targetBranch"] != "main" || pr["state"] != "open" {
		t.Errorf("open_pull_request = %v", pr)
	}
	if got := answer(GetIssueDevelopmentTool, issue); len(got["branches"].([]any)) != 1 || len(got["pullRequests"].([]any)) != 1 {
		t.Errorf("get_issue_development after = %v", got)
	}
	written := c.controlled("GET", "/control/tenants/"+tenant+"/records", nil, http.StatusOK)
	portal := written["repositories"].(map[string]any)["CP/acme/portal"].(map[string]any)
	if portal["issueBranches"].(map[string]any)[branch["name"].(string)] != "CP-12" || len(portal["pullRequests"].([]any)) != 1 {
		t.Errorf("the records = %v", portal)
	}

	for _, refused := range []struct {
		name string
		args map[string]any
		says string
	}{
		{ListRepositoriesTool, map[string]any{"projectKey": "XX"}, projectNotFound},
		{CreateIssueBranchTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal", "name": "main"}, defaultBranchRefused},
		{CreateIssueBranchTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal", "name": "a b"}, badBranchName},
		{CreateIssueBranchTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal"}, branchExists},
		{CreateIssueBranchTool, map[string]any{"issueKey": "CP-12", "repository": "acme/other"}, repositoryNotFound},
		{CreateIssueBranchTool, map[string]any{"issueKey": "CP-12", "repository": "acme/tokenless"}, noToken},
		{OpenPullRequestTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal", "branch": "main", "title": "t"}, defaultBranchRefused},
		{OpenPullRequestTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal", "branch": "elsewhere", "title": "t"}, notIssueBranch},
		{OpenPullRequestTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal", "branch": branch["name"], "title": "t"}, pullRequestExists},
		{OpenPullRequestTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal", "branch": branch["name"], "title": " "}, pullRequestTitle},
	} {
		if got := c.tool(tenant, erin, refused.name, refused.args); got["isError"] != true || !strings.Contains(stringsOf(got["content"]), refused.says) {
			t.Errorf("%s %v = %v, want %q", refused.name, refused.args, got, refused.says)
		}
	}
	if got := c.tool(tenant, PATPrefix+"frank", ListRepositoriesTool, map[string]any{"projectKey": "CP"}); got["isError"] != true {
		t.Errorf("someone outside the project lists %v", got)
	}
}

// A branch and a pull request are writes of issue.write, which a connected app reading alone is refused.
func TestArmatureRepositoryWritesNeedIssueWrite(t *testing.T) {
	c := newContract(t)
	const tenant = "armature--repo-scopes"
	repoTenant(c, tenant, true)
	grant := c.armatureGrant(tenant, "erin", "read issue.write git.write")
	args := map[string]any{"issueKey": "CP-12", "repository": "acme/portal"}
	if got := c.tool(tenant, grant("read"), CreateIssueBranchTool, args); got["isError"] != true ||
		!strings.Contains(stringsOf(got["content"]), "allowed to read only") {
		t.Errorf("a reading token made a branch: %v", got)
	}
	if got := c.tool(tenant, grant("read issue.write"), CreateIssueBranchTool, args); got["isError"] == true {
		t.Errorf("a writing token was refused a branch: %v", got)
	}
}
