package toolstub

import (
	"encoding/json"
	"strconv"
	"strings"
)

// commentOnRequirement starts a thread on a requirement the caller reads, as Exciter's comment_on_requirement; the
// thread is kept in the tenant's records by requirement key so a test reads what a workflow said.
func (s *Stub) commentOnRequirement(c *call, who *caller, args map[string]json.RawMessage) any {
	req := findRequirement(c.t, who, stringArg(args, "reqKey"))
	if req == nil {
		return refusal(requirementNotFound)
	}
	markdown := stringArg(args, "bodyMarkdown")
	body := args["body"]
	if string(body) == "null" || len(body) == 0 {
		body = nil
	}
	if strings.TrimSpace(markdown) == "" && body == nil {
		return refusal("Say what the comment says, as bodyMarkdown.")
	}
	var doc any = markdownDocument(markdown)
	if body != nil {
		_ = json.Unmarshal(body, &doc)
	}
	now := s.stubNow()
	n := strconv.Itoa(len(c.t.written.requirementComments[req.key]))
	threadID := stubID(c.t.name, "thread", req.key, n)
	comment := map[string]any{"id": stubID(c.t.name, "comment", req.key, n), "threadId": threadID,
		"authorId": userID(who.username), "authorName": who.username, "revision": req.revision, "body": doc,
		"bodyMarkdown": markdown, "deleted": false, "createdAt": now, "editedAt": nil, "via": via(c.t),
		"can": map[string]any{"edit": true, "delete": true}}
	thread := map[string]any{"id": threadID, "requirementId": req.id(), "resolved": false, "resolvedByName": "",
		"resolvedAt": nil, "comments": []any{comment}, "can": map[string]any{"reply": true, "resolve": true}}
	c.t.written.requirementComments[req.key] = append(c.t.written.requirementComments[req.key], map[string]any{
		"threadId": threadID, "bodyMarkdown": markdown, "author": who.username, "revision": req.revision, "createdAt": now})
	return answered(map[string]any{"thread": thread})
}
