package toolstub

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GetRequirementTool is the reading tool the stub answers besides whoami, for the people a tenant lets read each
// requirement: Rotor asks it before an event trigger starts a run, as the trigger's owner (#35).
const GetRequirementTool = "get_requirement"

// stubTime is the time every stub requirement was made and changed, so answers are stable.
var stubTime = time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)

// getRequirement answers get_requirement in the shape of Exciter's output schema, for a key the tenant lets the
// caller read; any other key is refused as Exciter refuses one it does not show.
func getRequirement(t *tenant, who *caller, arguments json.RawMessage) any {
	var args struct {
		ReqKey string `json:"reqKey"`
	}
	_ = json.Unmarshal(arguments, &args)
	if !slices.Contains(t.opts.Requirements[args.ReqKey], who.username) {
		return refusal("There is no requirement " + args.ReqKey + ", or you may not see it.")
	}
	structured := map[string]any{"requirement": requirement(args.ReqKey)}
	text, _ := json.Marshal(structured)
	return map[string]any{"content": []toolContent{{Type: "text", Text: string(text)}}, "structuredContent": structured}
}

// requirement is a draft requirement by its key, such as RTR-1, with every field Exciter's schema asks for.
func requirement(key string) map[string]any {
	project, number, _ := strings.Cut(key, "-")
	n := 0
	for _, r := range number {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	at := stubTime.Format(time.RFC3339)
	return map[string]any{
		"id": uuid.NewSHA1(uuid.NameSpaceURL, []byte("requirement:"+key)).String(), "key": key, "projectKey": project, "number": n,
		"kind": "requirement", "title": "Requirement " + key, "statement": nil, "rationale": nil, "type": nil,
		"attributes": map[string]any{}, "revision": 1, "status": nil, "owner": nil, "priority": nil, "labels": []string{},
		"parentId": nil, "outline": "1", "formerKeys": []string{}, "copiedFrom": nil, "reuse": nil,
		"createdAt": at, "createdByName": "Stub", "updatedAt": at, "updatedByName": "Stub", "trashedAt": nil,
		"can":      map[string]any{"edit": false, "transition": false, "delete": false, "link": false, "comment": false},
		"comments": map[string]any{"threads": 0, "open": 0}, "watching": map[string]any{"requirement": false, "project": false},
		"approval": map[string]any{"pendingReviewId": nil, "approvedRevision": nil, "current": false}, "lock": nil,
	}
}
