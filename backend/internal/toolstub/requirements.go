package toolstub

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Exciter's tools the stub answers besides whoami, in their snapshot's output schemas, which clients check answers
// against: get_requirement before a trigger starts a run (#35), the story workflow's (#130), and the feature
// workflow's tracing and test executions (#139).
const (
	GetRequirementTool     = "get_requirement"
	SearchRequirementsTool = "search_requirements"
	CreateRequirementTool  = "create_requirement"
	UpdateRequirementTool  = "update_requirement"
	ListArmatureLinksTool  = "list_armature_links"
	LinkArmatureIssueTool  = "link_armature_issue"
	RequestReviewsTool     = "request_reviews"
	ListTypesTool          = "list_types"
	GetTreeTool            = "get_tree"
	LinkRequirementsTool   = "link_requirements"
	ListTestPlansTool      = "list_test_plans"
	StartTestExecutionTool = "start_test_execution"
	RecordTestResultTool   = "record_test_result"
	CloseTestExecutionTool = "close_test_execution"
)

// stubTime is the time every requirement and issue of a tenant's options was made and changed, so answers are stable.
var stubTime = time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)

// Exciter's words for what the stub refuses, as its API says them.
const (
	requirementNotFound = "That requirement was not found. Check its key, or ask the project's administrators to let you in."
	baseRevisionMissing = "Say which revision you changed, as baseRevision, so a newer one is not overwritten."
)

// callExciter answers the Exciter tools the stub plays; ok is false for one it only lists.
func (s *Stub) callExciter(c *call, who *caller, name string, arguments json.RawMessage) (any, bool) {
	var args map[string]json.RawMessage
	_ = json.Unmarshal(arguments, &args)
	switch name {
	case WhoamiTool:
		return answered(whoami(c.t, who)), true
	case GetRequirementTool:
		req := findRequirement(c.t, who, stringArg(args, "reqKey"))
		if req == nil {
			return refusal("There is no requirement " + stringArg(args, "reqKey") + ", or you may not see it."), true
		}
		return answered(map[string]any{"requirement": req.answer()}), true
	case SearchRequirementsTool:
		return searchRequirements(c.t, who, args), true
	case CreateRequirementTool:
		return s.createRequirement(c, who, args), true
	case UpdateRequirementTool:
		return s.updateRequirement(c, who, args), true
	case ListArmatureLinksTool:
		return listArmatureLinks(c.t, who, args), true
	case LinkArmatureIssueTool:
		return s.linkArmatureIssue(c, who, args), true
	case RequestReviewsTool:
		return s.requestReviews(c, who, args), true
	case ListTypesTool:
		return listTypes(args), true
	case GetTreeTool:
		return getTree(c.t, who, args), true
	case LinkRequirementsTool:
		return s.linkRequirements(c, who, args), true
	case ListTestPlansTool:
		return listTestPlans(c.t, who, args), true
	case StartTestExecutionTool:
		return s.startTestExecution(c, who, args), true
	case RecordTestResultTool:
		return s.recordTestResult(c, who, args), true
	case CloseTestExecutionTool:
		return s.closeTestExecution(c, who, args), true
	}
	return nil, false
}

// storedRequirement is a requirement of a tenant: one its options let some people read, or one a caller created.
type storedRequirement struct {
	key, project, kind, title string
	number, revision          int
	statement, rationale      string
	priority                  *string
	labels                    []string
	createdBy, updatedBy      string
	createdAt, updatedAt      string
	pendingReview             *string
	created                   bool
	// typeID is the requirement type it was given, and testCase what a test case carries, nil for any other.
	typeID   *string
	testCase *testCase
}

// seededRequirement is a draft of the tenant's options by its key, such as RTR-1.
func seededRequirement(key string) *storedRequirement {
	project, number, _ := strings.Cut(key, "-")
	n, _ := strconv.Atoi(number)
	at := stubTime.Format(time.RFC3339)
	return &storedRequirement{key: key, project: project, kind: "requirement", title: "Requirement " + key, number: n, revision: 1,
		labels: []string{}, createdBy: "Stub", updatedBy: "Stub", createdAt: at, updatedAt: at}
}

// findRequirement is a requirement the caller may read: one somebody created there, which everyone may, or one of
// the tenant's options that names the caller.
func findRequirement(t *tenant, who *caller, key string) *storedRequirement {
	for _, r := range t.written.requirements {
		if r.key == key {
			return r
		}
	}
	if key != "" && slices.Contains(t.opts.Requirements[key], who.username) {
		return seededRequirement(key)
	}
	return nil
}

// readableRequirements are every requirement the caller may read, the options' first, by key.
func readableRequirements(t *tenant, who *caller) []*storedRequirement {
	out := []*storedRequirement{}
	keys := make([]string, 0, len(t.opts.Requirements))
	for key := range t.opts.Requirements {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if r := findRequirement(t, who, key); r != nil && !r.created {
			out = append(out, r)
		}
	}
	return append(out, t.written.requirements...)
}

func (r *storedRequirement) id() string { return stubID("requirement", r.key) }

// answer is the requirement in Exciter's Requirement shape, with every field its schema asks for.
func (r *storedRequirement) answer() map[string]any {
	var statement, rationale any
	out := map[string]any{
		"id": r.id(), "key": r.key, "projectKey": r.project, "number": r.number, "kind": r.kind, "title": r.title,
		"attributes": map[string]any{}, "revision": r.revision, "status": nil, "owner": nil, "labels": r.labels,
		"parentId": nil, "outline": strconv.Itoa(r.number), "formerKeys": []string{}, "copiedFrom": nil, "reuse": nil,
		"createdAt": r.createdAt, "createdByName": r.createdBy, "updatedAt": r.updatedAt, "updatedByName": r.updatedBy,
		"trashedAt": nil, "lock": nil, "type": typeRef(r.project, r.typeID), "computed": map[string]any{}, "risk": nil,
		"quality":  map[string]any{"score": nil, "active": 0, "accepted": 0, "stale": false},
		"can":      map[string]any{"edit": r.created, "transition": r.created, "delete": false, "link": r.created, "comment": r.created},
		"comments": map[string]any{"threads": 0, "open": 0}, "watching": map[string]any{"requirement": false, "project": false},
		"approval": map[string]any{"pendingReviewId": r.pendingReview, "approvedRevision": nil, "current": false},
	}
	if r.statement != "" {
		statement, out["statementMarkdown"] = markdownDocument(r.statement), r.statement
	}
	if r.rationale != "" {
		rationale, out["rationaleMarkdown"] = markdownDocument(r.rationale), r.rationale
	}
	out["statement"], out["rationale"] = statement, rationale
	if r.testCase != nil {
		out["testCase"] = r.testCase.answer()
	} else {
		out["testCase"] = nil
	}
	if r.priority != nil {
		out["priority"] = *r.priority
	} else {
		out["priority"] = nil
	}
	return out
}

// ref is the requirement in Exciter's RequirementRef shape.
func (r *storedRequirement) ref() map[string]any {
	return map[string]any{"id": r.id(), "key": r.key, "projectKey": r.project, "kind": r.kind, "title": r.title,
		"revision": r.revision, "status": nil, "trashed": false}
}

// via is how Exciter names what a write came through: the connection's kind, and Rotor.
func via(t *tenant) map[string]any {
	if t.oauth() {
		return map[string]any{"kind": "oauth", "name": "Rotor"}
	}
	return map[string]any{"kind": "token", "name": "Rotor"}
}

// stringArg is a string argument, or "" when it is missing or not a string.
func stringArg(args map[string]json.RawMessage, name string) string {
	var v string
	_ = json.Unmarshal(args[name], &v)
	return v
}
