//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/connections"
)

// storyCalls are the calls the story workflow makes with no approval, which the trigger's owner agrees to.
var storyCalls = []string{"armature-write.add_comment", "exciter-write.create_requirement", "exciter-write.link_armature_issue",
	"exciter-write.request_reviews", "exciter-write.update_requirement"}

// Records is what a tenant holds of what was written to it: comments, requirements, links and reviews (#129).
func (s *toolStub) Records() map[string]any {
	return s.controlCall(http.MethodGet, "/control/tenants/"+s.name+"/records", nil, http.StatusOK)
}

// mcpCall calls a tool at the stub with a token, as a person's own client would, and answers its structured content.
func mcpCall(t *testing.T, stub *toolStub, token, name string, arguments map[string]any) map[string]any {
	t.Helper()
	call, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
	req, _ := http.NewRequest(http.MethodPost, stub.BaseURL+"/api/v1/mcp", bytes.NewReader(call))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := stub.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var answer struct {
		Result struct {
			IsError           bool           `json:"isError"`
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &answer) != nil || answer.Result.IsError {
		t.Fatalf("calling %s = %s", name, raw)
	}
	return answer.Result.StructuredContent
}

// armatureEvent is a signed version 1 delivery from Armature, which occurred now.
func armatureEvent(t *testing.T, toolID, secret, topic, payload string) string {
	t.Helper()
	id := uuid.NewString()
	body := []byte(fmt.Sprintf(`{"version":1,"id":%q,"topic":%q,"orgId":"0199a000-0000-7000-8000-000000000001","occurredAt":%q,"payload":%s}`,
		id, topic, time.Now().UTC().Format(time.RFC3339), payload))
	if status, answer := deliver(t, toolID, "Armature", secret, body, time.Now(), "1", nil); status != http.StatusAccepted {
		t.Fatalf("delivering %s = %d %v", topic, status, answer)
	}
	return id
}

// analyseTurn is the stub model's answer to the story workflow's agent, in its output schema.
func analyseTurn(output map[string]any) map[string]any {
	return map[string]any{"toolCalls": []map[string]any{{"name": "submit_output", "arguments": output}}}
}

// The story workflow as it ships (#130): questions, its own comments ignored, the answer filed as requirements for
// review, a bug left alone, and every unattended write audited.
func TestAStoryIsClarifiedOnItsTicketAndBecomesRequirements(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	key := "llm-key-" + uuid.NewString()
	llm := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		analyseTurn(map[string]any{"verdict": "questions", "questions": []string{"Which formats must the export offer?"}, "changes": []any{}}),
		analyseTurn(map[string]any{"verdict": "ready", "questions": []any{}, "changes": []map[string]any{
			{"action": "create", "title": "Export as CSV", "statementMarkdown": "The system shall export a project's requirements as CSV."},
			{"action": "create", "title": "Export as PDF", "statementMarkdown": "The system shall export a project's requirements as PDF."}}}),
	}})
	alice := signedIn(t, "alice")
	provider := addProvider(t, alice, llm, key)

	exciterName := "exciter-" + uuid.NewString()[:8]
	exciter := newToolStub(t, map[string]any{"product": exciterName})
	bob, bobID, _ := connected(t, exciter)
	armature := newToolStub(t, map[string]any{"product": "armature", "issues": map[string]any{
		"CP-12": map[string]any{"type": "Story", "summary": "Export requirements", "descriptionMarkdown": "As a reviewer I want to export them.",
			"reporter": "bob", "readers": []string{"bob"}},
		"CP-13": map[string]any{"type": "Bug", "summary": "Export crashes", "reporter": "bob", "readers": []string{"bob"}},
	}})
	armatureID := connectArmature(t, armature, bob)
	secret := "armature_whs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if status, hook := alice.api(http.MethodPut, "/tools/"+armatureID.String()+"/hook", map[string]any{"secret": secret}); status != http.StatusOK {
		t.Fatalf("setting Armature's secret = %d %v", status, hook)
	}

	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "story-to-requirements.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(string(example), "tool: exciter\n", "tool: "+exciterName+"\n")
	source = strings.Replace(source, "limits:", fmt.Sprintf("model: {name: stub-model, provider: %s}\nlimits:", provider["name"]), 1)
	workflowID := createWorkflow(t, bob, source)
	trigger := addTrigger(t, bob, workflowID, map[string]any{"name": "Stories", "kind": "event", "inputs": map[string]any{"exciterProject": "RTR"},
		"allowUnattended": storyCalls, "event": map[string]any{"toolId": armatureID.String(), "topics": []string{"issue.created", "comment.added"},
			"projects": []string{"CP"}, "conditions": []map[string]any{{"path": "payload.internal", "op": "notEquals", "value": true}}}},
		http.StatusCreated)
	triggerID := trigger["id"].(string)
	ran := func(eventID string) string {
		t.Helper()
		outcome, reason, run := firing(t, h, triggerID, eventID)
		if outcome != "started" || run == nil {
			t.Fatalf("the firing ended %q (%q)", outcome, reason)
		}
		done := waitRun(t, bob, run.String(), "succeeded", "failed")
		if done["status"] != "succeeded" {
			t.Fatalf("the run ended %v: %v", done["status"], done["error"])
		}
		t.Logf("run %s answered %v", run, done["output"])
		return run.String()
	}
	skippedAsOwn := func(eventID, what string) {
		t.Helper()
		if outcome, reason, _ := firing(t, h, triggerID, eventID); outcome != "skipped" || !strings.Contains(reason, "own write") {
			t.Errorf("%s ended %q (%q), want skipped as the workflow's own write", what, outcome, reason)
		}
	}

	// A new story: questions on its ticket, and that comment does not start the workflow again.
	asked := ran(armatureEvent(t, armatureID.String(), secret, "issue.created",
		`{"issueId":"0b6e2f7a-3c41-4d8e-9f52-1a7c9e4b2d60","key":"CP-12","summary":"Export requirements","actorId":"5d3a8c19-7e2b-4f60-a1d4-8b9c0e6f2a37"}`))
	comments, _ := armature.Records()["comments"].(map[string]any)["CP-12"].([]any)
	if len(comments) != 1 || !strings.Contains(stringOf(comments[0]), "Questions from Rotor") || !strings.Contains(stringOf(comments[0]), "Which formats") {
		t.Fatalf("after the first run CP-12 has the comments %v", comments)
	}
	questions, _ := comments[0].(map[string]any)["id"].(string)
	skippedAsOwn(armatureEvent(t, armatureID.String(), secret, "comment.added",
		fmt.Sprintf(`{"issueId":"i","key":"CP-12","commentId":%q,"actorId":"a","internal":false,"mentions":null}`, questions)), "the questions' own event")

	// The reporter answers with their own client: the workflow files the requirements and says so on the ticket.
	token, err := controlPlane(t, h, nil).Mint(ctx, connections.MintRequest{UserID: bobID, ToolID: armatureID, Scopes: []string{"comment.write"}, RunID: "bob-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatal(err)
	}
	answer := mcpCall(t, armature, token.Value, "add_comment", map[string]any{"issueKey": "CP-12", "text": "CSV and PDF."})
	answerID, _ := answer["comment"].(map[string]any)["id"].(string)
	filed := ran(armatureEvent(t, armatureID.String(), secret, "comment.added",
		fmt.Sprintf(`{"issueId":"i","key":"CP-12","commentId":%q,"actorId":"a","internal":false,"mentions":null}`, answerID)))
	records := exciter.Records()
	requirements, _ := records["requirements"].([]any)
	if len(requirements) != 2 || !strings.Contains(stringOf(requirements), "Export as CSV") || !strings.Contains(stringOf(requirements), "Export as PDF") {
		t.Fatalf("Exciter holds %v", requirements)
	}
	links, _ := records["armatureLinks"].(map[string]any)
	if len(links) != 2 || !strings.Contains(stringOf(links), "CP-12") || !strings.Contains(stringOf(links), "implements") {
		t.Errorf("the requirements are linked as %v", links)
	}
	if reviews, _ := records["reviews"].([]any); len(reviews) != 2 {
		t.Errorf("reviews were asked for %v", reviews)
	}
	comments, _ = armature.Records()["comments"].(map[string]any)["CP-12"].([]any)
	if len(comments) != 3 || !strings.Contains(stringOf(comments[2]), "Requirements from Rotor") || !strings.Contains(stringOf(comments[2]), "RTR-1") {
		t.Fatalf("after the answer CP-12 has the comments %v", comments)
	}
	handover, _ := comments[2].(map[string]any)["id"].(string)
	skippedAsOwn(armatureEvent(t, armatureID.String(), secret, "comment.added",
		fmt.Sprintf(`{"issueId":"i","key":"CP-12","commentId":%q,"actorId":"a","internal":false,"mentions":null}`, handover)), "the handover's own event")

	// A bug starts a run that leaves it alone.
	ran(armatureEvent(t, armatureID.String(), secret, "issue.created", `{"issueId":"i2","key":"CP-13","summary":"Export crashes","actorId":"a"}`))
	if bugs, _ := armature.Records()["comments"].(map[string]any)["CP-13"].([]any); len(bugs) != 0 {
		t.Errorf("the bug got comments %v", bugs)
	}

	// Each write was unattended, audited with the trigger and what it wrote, and kept in the run's ledger.
	var audited, withSubjects int
	if err := h.super.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE jsonb_array_length(COALESCE(data->'subjects', '[]'::jsonb)) > 0)
		FROM audit_log WHERE action = 'trigger.unattended_write' AND target_id = $1`, triggerID).Scan(&audited, &withSubjects); err != nil {
		t.Fatal(err)
	}
	// One questions comment; two requirements created and linked, one review request and the handover comment.
	if audited != 7 || withSubjects < 4 {
		t.Errorf("the audit log holds %d unattended writes, %d naming what they wrote; want 7, with the comments and requirements named", audited, withSubjects)
	}
	var ledger int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM run_write WHERE run_id IN ($1, $2)`, asked, filed).Scan(&ledger); err != nil || ledger == 0 {
		t.Errorf("the runs' ledger holds %d writes (%v)", ledger, err)
	}
}

// connectArmature adds an Armature tenant, allows Rotor there as its administrator would, and connects bob to it.
func connectArmature(t *testing.T, stub *toolStub, bob *browser) uuid.UUID {
	t.Helper()
	alice := signedIn(t, "alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	if status != http.StatusCreated {
		t.Fatalf("adding Armature = %d %v", status, answer)
	}
	id, _ := toolOf(t, answer)["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+id, nil) })
	stub.ApproveClients()
	if landed := bob.landing(t, bob.consent(t, id, "bob", "allow")); !strings.Contains(landed, "connected=") {
		t.Fatalf("connecting to Armature landed on %q", landed)
	}
	return uuid.MustParse(id)
}
