package toolstub

import (
	"net/http"
	"strings"
	"testing"
)

// list_labels and bulk_edit_issues answer in the shapes of Armature's document, labels are the installation's in the
// spelling first given, and adding one that does not exist coins it, as Armature's bulk change does (#270).
func TestArmatureLabelsAreListedAndAddedAsArmatureDoes(t *testing.T) {
	c := newContract(t)
	const tenant = "armature--labels"
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "oauth": false, "product": "armature",
		"labels": []string{"ui", "Backend"},
		"issues": map[string]any{
			"CP-1": map[string]any{"summary": "Export crashes", "readers": []string{"erin"}},
			"CP-2": map[string]any{"summary": "Slow login", "readers": []string{"erin"}, "labels": []string{"bug", "backend"}},
			"CP-3": map[string]any{"summary": "Hidden", "readers": []string{"frank"}},
		}}, http.StatusCreated)
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
	names := func(list any) string {
		var out []string
		for _, l := range list.([]any) {
			out = append(out, l.(map[string]any)["name"].(string))
		}
		return strings.Join(out, " ")
	}
	counts := func(page map[string]any) map[string]any {
		out := map[string]any{}
		for _, l := range page["labels"].([]any) {
			out[l.(map[string]any)["name"].(string)] = l.(map[string]any)["issueCount"]
		}
		return out
	}

	page := answer(ListLabelsTool, map[string]any{})
	if got := names(page["labels"]); got != "Backend bug ui" || page["total"] != float64(3) || page["limit"] != float64(defaultLabelPage) {
		t.Errorf("list_labels = %v, want Backend, bug and ui by name, the issue's backend taken for Backend", page)
	}
	if got := counts(page); got["Backend"] != float64(1) || got["ui"] != float64(0) {
		t.Errorf("the labels are used %v times, want Backend once and ui never", got)
	}
	if got := names(answer(ListLabelsTool, map[string]any{"q": "U"})["labels"]); got != "ui bug" {
		t.Errorf("list_labels for u = %q, want the name starting with it first", got)
	}
	if got := answer(ListLabelsTool, map[string]any{"limit": 2, "offset": 2}); names(got["labels"]) != "ui" || got["total"] != float64(3) {
		t.Errorf("the second page of two = %v", got)
	}
	if got := names(answer(GetIssueTool, map[string]any{"issueKey": "CP-2"})["issue"].(map[string]any)["labels"]); got != "backend bug" {
		t.Errorf("CP-2 carries %q, want its labels by name", got)
	}

	edited := answer(BulkEditIssuesTool, map[string]any{"keys": []string{"CP-1", "CP-3", "CP-9"},
		"change": map[string]any{"addLabels": []string{"BUG", "export", "bug"}}})
	if got := strings.Join(stringSlice(edited["applied"]), " "); got != "CP-1" || len(edited["refused"].([]any)) != 2 {
		t.Errorf("the bulk change = %v, want CP-1 applied and the issues erin cannot read refused", edited)
	}
	if got := names(answer(GetIssueTool, map[string]any{"issueKey": "CP-1"})["issue"].(map[string]any)["labels"]); got != "bug export" {
		t.Errorf("CP-1 carries %q, want bug in its first spelling and export coined", got)
	}
	if got := counts(answer(ListLabelsTool, map[string]any{})); got["export"] != float64(1) || got["bug"] != float64(2) {
		t.Errorf("after the change the labels are used %v times", got)
	}
	bad := answer(BulkEditIssuesTool, map[string]any{"keys": []string{"CP-1"}, "change": map[string]any{"addLabels": []string{"two words"}}})
	if refused := bad["refused"].([]any); len(refused) != 1 || !strings.Contains(refused[0].(map[string]any)["reason"].(string), "one word") {
		t.Errorf("a label of two words = %v, want it refused for the issue", bad)
	}
	written := c.controlled("GET", "/control/tenants/"+tenant+"/records", nil, http.StatusOK)
	if got := strings.Join(stringSlice(written["labels"].(map[string]any)["CP-1"]), " "); got != "bug export" {
		t.Errorf("the records hold CP-1's labels as %q", got)
	}

	for _, args := range []map[string]any{{"keys": []string{}, "change": map[string]any{"addLabels": []string{"ui"}}},
		{"keys": []string{"CP-1"}, "change": map[string]any{}}, {"keys": []string{"CP-1"}, "change": map[string]any{"priority": "high"}}} {
		if refused := c.tool(tenant, erin, BulkEditIssuesTool, args); refused["isError"] != true {
			t.Errorf("Armature would refuse the bulk change %v the stub answered: %v", args, refused)
		}
	}
}

// Only a grant of issue.write labels through bulk_edit_issues, as Armature's appWrites lists POST /issues/bulk.
func TestArmatureBulkEditNeedsIssueWrite(t *testing.T) {
	c := newContract(t)
	const tenant = "armature--bulk-scopes"
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "product": "armature", "labels": []string{"ui"},
		"issues": map[string]any{"CP-1": map[string]any{"summary": "s", "readers": []string{"alice"}}}}, http.StatusCreated)
	grant := c.armatureGrant(tenant, "alice", "read issue.write comment.write")
	change := map[string]any{"keys": []string{"CP-1"}, "change": map[string]any{"addLabels": []string{"ui"}}}
	for scope, want := range map[string]bool{"read": false, "read comment.write": false, "read issue.write": true} {
		token := grant(scope)
		if labelled := c.tool(tenant, token, BulkEditIssuesTool, change)["isError"] != true; labelled != want {
			t.Errorf("a token of %q labelled: %v, want %v", scope, labelled, want)
		}
		if listed := c.tool(tenant, token, ListLabelsTool, map[string]any{})["isError"] != true; !listed {
			t.Errorf("a token of %q could not list the labels", scope)
		}
	}
}

func stringSlice(v any) []string {
	var out []string
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}
