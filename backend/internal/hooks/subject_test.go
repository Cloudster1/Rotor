package hooks

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/triggers"
)

func TestEachToolsEventNamesTheCallThatReadsItsSubject(t *testing.T) {
	for _, c := range []struct {
		e    Event
		name string
		args map[string]any
		ok   bool
	}{
		{event("exciter", "review.decided", `{"requirement":{"key":"RTR-1"}}`), "get_requirement", map[string]any{"reqKey": "RTR-1"}, true},
		{event("armature", "issue.created", armatureIssueCreated), "get_issue", map[string]any{"issueKey": "CP-12"}, true},
		{event("armature", "sla.breached", armatureSLABreached), "get_issue", map[string]any{"issueKey": "CP-13"}, true},
		{event("stator", "page.published", `{"page":{"id":"p-1"}}`), "get_page", map[string]any{"pageID": "p-1"}, true},
		{event("exciter", "baseline.taken", `{"baseline":{"id":"b"}}`), "", nil, false},
		{event("jira", "issue.created", `{"issue":{"key":"X-1"}}`), "", nil, false},
	} {
		name, args, ok := Subject(c.e)
		if name != c.name || ok != c.ok || (ok && !reflect.DeepEqual(args, c.args)) {
			t.Errorf("%s %s: %s %v %v", c.e.Tool, c.e.Topic, name, args, ok)
		}
	}
}

// Payloads as Armature's docs/webhooks.md describes them.
const (
	armatureIssue = `"issueId":"0b6e2f7a-3c41-4d8e-9f52-1a7c9e4b2d60"`
	armatureActor = `"actorId":"5d3a8c19-7e2b-4f60-a1d4-8b9c0e6f2a37"`

	armatureIssueCreated = `{` + armatureIssue + `,"key":"CP-12","summary":"Login fails",` + armatureActor + `}`
	armatureIssueUpdated = `{` + armatureIssue + `,"key":"CP-12",` + armatureActor +
		`,"changes":[{"field":"summary","from":"Login fails","to":"Login fails on Safari"}]}`
	armatureIssueMoved = `{` + armatureIssue + `,"key":"OPS-4",` + armatureActor +
		`,"changes":[{"field":"project","from":"CP","to":"OPS"}],"movedFrom":"CP-12"}`
	armatureIssueTransitioned = `{` + armatureIssue + `,"key":"CP-12","transition":"Start progress","fromStatus":"Open",` +
		`"toStatus":"In Progress","fromStatusCategory":"todo","toStatusCategory":"in_progress",` + armatureActor + `}`
	armatureCommentAdded = `{` + armatureIssue + `,"key":"CP-12","commentId":"c2f1e0d9-8b7a-4c6d-9e5f-4a3b2c1d0e9f",` +
		armatureActor + `,"internal":false,"mentions":null}`
	armatureSLABreached  = `{"issueKey":"CP-13","timerId":"7a9b8c6d-5e4f-4a3b-9c2d-1e0f9a8b7c6d","metric":"first_response"}`
	armatureCommitLinked = `{"issueKey":"CP-14","repositoryId":"3e4f5a6b-7c8d-4e9f-a0b1-c2d3e4f5a6b7","sha":"9fceb02"}`
)

func TestEachArmatureTopicNamesItsProjectAndIssue(t *testing.T) {
	for _, c := range []struct {
		topic, payload, project, issue string
	}{
		{"issue.created", armatureIssueCreated, "CP", "CP-12"},
		{"issue.updated", armatureIssueUpdated, "CP", "CP-12"},
		{"issue.updated", armatureIssueMoved, "OPS", "OPS-4"},
		{"issue.transitioned", armatureIssueTransitioned, "CP", "CP-12"},
		{"comment.added", armatureCommentAdded, "CP", "CP-12"},
		{"sla.breached", armatureSLABreached, "CP", "CP-13"},
		{"vcs.commit.linked", armatureCommitLinked, "CP", "CP-14"},
		{"issue.created", `{"issue":{"key":"CP-15"}}`, "CP", "CP-15"},
		{"issue.created", `{"key":"nodash"}`, "", "nodash"},
		{"ci.run.recorded", `{"repositoryId":"r","externalId":"x","sha":"s","status":"success"}`, "", ""},
	} {
		e := event("armature", c.topic, c.payload)
		if got := Project(e); got != c.project {
			t.Errorf("%s %s: project %q, want %q", c.topic, c.payload, got, c.project)
		}
		name, args, ok := Subject(e)
		if c.issue == "" {
			if ok {
				t.Errorf("%s: %s %v for an event about no issue", c.topic, name, args)
			}
			continue
		}
		if name != "get_issue" || !ok || args["issueKey"] != c.issue {
			t.Errorf("%s %s: %s %v %v, want issue %s", c.topic, c.payload, name, args, ok, c.issue)
		}
	}
}

type fakeReader struct {
	readable bool
	err      error
	asked    string
}

func (f *fakeReader) Read(_ context.Context, _ uuid.UUID, name string, _ map[string]any) (bool, error) {
	f.asked = name
	return f.readable, f.err
}

func TestTheProbeAsksTheToolAndSkipsWhatItCannotAsk(t *testing.T) {
	tool := uuid.New()
	trigger := triggers.Trigger{Kind: triggers.KindEvent, ToolID: &tool}
	inputs := map[string]any{"event": map[string]any{"tool": "exciter", "topic": "requirement.created", "id": "e",
		"payload": map[string]any{"requirement": map[string]any{"key": "RTR-1"}}}}
	reader := &fakeReader{readable: true}
	if ok, err := Probe(reader)(context.Background(), trigger, inputs); !ok || err != nil || reader.asked != "get_requirement" {
		t.Errorf("a readable subject = %v %v, asked %q", ok, err, reader.asked)
	}
	reader = &fakeReader{err: errors.New("down")}
	if _, err := Probe(reader)(context.Background(), trigger, inputs); err == nil {
		t.Error("a tool that did not answer is no passing failure")
	}
	reader = &fakeReader{readable: true}
	if ok, _ := Probe(reader)(context.Background(), trigger, map[string]any{}); ok || reader.asked != "" {
		t.Error("a firing without an event was let through")
	}
}
