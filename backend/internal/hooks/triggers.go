package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/triggers"
)

// CodeEvent marks a problem with an event trigger's tool or filters.
const CodeEvent = "event"

// Connections is the person's own connections; *connections.Service is the real one.
type Connections interface {
	List(ctx context.Context) ([]connections.Connection, error)
}

// Reader reads a subject at a tool as the person on the context; *catalog.Service is the real one.
type Reader interface {
	Read(ctx context.Context, toolID uuid.UUID, name string, arguments map[string]any) (bool, error)
}

// CheckEvent is the triggers' check of an event trigger, as the person on the context: filters that read, a tool
// Rotor takes events from, and their own live connection to it, since each event's subject is read as them.
func (s *Service) CheckEvent(conns Connections) func(ctx context.Context, in triggers.Input) []triggers.Problem {
	return func(ctx context.Context, in triggers.Input) []triggers.Problem {
		var out []triggers.Problem
		add := func(field, code, message string) {
			out = append(out, triggers.Problem{Field: field, Code: code, Message: message})
		}
		var conditions []Condition
		if len(in.Conditions) > 0 && json.Unmarshal(in.Conditions, &conditions) != nil {
			add("event/conditions", CodeEvent, "Conditions are a list of {path, op, value}.")
		}
		problems := Filter{Topics: in.Topics, Projects: in.Projects, Conditions: conditions}.Problems()
		fields := make([]string, 0, len(problems))
		for field := range problems {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			add(field, CodeEvent, problems[field])
		}
		if in.ToolID == uuid.Nil {
			add("event/toolId", CodeEvent, "Pick the tool whose events start the trigger.")
			return out
		}
		h, err := s.Get(ctx, in.ToolID)
		switch {
		case errors.Is(err, ErrNotFound):
			add("event/toolId", triggers.ReasonToolRemoved, "Rotor knows no such tool. Pick one of the tools under Settings.")
			return out
		case errors.Is(err, ErrNotProduct):
			add("event/toolId", CodeEvent, "Rotor takes events only from Armature, Exciter and Stator.")
			return out
		case err != nil:
			add("event/toolId", CodeEvent, "Rotor could not read the tool; try again.")
			return out
		}
		// A person making or turning on a trigger hears of a missing secret; a firing's event has arrived already.
		if !in.Firing && h.SecretSetAt == nil {
			add("event/toolId", CodeEvent, "No administrator has given Rotor this tool's webhook secret yet, so its events cannot arrive. "+
				"Ask one to set it up under Settings, Tools.")
		}
		listed, err := conns.List(ctx)
		if err != nil {
			add("event/toolId", CodeEvent, "Rotor could not read your connections; try again.")
			return out
		}
		for _, c := range listed {
			if c.ToolID == in.ToolID && c.Status == connections.StatusConnected {
				return out
			}
		}
		add("event/toolId", triggers.ReasonConnectionLost, "Connect your own account to the tool first: Rotor reads each event's "+
			"requirement, issue or page as you before a run starts.")
		return out
	}
}

// Probe tells whether the trigger's owner, on the context, may read the subject of the event a firing carries: a
// tool's payload is read with the rights of the administrator who added the webhook, not the owner's.
func Probe(reader Reader) func(ctx context.Context, t triggers.Trigger, inputs map[string]any) (bool, error) {
	return func(ctx context.Context, t triggers.Trigger, inputs map[string]any) (bool, error) {
		var e Event
		encoded, _ := json.Marshal(inputs["event"])
		if json.Unmarshal(encoded, &e) != nil || t.ToolID == nil {
			return false, nil
		}
		name, arguments, ok := Subject(e)
		if !ok {
			return false, nil
		}
		return reader.Read(ctx, *t.ToolID, name, arguments)
	}
}

// Subject is the MCP call that reads what an event is about: Exciter's requirement, Armature's issue or Stator's
// page; an event about nothing Rotor can read this way has none.
func Subject(e Event) (string, map[string]any, bool) {
	var tree any
	if json.Unmarshal(e.Payload, &tree) != nil {
		return "", nil, false
	}
	text := func(paths ...string) string {
		for _, p := range paths {
			if v, ok := at(tree, p); ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
		return ""
	}
	switch e.Tool {
	case "exciter":
		if key := text("requirement.key"); key != "" {
			return "get_requirement", map[string]any{"reqKey": key}, true
		}
	case "armature":
		if key := text("issue.key", "issueKey"); key != "" {
			return "get_issue", map[string]any{"issueKey": key}, true
		}
	case "stator":
		if id := text("page.id"); id != "" {
			return "get_page", map[string]any{"pageID": id}, true
		}
	}
	return "", nil, false
}
