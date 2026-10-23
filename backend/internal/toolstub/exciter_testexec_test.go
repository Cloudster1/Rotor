package toolstub

import (
	"net/http"
	"testing"
)

// answers calls an Exciter tool, insisting it is answered in the output schema the snapshot lists for it.
func (c *contract) answers(tenant, bearer, name string, args map[string]any) map[string]any {
	c.t.Helper()
	result := c.tool(tenant, bearer, name, args)
	if result["isError"] == true {
		c.t.Fatalf("%s %v refused: %v", name, args, result)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	fits(c.t, outputSchema(c.t, name), name, structured)
	return structured
}

// refuses insists the stub refuses each call, as Exciter would.
func (c *contract) refuses(tenant, bearer string, calls map[string][]map[string]any) {
	c.t.Helper()
	for name, each := range calls {
		for _, args := range each {
			if result := c.tool(tenant, bearer, name, args); result["isError"] != true {
				c.t.Errorf("%s %v was answered, which Exciter refuses: %v", name, args, result)
			}
		}
	}
}

// The catalogue tells reads from writes by the hints of the snapshot, so a test execution's writes ask for an
// approval and reading its results does not (#139).
func TestTheSnapshotSaysWhichTestExecutionCallsWrite(t *testing.T) {
	want := map[string]bool{StartTestExecutionTool: false, RecordTestResultTool: false, CloseTestExecutionTool: false,
		"get_test_case_results": true, ListTestPlansTool: true, LinkRequirementsTool: false, GetTreeTool: true}
	for _, tool := range listed(t, SnapshotExciter) {
		name, _ := tool["name"].(string)
		if reads, ok := want[name]; ok {
			if readOnlyTool(tool) != reads {
				t.Errorf("the snapshot's %s reads = %v, want %v", name, readOnlyTool(tool), reads)
			}
			delete(want, name)
		}
	}
	if len(want) > 0 {
		t.Errorf("the snapshot lacks %v", want)
	}
}

// A feature workflow files test cases of the project's test case type, links them to the requirements they verify
// and reads the tree, each answered in Exciter's shapes and refused where Exciter refuses (#139).
func TestTracingAnswersInTheShapesExciterLists(t *testing.T) {
	c := newContract(t)
	const tenant = "tracing"
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "oauth": false,
		"requirements": map[string]any{"RTR-1": []string{"erin"}}}, http.StatusCreated)
	erin := PATPrefix + "erin"
	types := c.answers(tenant, erin, ListTypesTool, map[string]any{"projectKey": "RTR"})["types"].([]any)
	testType := types[1].(map[string]any)
	if testType["role"] != roleTestCase {
		t.Fatalf("the project's types = %v, want a test case type second", types)
	}
	made := c.answers(tenant, erin, CreateRequirementTool, map[string]any{"projectKey": "RTR", "title": "Sign in with a passkey",
		"typeId": testType["id"], "testCase": map[string]any{"preconditions": nil, "parameters": []any{},
			"steps": []map[string]any{{"actionMarkdown": "Sign in", "expectedMarkdown": "Signed in"}}}})
	tc := made["requirement"].(map[string]any)
	if tc["key"] != "RTR-2" || tc["testCase"] == nil || tc["type"].(map[string]any)["name"] != "Test case" {
		t.Errorf("the test case = %v", tc)
	}
	link := c.answers(tenant, erin, LinkRequirementsTool, map[string]any{"reqKey": "RTR-2", "otherKey": "RTR-1", "typeKey": "verifies"})
	if l := link["link"].(map[string]any); l["phrase"] != "verifies" || l["other"].(map[string]any)["key"] != "RTR-1" {
		t.Errorf("the link = %v", l)
	}
	back := c.answers(tenant, erin, LinkRequirementsTool, map[string]any{"reqKey": "RTR-1", "otherKey": "RTR-2", "typeKey": "relates",
		"direction": "incoming"})
	if l := back["link"].(map[string]any); l["phrase"] != "relates to" || l["direction"] != "incoming" {
		t.Errorf("the incoming link = %v", l)
	}
	if nodes := c.answers(tenant, erin, GetTreeTool, map[string]any{"projectKey": "RTR"})["nodes"].([]any); len(nodes) != 2 {
		t.Errorf("RTR's tree = %v, want RTR-1 and RTR-2", nodes)
	}
	c.refuses(tenant, erin, map[string][]map[string]any{
		LinkRequirementsTool: {
			{"reqKey": "RTR-2", "otherKey": "RTR-1", "typeKey": "verifies"},
			{"reqKey": "RTR-2", "otherKey": "RTR-2", "typeKey": "verifies"},
			{"reqKey": "RTR-2", "otherKey": "RTR-9", "typeKey": "verifies"},
			{"reqKey": "RTR-2", "otherKey": "RTR-1", "typeKey": "blocks"},
		},
		CreateRequirementTool: {
			{"projectKey": "RTR", "title": "No test case", "testCase": map[string]any{"preconditions": nil, "steps": []any{}, "parameters": []any{}}},
			{"projectKey": "RTR", "title": "Foreign type", "typeId": typeID("CP", roleTestCase)},
			{"projectKey": "RTR", "title": "Both ways", "typeId": testType["id"], "testCase": map[string]any{"parameters": []any{},
				"steps": []map[string]any{{"actionMarkdown": "Sign in", "action": nil, "expectedMarkdown": "Signed in"}}}},
		},
	})
	links := c.controlled("GET", "/control/tenants/"+tenant+"/records", nil, http.StatusOK)["requirementLinks"].([]any)
	if len(links) != 2 || links[0].(map[string]any)["source"] != "RTR-2" || links[0].(map[string]any)["type"] != "verifies" {
		t.Errorf("the recorded links = %v", links)
	}
}

// A run starts an execution of a plan the tenant holds, records its cases' results, a case's outcome following from
// its steps, and closes it, in Exciter's shapes; nobody else records in it, and a closed one never changes (#139).
func TestTestExecutionsAnswerInTheShapesExciterLists(t *testing.T) {
	c := newContract(t)
	const tenant = "executions"
	plan := TestPlan{Project: "RTR", Title: "Release", Cases: []string{"RTR-2", "RTR-5"}}
	made := c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "oauth": false, "testPlans": []TestPlan{plan}},
		http.StatusCreated)
	planID := TestPlanID(tenant, plan)
	if plans := made["testPlans"].([]any); len(plans) != 1 || plans[0].(map[string]any)["id"] != planID {
		t.Fatalf("the tenant names its plans as %v, want %s", plans, planID)
	}
	erin, frank := PATPrefix+"erin", PATPrefix+"frank"
	testCase := map[string]any{"preconditions": nil, "parameters": []any{}, "steps": []map[string]any{
		{"actionMarkdown": "Open", "expectedMarkdown": "Opened"}, {"actionMarkdown": "Close", "expectedMarkdown": "Closed"}}}
	c.answers(tenant, erin, CreateRequirementTool, map[string]any{"projectKey": "RTR", "title": "Placeholder"})
	c.answers(tenant, erin, CreateRequirementTool, map[string]any{"projectKey": "RTR", "title": "Open and close",
		"typeId": typeID("RTR", roleTestCase), "testCase": testCase})
	if plans := c.answers(tenant, erin, ListTestPlansTool, map[string]any{"projectKey": "RTR"})["plans"].([]any); len(plans) != 1 {
		t.Errorf("RTR's plans = %v", plans)
	}
	started := c.answers(tenant, erin, StartTestExecutionTool, map[string]any{"projectKey": "RTR", "title": "Release 1.0",
		"planId": planID, "version": "1.0", "testers": []map[string]any{{"subjectType": "user", "userId": userID("erin")}}})
	execution := started["execution"].(map[string]any)
	id := execution["id"].(string)
	if len(execution["cases"].([]any)) != 2 || execution["progress"].(map[string]any)["notRun"] != float64(2) {
		t.Errorf("the execution started = %v", execution)
	}
	stepped := c.answers(tenant, erin, RecordTestResultTool, map[string]any{"executionID": id, "reqKey": "RTR-2", "iteration": 1,
		"steps": []map[string]any{{"step": 1, "outcome": "passed"}, {"step": 2, "outcome": "passed", "note": "Closed at once"}}})
	if run := stepped["case"].(map[string]any)["run"].(map[string]any); run["outcome"] != "passed" || run["source"] != "tester" {
		t.Errorf("a case whose steps all passed = %v, want passed", run)
	}
	c.answers(tenant, erin, RecordTestResultTool, map[string]any{"executionID": id, "reqKey": "RTR-5", "iteration": 1,
		"outcome": "failed", "note": "The seeded case failed."})
	c.refuses(tenant, erin, map[string][]map[string]any{RecordTestResultTool: {
		{"executionID": id, "reqKey": "RTR-2", "iteration": 1, "steps": []map[string]any{{"step": 3, "outcome": "passed"}}},
		{"executionID": id, "reqKey": "RTR-2", "iteration": 1, "outcome": "flaky"},
		{"executionID": id, "reqKey": "RTR-7", "iteration": 1, "outcome": "passed"},
		{"executionID": id, "reqKey": "RTR-2", "iteration": 2, "outcome": "passed"},
	}, StartTestExecutionTool: {
		{"projectKey": "RTR", "title": "No testers", "planId": planID, "testers": []any{}},
		{"projectKey": "CP", "title": "Other project", "planId": planID, "testers": []map[string]any{{"subjectType": "user", "userId": userID("erin")}}},
	}})
	c.refuses(tenant, frank, map[string][]map[string]any{
		RecordTestResultTool:   {{"executionID": id, "reqKey": "RTR-2", "iteration": 1, "outcome": "failed"}},
		CloseTestExecutionTool: {{"executionID": id}},
	})
	closed := c.answers(tenant, erin, CloseTestExecutionTool, map[string]any{"executionID": id, "comment": "Done"})["execution"].(map[string]any)
	if progress := closed["progress"].(map[string]any); closed["state"] != "closed" || progress["passed"] != float64(1) ||
		progress["failed"] != float64(1) {
		t.Errorf("the closed execution = %v", closed)
	}
	c.refuses(tenant, erin, map[string][]map[string]any{
		RecordTestResultTool:   {{"executionID": id, "reqKey": "RTR-2", "iteration": 1, "outcome": "failed"}},
		CloseTestExecutionTool: {{"executionID": id}},
	})
	kept := c.controlled("GET", "/control/tenants/"+tenant+"/records", nil, http.StatusOK)["testExecutions"].([]any)
	if len(kept) != 1 || kept[0].(map[string]any)["state"] != "closed" {
		t.Errorf("the recorded executions = %v", kept)
	}
}
