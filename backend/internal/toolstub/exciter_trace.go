package toolstub

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"
)

// What a feature workflow traces at Exciter (#139): requirement types, test cases among them, a project's tree, and
// links between requirements, answered as Exciter answers them.

// The roles of a project's requirement types; every project of a tenant has one type of each.
const (
	roleRequirement = "requirement"
	roleTestCase    = "testCase"
)

var typeNames = map[string]string{roleRequirement: "Requirement", roleTestCase: "Test case"}

// Exciter's words for what it refuses of types, test cases and links.
const (
	typeNotOurs   = "That type is not one of this project's. Pick one from its types."
	notTestCase   = "Only a requirement of a type marked as test cases has preconditions, steps and parameters. Pick such a type first."
	testCaseShape = "Send the test case as preconditions, steps of an action and an expected result, and parameters."
)

// typeID is the id of a project's type of a role.
func typeID(project, role string) string { return stubID("type", project, role) }

// typeRole is the role of a type of the project, or "" for an id that names none of its types.
func typeRole(project, id string) string {
	for role := range typeNames {
		if typeID(project, role) == id {
			return role
		}
	}
	return ""
}

// typeRef is a requirement's type in Exciter's RequirementTypeRef shape, nil for none.
func typeRef(project string, id *string) any {
	if id == nil {
		return nil
	}
	role := typeRole(project, *id)
	return map[string]any{"id": *id, "name": typeNames[role], "icon": "", "colour": ""}
}

func listTypes(args map[string]json.RawMessage) any {
	project := stringArg(args, "projectKey")
	if project == "" {
		return refusal("Name the project, as projectKey.")
	}
	at := stubTime.Format(time.RFC3339)
	types := []map[string]any{}
	for i, role := range []string{roleRequirement, roleTestCase} {
		types = append(types, map[string]any{"id": typeID(project, role), "name": typeNames[role], "description": "", "icon": "",
			"colour": "", "position": i + 1, "workflowId": nil, "role": role, "roleHeld": false, "retired": false, "requirements": 0,
			"createdAt": at, "updatedAt": at})
	}
	return answered(map[string]any{"types": types})
}

// testCase is what a test case carries, as Exciter answers it, and how many steps it has.
type testCase struct {
	content map[string]any
	steps   int
}

func (tc *testCase) answer() map[string]any { return tc.content }

// readTestCase takes a test case's content, each text as a document or as Markdown, which Exciter keeps as both.
func readTestCase(raw json.RawMessage) (*testCase, bool) {
	var in struct {
		Preconditions         json.RawMessage `json:"preconditions"`
		PreconditionsMarkdown *string         `json:"preconditionsMarkdown"`
		Steps                 []struct {
			Action           json.RawMessage `json:"action"`
			Expected         json.RawMessage `json:"expected"`
			ActionMarkdown   *string         `json:"actionMarkdown"`
			ExpectedMarkdown *string         `json:"expectedMarkdown"`
		} `json:"steps"`
		Parameters []struct {
			Name   string   `json:"name"`
			Values []string `json:"values"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, false
	}
	content := map[string]any{"steps": []any{}, "parameters": []any{}}
	putText(content, "preconditions", in.Preconditions, in.PreconditionsMarkdown)
	for _, step := range in.Steps {
		out := map[string]any{}
		putText(out, "action", step.Action, step.ActionMarkdown)
		putText(out, "expected", step.Expected, step.ExpectedMarkdown)
		content["steps"] = append(content["steps"].([]any), out)
	}
	for _, p := range in.Parameters {
		values := p.Values
		if values == nil {
			values = []string{}
		}
		content["parameters"] = append(content["parameters"].([]any), map[string]any{"name": p.Name, "values": values})
	}
	return &testCase{content: content, steps: len(in.Steps)}, true
}

// putText puts a text of a test case under name: the document given, or the Markdown's with the Markdown beside it.
func putText(out map[string]any, name string, document json.RawMessage, markdown *string) {
	switch {
	case markdown != nil:
		out[name], out[name+"Markdown"] = markdownDocument(*markdown), *markdown
	case len(document) > 0 && string(document) != "null":
		out[name] = document
	default:
		out[name] = nil
	}
}

// applyType takes a requirement's type and test case from the arguments, answering what Exciter would refuse.
func (r *storedRequirement) applyType(args map[string]json.RawMessage) string {
	if raw, sent := args["typeId"]; sent {
		var id *string
		if json.Unmarshal(raw, &id) != nil || (id != nil && typeRole(r.project, *id) == "") {
			return typeNotOurs
		}
		r.typeID = id
	}
	role := ""
	if r.typeID != nil {
		role = typeRole(r.project, *r.typeID)
	}
	raw, sent := args["testCase"]
	switch {
	case sent && role != roleTestCase:
		return notTestCase
	case sent:
		tc, ok := readTestCase(raw)
		if !ok {
			return testCaseShape
		}
		r.testCase = tc
	case role != roleTestCase:
		r.testCase = nil
	case r.testCase == nil:
		r.testCase, _ = readTestCase(json.RawMessage(`{}`))
	}
	return ""
}

// getTree answers the requirements of a project the caller may read in reading order: each under the one it was
// placed under, siblings in the order they were made. The stub numbers outlines by key, not by place.
func getTree(t *tenant, who *caller, args map[string]json.RawMessage) any {
	project := stringArg(args, "projectKey")
	if project == "" {
		return refusal("Name the project, as projectKey.")
	}
	var mine []*storedRequirement
	for _, r := range readableRequirements(t, who) {
		if r.project == project {
			mine = append(mine, r)
		}
	}
	nodes := []map[string]any{}
	var walk func(parent *string, depth int)
	walk = func(parent *string, depth int) {
		for _, r := range mine {
			if !sameParent(r.parentID, parent) {
				continue
			}
			children := 0
			for _, c := range mine {
				if c.parentID != nil && *c.parentID == r.id() {
					children++
				}
			}
			nodes = append(nodes, map[string]any{"id": r.id(), "key": r.key, "kind": r.kind, "title": r.title, "parentId": r.parentID,
				"depth": depth, "outline": strconv.Itoa(r.number), "typeId": r.typeID, "status": nil, "revision": r.revision,
				"children": children, "quality": nil})
			id := r.id()
			walk(&id, depth+1)
		}
	}
	walk(nil, 0)
	return answered(map[string]any{"nodes": nodes})
}

func sameParent(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// linkType is one of the link types every Exciter installation starts with.
type linkType struct {
	key, name, forward, backward string
	acyclic                      bool
	suspectEnd, counts           string
}

var linkTypes = []linkType{
	{"derives", "Derives", "derives from", "is derived into", true, "target", "decomposition"},
	{"refines", "Refines", "refines", "is refined by", true, "target", "decomposition"},
	{"satisfies", "Satisfies", "satisfies", "is satisfied by", true, "target", "decomposition"},
	{"verifies", "Verifies", "verifies", "is verified by", true, "target", "verification"},
	{"conflicts", "Conflicts", "conflicts with", "conflicts with", false, "both", "nothing"},
	{"relates", "Relates", "relates to", "relates to", false, "none", "nothing"},
	{"mitigates", "Mitigates", "mitigates", "is mitigated by", true, "source", "mitigation"},
	{"leads_to", "Leads to", "leads to", "follows from", true, "none", "nothing"},
}

func (lt linkType) id() string { return stubID("link-type", lt.key) }

func (lt linkType) answer() map[string]any {
	return map[string]any{"id": lt.id(), "key": lt.key, "name": lt.name, "forward": lt.forward, "backward": lt.backward,
		"acyclic": lt.acyclic, "suspectEnd": lt.suspectEnd, "counts": lt.counts, "scope": "org", "projectKey": "",
		"enabled": true, "rules": []any{}, "links": 0, "retired": false, "updatedAt": stubTime.Format(time.RFC3339)}
}

// requirementLink is a link between two requirements of a tenant, by their keys.
type requirementLink struct {
	id, typeKey, source, target string
	sourceRevision              int
	targetRevision              int
	createdBy, createdAt        string
}

func (l requirementLink) view() map[string]any {
	return map[string]any{"id": l.id, "type": l.typeKey, "source": l.source, "target": l.target,
		"sourceRevision": l.sourceRevision, "targetRevision": l.targetRevision, "createdByName": l.createdBy, "createdAt": l.createdAt}
}

// linkRequirements links a requirement to another by a link type's key or id, the requirement the source unless the
// direction is incoming; a self-link and a repeat are refused, as Exciter refuses them.
func (s *Stub) linkRequirements(c *call, who *caller, args map[string]json.RawMessage) any {
	req := findRequirement(c.t, who, stringArg(args, "reqKey"))
	otherKey := strings.TrimSpace(stringArg(args, "otherKey"))
	other := findRequirement(c.t, who, otherKey)
	direction := stringArg(args, "direction")
	if direction == "" {
		direction = "outgoing"
	}
	typeKey, typeID := stringArg(args, "typeKey"), stringArg(args, "typeId")
	at := slices.IndexFunc(linkTypes, func(lt linkType) bool { return lt.key == typeKey || lt.id() == typeID })
	switch {
	case req == nil:
		return refusal(requirementNotFound)
	case otherKey == "":
		return refusal("Name the requirement to link to, such as CHK-R2.")
	case other == nil:
		return refusal("There is no requirement " + otherKey + " you can see. Check the key.")
	case other.key == req.key:
		return refusal("A requirement cannot link to itself. Pick another.")
	case direction != "outgoing" && direction != "incoming":
		return refusal("Say whether this requirement is the link's source (outgoing) or its target (incoming).")
	case at < 0:
		return refusal("Pick a link type, such as derives.")
	}
	lt, source, target := linkTypes[at], req, other
	phrase := lt.forward
	if direction == "incoming" {
		source, target, phrase = other, req, lt.backward
	}
	for _, l := range c.t.written.links {
		if l.typeKey == lt.key && l.source == source.key && l.target == target.key {
			return refusal("Those requirements are already linked that way.")
		}
	}
	link := requirementLink{id: stubID(c.t.name, "link", lt.key, source.key, target.key), typeKey: lt.key, source: source.key,
		target: target.key, sourceRevision: source.revision, targetRevision: target.revision, createdBy: who.username,
		createdAt: s.stubNow()}
	c.t.written.links = append(c.t.written.links, link)
	return answered(map[string]any{"link": map[string]any{"id": link.id, "type": lt.answer(), "direction": direction,
		"phrase": phrase, "other": other.ref(), "sourceRevision": link.sourceRevision, "targetRevision": link.targetRevision,
		"suspect": []any{}, "createdAt": link.createdAt, "createdByName": who.username, "via": via(c.t)}})
}
