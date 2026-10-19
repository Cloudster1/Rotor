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
		{event("armature", "issue.created", `{"issue":{"key":"CP-12"}}`), "get_issue", map[string]any{"issueKey": "CP-12"}, true},
		{event("armature", "sla.breached", `{"issueKey":"CP-13"}`), "get_issue", map[string]any{"issueKey": "CP-13"}, true},
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
