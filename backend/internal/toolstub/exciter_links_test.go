package toolstub

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// A feature's requirements sit under a section, its test cases under another, and a test case whose requirement
// changed since it was linked reads back as suspect, all in Exciter's shapes (#140).
func TestPlacesAndSuspectLinksAnswerInTheShapesExciterLists(t *testing.T) {
	c := newContract(t)
	const tenant = "places"
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "oauth": false}, http.StatusCreated)
	erin := PATPrefix + "erin"
	create := func(args map[string]any) map[string]any {
		return c.answers(tenant, erin, CreateRequirementTool, args)["requirement"].(map[string]any)
	}
	feature := create(map[string]any{"projectKey": "RTR", "title": "Checkout", "kind": "section"})
	under := map[string]any{"parentId": feature["id"], "afterId": nil}
	pay := create(map[string]any{"projectKey": "RTR", "title": "Pay by card", "place": under})
	tc := create(map[string]any{"projectKey": "RTR", "title": "Pays by card", "typeId": typeID("RTR", roleTestCase),
		"testCase": map[string]any{"preconditions": nil, "parameters": []any{}, "steps": []any{}}})
	if pay["parentId"] != feature["id"] {
		t.Errorf("the requirement placed under the section answers parentId %v", pay["parentId"])
	}
	nodes := c.answers(tenant, erin, GetTreeTool, map[string]any{"projectKey": "RTR"})["nodes"].([]any)
	keys := []any{}
	for _, n := range nodes {
		keys = append(keys, n.(map[string]any)["key"])
	}
	if len(nodes) != 3 || keys[1] != "RTR-2" || nodes[1].(map[string]any)["depth"] != float64(1) || nodes[0].(map[string]any)["children"] != float64(1) {
		t.Errorf("RTR's tree = %v, want RTR-2 under RTR-1", nodes)
	}

	c.answers(tenant, erin, LinkRequirementsTool, map[string]any{"reqKey": tc["key"], "otherKey": "RTR-2", "typeKey": "verifies"})
	verified := func() map[string]any {
		links := c.answers(tenant, erin, ListLinksTool, map[string]any{"reqKey": "RTR-2"})["links"].([]any)
		if len(links) != 1 {
			t.Fatalf("RTR-2's links = %v", links)
		}
		return links[0].(map[string]any)
	}
	if l := verified(); l["direction"] != "incoming" || l["phrase"] != "is verified by" || len(l["suspect"].([]any)) != 0 ||
		l["other"].(map[string]any)["key"] != "RTR-3" {
		t.Errorf("the fresh link reads %v", l)
	}
	c.answers(tenant, erin, UpdateRequirementTool, map[string]any{"reqKey": "RTR-2", "baseRevision": 1, "statementMarkdown": "By card or wallet."})
	if suspect := verified()["suspect"].([]any); len(suspect) != 1 || suspect[0].(map[string]any)["end"] != "target" ||
		suspect[0].(map[string]any)["current"] != float64(2) {
		t.Errorf("after RTR-2 changed its link is suspect as %v", suspect)
	}
	c.refuses(tenant, erin, map[string][]map[string]any{
		CreateRequirementTool: {{"projectKey": "CP", "title": "Elsewhere", "place": under}},
		ListLinksTool:         {{"reqKey": "RTR-9"}},
	})
}

// A repository's fixture files are on its default branch from the first commit, which the real git client clones.
func TestARepositoryStartsWithItsFixtureFiles(t *testing.T) {
	c := newContract(t)
	const tenant = "fixture"
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "oauth": false, "product": "armature",
		"repositories": []map[string]any{{"project": "CP", "name": "acme/portal", "readers": []string{"erin"},
			"files": map[string]string{"sum.mjs": "export const sum = (a, b) => a + b;\n", "a.txt": "first\n"}}}}, http.StatusCreated)
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant + "-bad", "product": "armature",
		"repositories": []map[string]any{{"project": "CP", "name": "acme/portal", "files": map[string]string{"src/x": ""}}}},
		http.StatusUnprocessableEntity)
	git := newGitClient(t)
	work := t.TempDir()
	clone := c.base + "/t/" + tenant + "/api/v1/git/CP/acme/portal.git"
	if out, ok := git.run(work, append(bearer(PATPrefix+"erin"), "clone", "-q", clone, "portal")...); !ok {
		t.Fatalf("cloning the fixture failed: %s", out)
	}
	for name, want := range map[string]string{"sum.mjs": "export const sum = (a, b) => a + b;\n", "a.txt": "first\n"} {
		if got, err := os.ReadFile(filepath.Join(work, "portal", name)); err != nil || string(got) != want {
			t.Errorf("the clone's %s = %q (%v), want %q", name, got, err, want)
		}
	}
	if out, ok := git.run(filepath.Join(work, "portal"), "fsck", "--strict"); !ok {
		t.Errorf("git finds the fixture's objects broken: %s", out)
	}
}
