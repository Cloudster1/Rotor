package toolstub

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitClient runs the real git client, which the Go image carries, against the stand-in; nothing of the person's
// own configuration or credentials takes part.
type gitClient struct {
	t    *testing.T
	home string
}

func newGitClient(t *testing.T) *gitClient {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("the git proxy's stand-in is held to the real git client, and this image has none: %v", err)
	}
	return &gitClient{t: t, home: t.TempDir()}
}

// run runs git in dir and answers its output and whether it succeeded.
func (g *gitClient) run(dir string, args ...string) (string, bool) {
	g.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + g.home, "PATH=" + os.Getenv("PATH"), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Erin", "GIT_AUTHOR_EMAIL=erin@suite.test", "GIT_COMMITTER_NAME=Erin", "GIT_COMMITTER_EMAIL=erin@suite.test"}
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// bearer is git's option to send a token as a bearer, as Rotor's runner will.
func bearer(token string) []string {
	return []string{"-c", "http.extraHeader=Authorization: Bearer " + token}
}

// commit adds a file to a checkout and commits it.
func (g *gitClient) commit(dir, file, text string) {
	g.t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o600); err != nil {
		g.t.Fatal(err)
	}
	if out, ok := g.run(dir, "add", file); !ok {
		g.t.Fatal(out)
	}
	if out, ok := g.run(dir, "commit", "-q", "-m", "Change "+file); !ok {
		g.t.Fatal(out)
	}
}

// The real git client clones through the stand-in by bearer and by password, whole and at depth 1, and pushes the
// issue's branch, which create_issue_branch made; everything else the proxy refuses, it refuses (#137).
func TestTheGitProxyStandInClonesAndPushesAsArmatureDoes(t *testing.T) {
	c := newContract(t)
	const tenant = "git"
	repoTenant(c, tenant, false)
	erin := PATPrefix + "erin"
	git := newGitClient(t)
	clone := c.base + "/t/" + tenant + "/api/v1/git/CP/acme/portal.git"
	work := t.TempDir()

	if out, ok := git.run(work, "clone", "-q", clone, "anonymous"); ok || !strings.Contains(out, "Authentication failed") && !strings.Contains(out, "could not read Username") {
		t.Errorf("a clone without a token = %v %s", ok, out)
	}
	byPassword := strings.Replace(clone, "://", "://rotor:"+erin+"@", 1)
	if out, ok := git.run(work, "clone", "-q", byPassword, "password"); !ok {
		t.Fatalf("a clone by password failed: %s", out)
	}
	if out, ok := git.run(work, append(bearer(erin), "clone", "-q", "--depth", "1", clone, "shallow")...); !ok {
		t.Fatalf("a shallow clone by bearer failed: %s", out)
	}
	if out, _ := git.run(filepath.Join(work, "shallow"), "rev-parse", "--is-shallow-repository"); strings.TrimSpace(out) != "true" {
		t.Errorf("the depth 1 clone is not shallow: %s", out)
	}
	if out, ok := git.run(work, append(bearer(PATPrefix+"frank"), "clone", "-q", clone, "outsider")...); ok || !strings.Contains(out, "not found in CP") {
		t.Errorf("an outsider's clone = %v %s", ok, out)
	}

	branch := c.tool(tenant, erin, CreateIssueBranchTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal"})
	name := branch["structuredContent"].(map[string]any)["branch"].(map[string]any)["name"].(string)
	dir := filepath.Join(work, "work")
	if out, ok := git.run(work, append(bearer(erin), "clone", "-q", clone, "work")...); !ok {
		t.Fatal(out)
	}
	git.run(dir, "checkout", "-q", name)
	// A file large enough that its next version goes as a delta against what the stand-in already holds.
	lines := strings.Repeat("// A line of the passkey package, long enough to be worth a delta.\n", 400)
	for i, text := range []string{lines, lines + "// one more\n"} {
		git.commit(dir, "passkey.go", "package passkey\n"+text)
		if out, ok := git.run(dir, append(bearer(erin), "push", "-q", "origin", name)...); !ok {
			t.Fatalf("push %d to the issue's branch failed: %s", i+1, out)
		}
	}
	head, _ := git.run(dir, "rev-parse", "HEAD")
	refs := c.controlled("GET", "/control/tenants/"+tenant+"/records", nil, http.StatusOK)["repositories"].(map[string]any)["CP/acme/portal"].(map[string]any)["refs"].(map[string]any)
	if refs["refs/heads/"+name] != strings.TrimSpace(head) {
		t.Errorf("the branch is at %v after the push, want %s", refs["refs/heads/"+name], head)
	}
	shallow := filepath.Join(work, "shallow")
	git.run(shallow, append(bearer(erin), "fetch", "-q", "--depth", "1", "origin", name)...)
	if out, ok := git.run(shallow, "cat-file", "-e", strings.TrimSpace(head)); !ok {
		t.Errorf("a shallow fetch does not bring the pushed commit: %s", out)
	}

	git.run(dir, "checkout", "-q", "-b", "elsewhere")
	git.commit(dir, "other.go", "package other\n")
	git.run(dir, "tag", "v1")
	git.run(dir, "checkout", "-q", name)
	git.run(dir, "reset", "-q", "--hard", "HEAD~1")
	git.commit(dir, "rewrite.go", "package rewrite\n")
	for _, refused := range []struct {
		args []string
		says string
	}{
		{[]string{"push", "origin", "HEAD:main"}, "main is the default branch"},
		{[]string{"push", "origin", "elsewhere"}, "elsewhere was not made for an issue"},
		{[]string{"push", "origin", "v1"}, "Tags are not pushed through Armature"},
		{[]string{"push", "origin", ":" + name}, "Armature does not delete branches"},
		{[]string{"push", "--force", "origin", name}, "Armature never force-pushes"},
	} {
		if out, ok := git.run(dir, append(bearer(erin), refused.args...)...); ok || !strings.Contains(out, refused.says) {
			t.Errorf("git %v = %v %s, want %q", refused.args, ok, out, refused.says)
		}
	}
}

// A connected app clones with read and pushes only with git.write, which Rotor asks for a repository binding that
// writes; a reading run's token cannot push.
func TestTheGitProxyStandInPushesOnlyWithGitWrite(t *testing.T) {
	c := newContract(t)
	const tenant = "armature--git-scopes"
	repoTenant(c, tenant, true)
	grant := c.armatureGrant(tenant, "erin", "read issue.write git.write")
	reading, writing := grant("read"), grant("read issue.write git.write")
	created := c.tool(tenant, writing, CreateIssueBranchTool, map[string]any{"issueKey": "CP-12", "repository": "acme/portal"})
	name := created["structuredContent"].(map[string]any)["branch"].(map[string]any)["name"].(string)
	git := newGitClient(t)
	clone := c.base + "/t/" + tenant + "/api/v1/git/CP/acme/portal.git"
	work := t.TempDir()
	if out, ok := git.run(work, append(bearer(reading), "clone", "-q", clone, "work")...); !ok {
		t.Fatalf("a reading token cannot clone: %s", out)
	}
	dir := filepath.Join(work, "work")
	git.run(dir, "checkout", "-q", name)
	git.commit(dir, "passkey.go", "package passkey\n")
	if out, ok := git.run(dir, append(bearer(reading), "push", "origin", name)...); ok || !strings.Contains(out, "not allowed to push to issue branches") {
		t.Errorf("a reading token's push = %v %s", ok, out)
	}
	if out, ok := git.run(dir, append(bearer(writing), "push", "origin", name)...); !ok {
		t.Errorf("a token with git.write cannot push: %s", out)
	}
}
