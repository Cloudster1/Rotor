package toolstub

import (
	"net/http"
	"testing"
)

// What the story workflow writes at Exciter answers in each tool's output schema in Exciter's snapshot, is kept for
// the next read, and is refused where Exciter refuses it, such as a revision written over another (#130).
func TestExciterWritesAnswerInTheShapesExciterLists(t *testing.T) {
	c := newContract(t)
	const tenant = "writes"
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "oauth": false,
		"requirements": map[string]any{"RTR-1": []string{"erin"}}}, http.StatusCreated)
	erin := PATPrefix + "erin"
	answer := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result := c.tool(tenant, erin, name, args)
		if result["isError"] == true {
			t.Fatalf("%s %v refused: %v", name, args, result)
		}
		structured, _ := result["structuredContent"].(map[string]any)
		fits(t, outputSchema(t, name), name, structured)
		return structured
	}

	made := answer(CreateRequirementTool, map[string]any{"projectKey": "RTR", "title": "Passkeys",
		"statementMarkdown": "The system shall let a member sign in with a passkey.", "priority": "must", "labels": []string{"auth"}})
	if req := made["requirement"].(map[string]any); req["key"] != "RTR-2" || req["revision"] != float64(1) {
		t.Errorf("the new requirement = %v, want RTR-2 after the options' RTR-1", req)
	}
	if found := answer(SearchRequirementsTool, map[string]any{"q": "PASSKEY sign", "project": []string{"RTR"}}); found["total"] != float64(1) {
		t.Errorf("searching for the new requirement = %v", found)
	}
	if found := answer(SearchRequirementsTool, map[string]any{"q": "fingerprint"}); found["total"] != float64(0) {
		t.Errorf("searching for words no requirement has = %v", found)
	}
	revised := answer(UpdateRequirementTool, map[string]any{"reqKey": "RTR-2", "baseRevision": 1, "statementMarkdown": "Passkeys only."})
	if req := revised["requirement"].(map[string]any); req["revision"] != float64(2) || req["statementMarkdown"] != "Passkeys only." {
		t.Errorf("the revised requirement = %v", req)
	}
	answer(LinkArmatureIssueTool, map[string]any{"reqKey": "RTR-2", "issueKey": "CP-12", "relation": "implements"})
	if links := answer(ListArmatureLinksTool, map[string]any{"reqKey": "RTR-2"})["links"].([]any); len(links) != 1 {
		t.Errorf("RTR-2 lists %d Armature links, want 1", len(links))
	}
	reviews := answer(RequestReviewsTool, map[string]any{"projectKey": "RTR", "comment": "From CP-12",
		"items": []map[string]any{{"key": "RTR-2", "revision": 2}, {"key": "RTR-2", "revision": 1}}})["results"].([]any)
	if len(reviews) != 2 || reviews[0].(map[string]any)["outcome"] != "requested" || reviews[1].(map[string]any)["outcome"] != "refused" {
		t.Errorf("requesting reviews = %v, want the current revision requested and the old one refused", reviews)
	}
	if read := c.tool(tenant, PATPrefix+"frank", GetRequirementTool, map[string]any{"reqKey": "RTR-2"}); read["isError"] == true {
		t.Errorf("a requirement somebody made is hidden from another member: %v", read)
	}

	for _, refused := range []map[string]any{
		c.tool(tenant, erin, UpdateRequirementTool, map[string]any{"reqKey": "RTR-2", "baseRevision": 1, "title": "Stale"}),
		c.tool(tenant, erin, UpdateRequirementTool, map[string]any{"reqKey": "RTR-2", "title": "No base"}),
		c.tool(tenant, erin, UpdateRequirementTool, map[string]any{"reqKey": "RTR-9", "baseRevision": 1}),
		c.tool(tenant, erin, CreateRequirementTool, map[string]any{"projectKey": "RTR"}),
		c.tool(tenant, erin, LinkArmatureIssueTool, map[string]any{"reqKey": "RTR-2", "issueKey": "CP-12", "relation": "blocks"}),
	} {
		if refused["isError"] != true {
			t.Errorf("Exciter would refuse what the stub answered: %v", refused)
		}
	}
	written := c.controlled("GET", "/control/tenants/"+tenant+"/records", nil, http.StatusOK)
	if len(written["requirements"].([]any)) != 1 || len(written["armatureLinks"].(map[string]any)["RTR-2"].([]any)) != 1 ||
		len(written["reviews"].([]any)) != 1 {
		t.Errorf("the records = %v", written)
	}
}
