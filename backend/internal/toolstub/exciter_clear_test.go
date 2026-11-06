package toolstub

import (
	"net/http"
	"testing"
)

// Clearing a suspect link confirms it against the current revisions of its ends, and a comment on a requirement is
// kept in the records, both in Exciter's shapes.
func TestClearingASuspectLinkAndCommentingOnARequirement(t *testing.T) {
	c := newContract(t)
	const tenant = "clearing"
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "oauth": false}, http.StatusCreated)
	erin := PATPrefix + "erin"
	create := func(title string) map[string]any {
		return c.answers(tenant, erin, CreateRequirementTool, map[string]any{"projectKey": "RTR", "title": title})["requirement"].(map[string]any)
	}
	pay, tc := create("Pay by card"), create("Pays by card")
	c.answers(tenant, erin, LinkRequirementsTool, map[string]any{"reqKey": tc["key"], "otherKey": pay["key"], "typeKey": "verifies"})
	c.answers(tenant, erin, UpdateRequirementTool, map[string]any{"reqKey": pay["key"], "baseRevision": 1, "statementMarkdown": "By card or wallet."})
	link := func() map[string]any {
		return c.answers(tenant, erin, ListLinksTool, map[string]any{"reqKey": pay["key"]})["links"].([]any)[0].(map[string]any)
	}
	if len(link()["suspect"].([]any)) != 1 {
		t.Fatalf("the link should be suspect after the change: %v", link())
	}
	id, source := link()["id"], link()["other"].(map[string]any)["revision"]

	c.refuses(tenant, erin, map[string][]map[string]any{ClearSuspectLinkTool: {
		{"linkID": id, "sourceRevision": source, "targetRevision": 1, "comment": "Still holds."},
		{"linkID": id, "sourceRevision": source, "targetRevision": 2, "comment": " "},
		{"linkID": stubID("nothing"), "sourceRevision": source, "targetRevision": 2, "comment": "Still holds."},
	}})
	if len(link()["suspect"].([]any)) != 1 {
		t.Errorf("a refused clearing changed the link: %v", link())
	}
	cleared := c.answers(tenant, erin, ClearSuspectLinkTool, map[string]any{"linkID": id, "sourceRevision": source,
		"targetRevision": 2, "comment": "The wallet does not change the case."})["link"].(map[string]any)
	if len(cleared["suspect"].([]any)) != 0 || cleared["targetRevision"] != float64(2) || len(link()["suspect"].([]any)) != 0 {
		t.Errorf("the cleared link reads %v, then %v", cleared, link())
	}

	thread := c.answers(tenant, erin, CommentOnRequirementTool, map[string]any{"reqKey": pay["key"], "bodyMarkdown": "Needs a wallet case."})["thread"].(map[string]any)
	if thread["resolved"] != false || len(thread["comments"].([]any)) != 1 {
		t.Errorf("the new thread reads %v", thread)
	}
	c.refuses(tenant, erin, map[string][]map[string]any{CommentOnRequirementTool: {
		{"reqKey": pay["key"], "bodyMarkdown": "  "}, {"reqKey": "RTR-99", "bodyMarkdown": "Hello."}}})
	recorded := c.controlled("GET", "/control/tenants/"+tenant+"/records", nil, http.StatusOK)["requirementComments"].(map[string]any)
	comments, _ := recorded[pay["key"].(string)].([]any)
	if len(comments) != 1 || comments[0].(map[string]any)["bodyMarkdown"] != "Needs a wallet case." || comments[0].(map[string]any)["author"] != "erin" {
		t.Errorf("the records hold %v", recorded)
	}
}
