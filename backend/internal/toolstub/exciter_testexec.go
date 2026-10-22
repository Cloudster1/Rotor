package toolstub

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Test executions at Exciter (#139): a tenant's test plans come from its options, as only Exciter's web client makes
// them, and a caller starts an execution of one, records its cases' results and closes it, kept per tenant.

// TestPlan is a test plan of an Exciter tenant: its project, its title and the keys of the test cases it picks.
type TestPlan struct {
	Project string   `json:"project"`
	Title   string   `json:"title"`
	Cases   []string `json:"cases"`
}

// TestPlanID is the id a tenant's test plan answers with, so a test can name it.
func TestPlanID(tenant string, plan TestPlan) string {
	return stubID(tenant, "test-plan", plan.Project, plan.Title)
}

// Exciter's bounds and words for test executions, as its API has them.
const (
	maxTitle         = 100
	maxVersion       = 200
	maxConfiguration = 500
	maxTesters       = 50
	maxNote          = 4000
	maxCloseComment  = 500
	testNotFound     = "That test plan, execution, case or file was not found. It may have been deleted."
	testForbidden    = "This is not yours to do here: plans are their project's editors', an execution its manager's, and results its testers' who may comment."
	executionClosed  = "The test execution is closed and never changes. Start another to run the cases again."
	outcomeRefused   = "Record passed, failed, blocked or skipped."
)

var outcomes = []string{"passed", "failed", "blocked", "skipped"}

// planCase is a case of a plan: a test case somebody wrote at the tenant, or one the options name that nobody did,
// which runs with no steps.
type planCase struct {
	key, id, title string
	revision       int
	content        map[string]any
	steps          int
}

func casesOf(t *tenant, who *caller, plan TestPlan) []planCase {
	out := []planCase{}
	for _, key := range plan.Cases {
		pc := planCase{key: key, id: stubID("requirement", key), title: "Test case " + key, revision: 1}
		if r := findRequirement(t, who, key); r != nil {
			pc.id, pc.title, pc.revision = r.id(), r.title, r.revision
			if r.testCase != nil {
				pc.content, pc.steps = r.testCase.content, r.testCase.steps
			}
		}
		out = append(out, pc)
	}
	return out
}

func planAnswer(t *tenant, who *caller, plan TestPlan) map[string]any {
	cases := []map[string]any{}
	for _, c := range casesOf(t, who, plan) {
		cases = append(cases, map[string]any{"id": c.id, "key": c.key, "title": c.title, "trashed": false})
	}
	at := stubTime.Format(time.RFC3339)
	return map[string]any{"id": TestPlanID(t.name, plan), "projectKey": plan.Project, "title": plan.Title, "description": "",
		"scope": "cases", "query": "", "cases": cases, "createdBy": person("Stub"), "createdAt": at, "updatedAt": at,
		"can": map[string]any{"edit": false}}
}

// person is somebody in Exciter's PersonRef shape.
func person(username string) map[string]any {
	return map[string]any{"id": userID(username), "name": username}
}

func listTestPlans(t *tenant, who *caller, args map[string]json.RawMessage) any {
	project := stringArg(args, "projectKey")
	if project == "" {
		return refusal("Name the project, as projectKey.")
	}
	plans := []map[string]any{}
	for _, plan := range t.opts.TestPlans {
		if plan.Project == project {
			plans = append(plans, planAnswer(t, who, plan))
		}
	}
	slices.SortFunc(plans, func(a, b map[string]any) int { return strings.Compare(a["title"].(string), b["title"].(string)) })
	return answered(map[string]any{"plans": plans})
}

// execution is a test execution of a tenant, with what was recorded of each case.
type execution struct {
	id, project, title, planID, planTitle string
	version, configuration, manager       string
	dueAt                                 any
	testers                               []map[string]any
	state, closeComment, openedAt         string
	closedAt                              any
	cases                                 []*caseRun
}

// caseRun is a case of an execution, pinned when it started, and what was recorded of it.
type caseRun struct {
	planCase
	outcome      *string
	note, by, at string
	results      map[int]stepResult
}

type stepResult struct{ outcome, note, by, at string }

// mayRecord says whether the caller records in the execution: its manager, a tester named, or anybody of a group.
func (e *execution) mayRecord(who *caller) bool {
	if who == nil {
		return false
	}
	return who.username == e.manager || slices.ContainsFunc(e.testers, func(s map[string]any) bool {
		return s["subjectType"] == "group" || s["userId"] == userID(who.username)
	})
}

// answer is the execution in Exciter's TestExecution shape as the caller reads it, nil for nobody in particular.
func (e *execution) answer(who *caller, withCases bool) map[string]any {
	progress := map[string]int{"total": len(e.cases), "passed": 0, "failed": 0, "blocked": 0, "skipped": 0, "notRun": 0}
	cases := []map[string]any{}
	for _, c := range e.cases {
		if c.outcome == nil {
			progress["notRun"]++
		} else {
			progress[*c.outcome]++
		}
		cases = append(cases, c.run())
	}
	out := map[string]any{"id": e.id, "projectKey": e.project, "title": e.title, "planId": e.planID, "planTitle": e.planTitle,
		"baseline": nil, "version": e.version, "configuration": e.configuration, "manager": person(e.manager), "dueAt": e.dueAt,
		"state": e.state, "testers": e.testers, "excluded": []any{}, "progress": progress, "closeComment": e.closeComment,
		"openedAt": e.openedAt, "closedAt": e.closedAt,
		"can": map[string]any{"manage": who != nil && who.username == e.manager, "record": e.mayRecord(who)}}
	if withCases {
		out["cases"] = cases
	}
	return out
}

// run is the case in Exciter's TestCaseRun shape.
func (c *caseRun) run() map[string]any {
	out := map[string]any{"case": map[string]any{"id": c.id, "key": c.key, "title": c.title, "trashed": false}, "iteration": 1,
		"revision": c.revision, "values": map[string]any{}, "outcome": c.outcome, "note": c.note, "source": nil,
		"recordedBy": nil, "recordedAt": nil, "ci": nil}
	if c.outcome != nil {
		out["source"], out["recordedBy"], out["recordedAt"] = "tester", person(c.by), c.at
	}
	return out
}

// answer is the case in Exciter's TestExecutionCase shape, with its steps as they were when the execution started.
func (c *caseRun) answer() map[string]any {
	var preconditions any
	steps := []any{}
	if c.content != nil {
		preconditions, steps = c.content["preconditions"], c.content["steps"].([]any)
	}
	results := []map[string]any{}
	for step := 1; step <= c.steps; step++ {
		if r, ok := c.results[step]; ok {
			results = append(results, map[string]any{"step": step, "outcome": r.outcome, "note": r.note,
				"recordedBy": person(r.by), "recordedAt": r.at})
		}
	}
	return map[string]any{"run": c.run(), "preconditions": preconditions, "steps": steps, "results": results,
		"files": []any{}, "issues": []any{}, "iterations": 1, "parameters": []any{}}
}

// rollup is what a case comes to from its steps, as Exciter's testexec.Rollup: failed when any failed, else blocked
// when any is, and once every step is recorded, skipped when all were and passed otherwise; nil while one is open.
func rollup(results map[int]stepResult, steps int) *string {
	var out *string
	for _, r := range results {
		switch r.outcome {
		case "failed":
			failed := "failed"
			return &failed
		case "blocked":
			blocked := "blocked"
			out = &blocked
		}
	}
	if out != nil || steps == 0 || len(results) < steps {
		return out
	}
	verdict := "skipped"
	for _, r := range results {
		if r.outcome != "skipped" {
			verdict = "passed"
		}
	}
	return &verdict
}

// tooLong says whether a text runs past max characters.
func tooLong(text string, max int) bool { return utf8.RuneCountInString(text) > max }

// checkTesters reads the testers to name, answering what Exciter refuses of them.
func checkTesters(raw json.RawMessage) ([]map[string]any, string) {
	var testers []struct {
		SubjectType string `json:"subjectType"`
		UserID      string `json:"userId"`
		GroupID     string `json:"groupId"`
		Name        string `json:"name"`
	}
	if json.Unmarshal(raw, &testers) != nil || len(testers) < 1 || len(testers) > maxTesters {
		return nil, fmt.Sprintf("Name 1 to %d people or groups who test.", maxTesters)
	}
	out, seen := []map[string]any{}, map[string]bool{}
	for _, s := range testers {
		id, field := s.UserID, "userId"
		if s.SubjectType == "group" {
			id, field = s.GroupID, "groupId"
		}
		switch {
		case id == "" || (s.SubjectType != "user" && s.SubjectType != "group"):
			return nil, "Name each tester by the id of the person or the group."
		case seen[id]:
			return nil, "Name each tester once."
		}
		seen[id] = true
		out = append(out, map[string]any{"subjectType": s.SubjectType, field: id, "name": s.Name})
	}
	return out, ""
}
