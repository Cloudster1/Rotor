package toolstub

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// list_project_issues answers in the shape of Armature's document: the project's issues the caller reads, narrowed
// by the NQL a digest sends, ordered and paged as Armature does, and NQL the stub does not read refused (#271).
func TestArmatureListsAProjectsIssuesByNQL(t *testing.T) {
	c := newContract(t)
	const tenant = "armature--digest"
	now := time.Now().UTC()
	ago := func(days int) string { return now.Add(-time.Duration(days) * day).Format(time.RFC3339) }
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "product": "armature", "oauth": false,
		"issues": map[string]any{
			"CP-1":  map[string]any{"summary": "Old and open", "readers": []string{"erin"}, "createdAt": ago(30)},
			"CP-2":  map[string]any{"summary": "Old, done this week", "readers": []string{"erin"}, "createdAt": ago(30), "category": "done", "resolvedAt": ago(2)},
			"CP-9":  map[string]any{"summary": "New", "readers": []string{"erin"}, "createdAt": ago(1), "category": "in_progress"},
			"CP-10": map[string]any{"summary": "New and done", "readers": []string{"erin"}, "createdAt": ago(3), "category": "done", "resolvedAt": ago(1)},
			"CP-11": map[string]any{"summary": "Hidden", "readers": []string{"frank"}, "createdAt": ago(1)},
			"QA-1":  map[string]any{"summary": "Elsewhere", "readers": []string{"erin"}, "createdAt": ago(1)},
		}}, http.StatusCreated)
	erin := PATPrefix + "erin"
	list := func(args map[string]any) map[string]any {
		t.Helper()
		result := c.tool(tenant, erin, ListProjectIssuesTool, args)
		if result["isError"] == true {
			t.Fatalf("list_project_issues %v refused: %v", args, result)
		}
		structured, _ := result["structuredContent"].(map[string]any)
		fits(t, armatureAnswer(t, ListProjectIssuesTool), ListProjectIssuesTool, structured)
		return structured
	}
	keys := func(page map[string]any) string {
		var out []string
		for _, is := range page["issues"].([]any) {
			out = append(out, is.(map[string]any)["key"].(string))
		}
		return strings.Join(out, " ")
	}

	if got := list(map[string]any{"projectKey": "CP"}); keys(got) != "CP-1 CP-2 CP-9 CP-10" || got["total"] != float64(4) {
		t.Errorf("the project's issues = %v, want those erin reads in CP, by number", got)
	}
	if got := keys(list(map[string]any{"projectKey": "CP", "q": "created >= -7d ORDER BY created ASC"})); got != "CP-10 CP-9" {
		t.Errorf("created in the last 7 days = %q, want CP-10 then CP-9", got)
	}
	done := list(map[string]any{"projectKey": "CP", "q": "statusCategory = done AND resolved >= -7d ORDER BY resolved DESC"})
	if keys(done) != "CP-10 CP-2" {
		t.Errorf("done in the last 7 days = %q, want the latest first", keys(done))
	}
	if first := done["issues"].([]any)[0].(map[string]any); first["status"].(map[string]any)["name"] != "Done" || first["resolvedAt"] != ago(1) {
		t.Errorf("a done issue reads %v", first)
	}
	if got := keys(list(map[string]any{"projectKey": "CP", "q": "statusCategory != done"})); got != "CP-1 CP-9" {
		t.Errorf("not done = %q", got)
	}
	if got := list(map[string]any{"projectKey": "CP-9", "limit": 2, "offset": 2}); keys(got) != "CP-9 CP-10" || got["total"] != float64(4) {
		t.Errorf("the second page of two, by an issue key's project = %v", got)
	}

	for _, q := range []string{"assignee = currentUser()", "created >= startOfWeek()", "created >= -7d OR resolved >= -7d", "ORDER BY rank"} {
		if refused := c.tool(tenant, erin, ListProjectIssuesTool, map[string]any{"projectKey": "CP", "q": q}); refused["isError"] != true {
			t.Errorf("the stub answered NQL it does not read, %q: %v", q, refused)
		}
	}
}
