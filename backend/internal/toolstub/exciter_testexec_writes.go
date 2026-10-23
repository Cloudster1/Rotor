package toolstub

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The writes of a test execution at Exciter (#139): starting one of a plan, recording a case's result and closing
// it, refused where Exciter refuses them.

func (s *Stub) startTestExecution(c *call, who *caller, args map[string]json.RawMessage) any {
	project, title := stringArg(args, "projectKey"), strings.TrimSpace(stringArg(args, "title"))
	version, configuration := strings.TrimSpace(stringArg(args, "version")), strings.TrimSpace(stringArg(args, "configuration"))
	planID := stringArg(args, "planId")
	at := slices.IndexFunc(c.t.opts.TestPlans, func(p TestPlan) bool { return p.Project == project && TestPlanID(c.t.name, p) == planID })
	testers, refused := checkTesters(args["testers"])
	switch {
	case title == "" || tooLong(title, maxTitle):
		return refusal(fmt.Sprintf("Name the execution in at most %d characters, such as Release 2.0 on Linux.", maxTitle))
	case tooLong(version, maxVersion):
		return refusal(fmt.Sprintf("Keep the version to %d characters.", maxVersion))
	case tooLong(configuration, maxConfiguration):
		return refusal(fmt.Sprintf("Keep the configuration to %d characters.", maxConfiguration))
	case refused != "":
		return refusal(refused)
	case at < 0:
		return refusal("Pick a test plan of this project.")
	case stringArg(args, "baselineId") != "":
		return refusal("Pick a baseline of this project, or none to run the cases as they are now.")
	}
	plan := c.t.opts.TestPlans[at]
	cases := []*caseRun{}
	for _, pc := range casesOf(c.t, who, plan) {
		cases = append(cases, &caseRun{planCase: pc, results: map[int]stepResult{}})
	}
	if len(cases) == 0 {
		return refusal("The plan holds no test case to run here. Add cases to it, or pick another baseline.")
	}
	var due any
	if raw, sent := args["dueAt"]; sent {
		_ = json.Unmarshal(raw, &due)
	}
	e := &execution{id: stubID(c.t.name, "test-execution", strconv.Itoa(len(c.t.written.executions)+1)), project: project,
		title: title, planID: planID, planTitle: plan.Title, version: version, configuration: configuration,
		manager: who.username, dueAt: due, testers: testers, state: "open", openedAt: s.stubNow(), cases: cases}
	c.t.written.executions = append(c.t.written.executions, e)
	return answered(map[string]any{"execution": e.answer(who, true)})
}

// findExecution is an execution of the tenant by its id, or nil.
func findExecution(t *tenant, id string) *execution {
	for _, e := range t.written.executions {
		if e.id == id {
			return e
		}
	}
	return nil
}

// recordTestResult records each step's outcome and actual result, and the case's outcome, which follows from its
// steps when none is given; whoever records last is its recorder. The stub runs each case once, as iteration 1.
func (s *Stub) recordTestResult(c *call, who *caller, args map[string]json.RawMessage) any {
	e := findExecution(c.t, stringArg(args, "executionID"))
	switch {
	case e == nil:
		return refusal(testNotFound)
	case e.state != "open":
		return refusal(executionClosed)
	case !e.mayRecord(who):
		return refusal(testForbidden)
	}
	key, iteration := strings.TrimSpace(stringArg(args, "reqKey")), 1
	_ = json.Unmarshal(args["iteration"], &iteration)
	at := slices.IndexFunc(e.cases, func(cr *caseRun) bool { return cr.key == key })
	if at < 0 || max(iteration, 1) != 1 {
		return refusal(testNotFound)
	}
	cr := e.cases[at]
	var in struct {
		Outcome *string `json:"outcome"`
		Note    string  `json:"note"`
		Steps   []struct {
			Step    int    `json:"step"`
			Outcome string `json:"outcome"`
			Note    string `json:"note"`
		} `json:"steps"`
	}
	raw, _ := json.Marshal(args)
	_ = json.Unmarshal(raw, &in)
	switch {
	case tooLong(in.Note, maxNote):
		return refusal(fmt.Sprintf("Keep the note to %d characters.", maxNote))
	case in.Outcome != nil && !slices.Contains(outcomes, *in.Outcome):
		return refusal(outcomeRefused)
	}
	for _, step := range in.Steps {
		switch {
		case step.Step < 1 || step.Step > cr.steps:
			return refusal(fmt.Sprintf("The case has steps 1 to %d at the revision it runs. Record one of those.", cr.steps))
		case !slices.Contains(outcomes, step.Outcome):
			return refusal(outcomeRefused)
		case tooLong(step.Note, maxNote):
			return refusal(fmt.Sprintf("Keep the actual result to %d characters.", maxNote))
		}
	}
	now := s.stubNow()
	for _, step := range in.Steps {
		cr.results[step.Step] = stepResult{outcome: step.Outcome, note: strings.TrimSpace(step.Note), by: who.username, at: now}
	}
	cr.outcome, cr.note, cr.by, cr.at = in.Outcome, strings.TrimSpace(in.Note), who.username, now
	if cr.outcome == nil {
		cr.outcome = rollup(cr.results, cr.steps)
	}
	return answered(map[string]any{"case": cr.answer()})
}

// closeTestExecution closes an execution its manager closes, or aborts it so it counts for nothing.
func (s *Stub) closeTestExecution(c *call, who *caller, args map[string]json.RawMessage) any {
	e := findExecution(c.t, stringArg(args, "executionID"))
	comment := strings.TrimSpace(stringArg(args, "comment"))
	var abort bool
	_ = json.Unmarshal(args["abort"], &abort)
	switch {
	case tooLong(comment, maxCloseComment):
		return refusal(fmt.Sprintf("Keep the comment to %d characters.", maxCloseComment))
	case e == nil:
		return refusal(testNotFound)
	case e.manager != who.username:
		return refusal(testForbidden)
	case e.state != "open":
		return refusal(executionClosed)
	}
	e.state, e.closeComment, e.closedAt = "closed", comment, s.stubNow()
	if abort {
		e.state = "aborted"
	}
	return answered(map[string]any{"execution": e.answer(who, true)})
}
