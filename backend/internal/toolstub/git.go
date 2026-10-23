package toolstub

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// The stand-in for Armature's git proxy (Cloudster1/Armature#333): repositories in memory, cloned and pushed as
// the token's person, with the proxy's checks and sentences from Armature's docs/integration.md.

// CapabilityGitProxy is what an Armature tenant lists in /meta for its git proxy.
const CapabilityGitProxy = "git-proxy@1"

// ArmatureGitWrite is the scope a connected app pushes with.
const ArmatureGitWrite = "git.write"

const (
	gitRoute           = apiPrefix + "/git/{projectKey}/{owner}/{name}"
	serviceUploadPack  = "git-upload-pack"
	serviceReceivePack = "git-receive-pack"
	defaultBranch      = "main"
	// fixtureTime is when the fixture commit was made, so every repository of a name starts at the same commit.
	fixtureTime = "1767225600 +0000"
)

// Repository is a repository an Armature tenant's project connects; readers see the project and may edit its
// issues, so they clone and push.
type Repository struct {
	Project       string   `json:"project"`
	Name          string   `json:"name"`
	DefaultBranch string   `json:"defaultBranch,omitempty"`
	Readers       []string `json:"readers"`
	// NoToken is a repository Armature holds no host token for, which it cannot reach.
	NoToken bool `json:"noToken,omitempty"`
}

// gitRepo is a repository's objects and refs, which branches were made for which issue, and its pull requests.
type gitRepo struct {
	Repository
	id       string
	objects  map[string]gitObject
	refs     map[string]string
	branches map[string]string
	pulls    []map[string]any
	// pushedWith is every token a push that landed carried, for a test to look for it where it must never be.
	pushedWith []string
}

// newGitRepo holds one commit with a README on the default branch.
func newGitRepo(tenant string, r Repository) *gitRepo {
	if r.DefaultBranch == "" {
		r.DefaultBranch = defaultBranch
	}
	g := &gitRepo{Repository: r, id: stubID(tenant, "repository", r.Project, r.Name), objects: map[string]gitObject{},
		refs: map[string]string{}, branches: map[string]string{}}
	blob := g.put(kindBlob, []byte("# "+r.Name+"\n\nA repository of the tool double.\n"))
	tree := g.put(kindTree, append([]byte("100644 README.md\x00"), mustHex(blob)...))
	who := "Armature Stub <stub@armature.test> " + fixtureTime
	g.refs["refs/heads/"+r.DefaultBranch] = g.put(kindCommit, []byte("tree "+tree+"\nauthor "+who+"\ncommitter "+who+
		"\n\nStart the repository\n"))
	return g
}

func (g *gitRepo) put(kind string, data []byte) string {
	id := objectID(kind, data)
	g.objects[id] = gitObject{kind: kind, data: data}
	return id
}

// repository finds a repository of the tenant's options by project and owner/name, made on first use.
func (t *tenant) repository(project, name string) *gitRepo {
	for _, r := range t.opts.Repositories {
		if !strings.EqualFold(r.Project, project) || r.Name != name {
			continue
		}
		key := r.Project + "/" + r.Name
		if t.repos[key] == nil {
			t.repos[key] = newGitRepo(t.name, r)
		}
		return t.repos[key]
	}
	return nil
}

// gitRefuse answers as the proxy does before any git: a plain sentence git prints after "remote:".
func gitRefuse(w http.ResponseWriter, status int, sentence string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="Armature", charset="UTF-8"`)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if sentence != "" {
		_, _ = io.WriteString(w, sentence+"\n")
	}
}

// gitToken is the credential git sent: a bearer, or the password of basic auth with any user name.
func gitToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if bearer, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return bearer
	}
	if basic, ok := strings.CutPrefix(auth, "Basic "); ok {
		decoded, err := base64.StdEncoding.DecodeString(basic)
		if _, password, found := strings.Cut(string(decoded), ":"); err == nil && found {
			return password
		}
	}
	return ""
}

// gitCaller lets a request through to a repository, or answers why not, as Armature's proxy does.
func (s *Stub) gitCaller(c *call, push bool) (*caller, *gitRepo, bool) {
	if !c.t.armature() || !slices.Contains(c.t.capabilities(), CapabilityGitProxy) {
		writeError(c.w, http.StatusNotFound, "not_found", "No such endpoint. This tool offers no git proxy.")
		return nil, nil, false
	}
	token := gitToken(c.r)
	if token == "" {
		gitRefuse(c.w, http.StatusUnauthorized, "")
		return nil, nil, false
	}
	who, _ := s.callerByToken(c, token)
	switch {
	case who == nil:
		gitRefuse(c.w, http.StatusUnauthorized, "That token is not valid any more. Get a new one and try again.")
		return nil, nil, false
	case who.app != "" && !slices.Contains(who.scopes, ArmatureRead):
		gitRefuse(c.w, http.StatusForbidden, who.app+" was not allowed to read. Connect it again.")
		return nil, nil, false
	}
	key := strings.ToUpper(chi.URLParam(c.r, "projectKey"))
	owner, err := url.PathUnescape(chi.URLParam(c.r, "owner"))
	repo := c.t.repository(key, owner+"/"+strings.TrimSuffix(chi.URLParam(c.r, "name"), ".git"))
	switch {
	case err != nil || repo == nil || !slices.Contains(repo.Readers, who.username):
		gitRefuse(c.w, http.StatusNotFound, "That repository was not found in "+key+". Check the project key and the repository's owner/name.")
		return nil, nil, false
	case repo.NoToken:
		gitRefuse(c.w, http.StatusConflict, repo.Name+" has no access token in Armature, so Armature cannot reach it. A project administrator adds one under the project's repositories.")
		return nil, nil, false
	case !push:
		return who, repo, true
	case who.app != "" && !slices.Contains(who.scopes, ArmatureGitWrite):
		gitRefuse(c.w, http.StatusForbidden, who.app+" was not allowed to push to issue branches. Connect it again and allow it to push to issue branches.")
		return nil, nil, false
	case who.app == "" && who.readOnly:
		gitRefuse(c.w, http.StatusForbidden, readOnlyToken)
		return nil, nil, false
	}
	return who, repo, true
}

func (s *Stub) handleGitRefs(c *call) {
	service := c.r.URL.Query().Get("service")
	if service != serviceUploadPack && service != serviceReceivePack {
		gitRefuse(c.w, http.StatusBadRequest, "Armature speaks git's smart protocol only. Use git 1.6.6 or later.")
		return
	}
	if _, repo, ok := s.gitCaller(c, service == serviceReceivePack); ok {
		c.w.Header().Set("Content-Type", "application/x-"+service+"-advertisement")
		c.w.Header().Set("Cache-Control", "no-cache")
		_, _ = c.w.Write(repo.advertise(service))
	}
}

// gitBody is a request's body, unpacked when git compressed it, up to the most a push may carry.
func gitBody(r *http.Request) ([]byte, bool) {
	var in io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		z, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, false
		}
		defer z.Close()
		in = z
	}
	body, err := io.ReadAll(io.LimitReader(in, config.ToolStubMaxPushBytes+1))
	return body, err == nil && len(body) <= config.ToolStubMaxPushBytes
}

func (s *Stub) handleGitUploadPack(c *call) {
	_, repo, ok := s.gitCaller(c, false)
	if !ok {
		return
	}
	body, whole := gitBody(c.r)
	answer, err := repo.uploadPack(bytes.NewReader(body))
	if !whole || err != nil {
		gitRefuse(c.w, http.StatusBadRequest, "The request could not be read. Fetch again with a current git.")
		return
	}
	c.w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	c.w.Header().Set("Cache-Control", "no-cache")
	_, _ = c.w.Write(answer)
}

func (s *Stub) handleGitReceivePack(c *call) {
	who, repo, ok := s.gitCaller(c, true)
	if !ok {
		return
	}
	body, whole := gitBody(c.r)
	push, err := readPush(body)
	if err != nil {
		gitRefuse(c.w, http.StatusBadRequest, "The push could not be read. Push again.")
		return
	}
	reasons, objects := repo.judge(c.t, who, push, whole)
	if !slices.Contains(push.asked, "report-status") {
		for _, u := range push.updates {
			if reasons[u.ref] != "" {
				gitRefuse(c.w, http.StatusForbidden, reasons[u.ref])
				return
			}
		}
	}
	if len(reasons) == 0 {
		for id, o := range objects {
			repo.objects[id] = o
		}
		for _, u := range push.updates {
			repo.refs[u.ref] = u.new
		}
		repo.pushedWith = append(repo.pushedWith, gitToken(c.r))
	}
	c.w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	c.w.Header().Set("Cache-Control", "no-cache")
	_, _ = c.w.Write(report(push, reasons))
}

// judge says why each command may not go to the host, as Armature's proxy does; when one is refused, the whole
// push is. It answers the pack's objects for a push that goes on.
func (g *gitRepo) judge(t *tenant, who *caller, push *pushRequest, whole bool) (map[string]string, map[string]gitObject) {
	reasons := map[string]string{}
	var objects map[string]gitObject
	for _, u := range push.updates {
		if reason := g.refuse(t, who, push, u, whole, &objects); reason != "" {
			reasons[u.ref] = reason
		}
	}
	if len(reasons) > 0 {
		for _, u := range push.updates {
			if reasons[u.ref] == "" {
				reasons[u.ref] = "Another ref in this push was refused, so none of it was pushed."
			}
		}
	}
	return reasons, objects
}

func (g *gitRepo) refuse(t *tenant, who *caller, push *pushRequest, u refUpdate, whole bool, objects *map[string]gitObject) string {
	branch, isBranch := strings.CutPrefix(u.ref, "refs/heads/")
	switch {
	case push.signed:
		return "Armature does not forward signed pushes. Push without --signed."
	case !whole:
		return "This push is larger than " + strconv.Itoa(config.ToolStubMaxPushBytes>>20) + " MiB, the most Armature forwards at once. Push fewer commits at a time."
	case strings.HasPrefix(u.ref, "refs/tags/"):
		return "Tags are not pushed through Armature. Push a branch made for an issue, and tag on the host."
	case !isBranch || branch == "":
		return "Only a branch made for an issue is pushed through Armature."
	case branch == g.DefaultBranch:
		return branch + " is the default branch, which takes work only through a pull request. Push to the issue's branch and open one."
	case u.new == zeroID:
		return "Armature does not delete branches. Delete it on the host, or leave it to its pull request."
	}
	key := g.branches[branch]
	if key == "" {
		return branch + " was not made for an issue. Make the issue's branch with create_issue_branch and push to that."
	}
	if issue, ok := t.opts.Issues[key]; !ok || !slices.Contains(issue.Readers, who.username) {
		return "You may not edit " + key + ", so you may not push to its branch."
	}
	if *objects == nil {
		found, err := readPack(push.pack, g.objects)
		if err != nil {
			return "Armature could not read the commits in this push, so it cannot tell it rewrites nothing. Push again with a current git."
		}
		*objects = found
	}
	if g.refs[u.ref] != u.old || !fastForward(*objects, u.old, u.new) {
		return "This would rewrite " + branch + ", and Armature never force-pushes. Fetch the branch, put your work on top of it and push again."
	}
	return ""
}

// fastForward says whether the pushed commits lead from new back to old, through the commits the push carries.
func fastForward(pushed map[string]gitObject, old, new string) bool {
	queue, seen := []string{new}, map[string]bool{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		o, ok := pushed[id]
		if seen[id] || !ok || o.kind != kindCommit {
			continue
		}
		seen[id] = true
		_, parents := commitLinks(o.data)
		if slices.Contains(parents, old) {
			return true
		}
		queue = append(queue, parents...)
	}
	return false
}

// view is the repository as the control API answers it, for a test to see what a run pushed and opened.
func (g *gitRepo) view() map[string]any {
	pulls := g.pulls
	if pulls == nil {
		pulls = []map[string]any{}
	}
	pushed := g.pushedWith
	if pushed == nil {
		pushed = []string{}
	}
	return map[string]any{"refs": g.refs, "issueBranches": g.branches, "pullRequests": pulls, "pushedWith": pushed}
}
