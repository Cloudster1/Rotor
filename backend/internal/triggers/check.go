package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/runs"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
	"github.com/cloudster1/rotor/backend/internal/workflows"
)

// Reasons Rotor turns a trigger off, as docs/api-contract-m6.md names them.
const (
	ReasonOwnerSwitchedOff = "owner_switched_off"
	ReasonConnectionLost   = "connection_lost"
	ReasonToolRemoved      = "tool_removed"
	ReasonWorkflowArchived = "workflow_archived"
	ReasonWorkflowHidden   = "workflow_hidden"
	ReasonUnattendedWrite  = "unattended_write"
	ReasonInputsRefused    = "inputs_refused"
	// ReasonUnattendedWidened is a version naming an unattended call the owner did not agree to (#128).
	ReasonUnattendedWidened = "unattended_widened"
	// ReasonUnattendedForbidden is an unattended call while administrators forbid them (#128).
	ReasonUnattendedForbidden = "unattended_forbidden"
)

// reasonSentences say each reason to the trigger's owner.
var reasonSentences = map[string]string{
	ReasonOwnerSwitchedOff: "The owner's account is switched off, so the trigger no longer starts runs.",
	ReasonConnectionLost:   "The workflow uses a tool you are no longer connected to. Connect it again, then turn the trigger on.",
	ReasonToolRemoved:      "The tool whose events started this trigger was removed from Rotor. Pick another tool, then turn it on.",
	ReasonWorkflowArchived: "The workflow was archived, so it starts no runs.",
	ReasonWorkflowHidden:   "You no longer see the workflow, so the trigger cannot start it. Ask its owner to share it again.",
	ReasonUnattendedWrite:  "The workflow may now write without an approval first. Put an approval node before each write, then turn the trigger on.",
	ReasonInputsRefused:    "The workflow's inputs no longer take the trigger's inputs. Change them to fit, then turn the trigger on.",
	ReasonUnattendedWidened: "The workflow now names calls that write without an approval which you did not agree to. " +
		"Agree to them in the trigger, then turn it on.",
	ReasonUnattendedForbidden: "An administrator forbade writes without an approval, which this workflow makes. " +
		"Put an approval node before each write, or ask an administrator to allow them again.",
}

// Sentence says a reason to the trigger's owner.
func Sentence(reason string) string { return reasonSentences[reason] }

// Problem is something wrong with a trigger, at a field such as schedule/cron or inputs/<pointer>.
type Problem struct {
	Field   string
	Code    string
	Message string
}

// Refused is a trigger Rotor will not make or turn on, with every problem found.
type Refused struct{ Problems []Problem }

func (e *Refused) Error() string {
	if len(e.Problems) == 0 {
		return "the trigger is refused"
	}
	return e.Problems[0].Message
}

// Problem codes of the checks below.
const (
	CodeSchedule = "schedule"
	CodeName     = "name"
	CodeKind     = "kind"
	// CodeUnattendedNotAllowed is a trigger whose owner did not agree to every unattended call of the workflow.
	CodeUnattendedNotAllowed = "unattended_not_allowed"
)

// eventPlaceholder stands in for an event when an event trigger's inputs are checked, before any event arrives.
var eventPlaceholder = map[string]any{"tool": "", "topic": "", "id": "", "occurredAt": "", "payload": map[string]any{}}

// checked is what a trigger runs: the workflow as its owner sees it, its current version, and that version read.
type checked struct {
	workflow workflows.Workflow
	version  workflows.Version
	spec     *spec.Spec
}

// check is a trigger checked as the person on the context: a workflow they see and have every connection for,
// whose every write waits for an approval, inputs that fit, and a schedule or filters that read.
func (s *Service) check(ctx context.Context, workflowID uuid.UUID, in Input) (checked, error) {
	var problems []Problem
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > config.MaxTriggerName {
		problems = append(problems, Problem{Field: "name", Code: CodeName, Message: "Give the trigger a name of at most 200 characters."})
	}
	switch in.Kind {
	case KindSchedule:
		if _, err := ParseSchedule(in.Cron, in.TimeZone); err != nil {
			var p *ScheduleProblem
			errors.As(err, &p)
			problems = append(problems, Problem{Field: "schedule/" + p.Field, Code: CodeSchedule, Message: p.Message})
		}
	case KindEvent:
		if s.CheckEvent == nil {
			problems = append(problems, Problem{Field: "kind", Code: CodeKind, Message: "Triggers from tool events are not set up on this installation yet."})
		} else {
			problems = append(problems, s.CheckEvent(ctx, in)...)
		}
	default:
		problems = append(problems, Problem{Field: "kind", Code: CodeKind, Message: "Say whether the trigger is a schedule or an event."})
	}
	w, version, err := s.workflows.Get(ctx, workflowID)
	if err != nil {
		return checked{}, err
	}
	if w.ArchivedAt != nil {
		return checked{}, runs.ErrArchived
	}
	parsed, specProblems := spec.Parse([]byte(version.SpecYAML))
	if parsed == nil || len(specProblems) > 0 {
		return checked{}, &Refused{Problems: append(problems, Problem{Field: "workflow", Code: ReasonInputsRefused,
			Message: "The workflow's current version no longer reads; its owner must save one that does."})}
	}
	if err := s.runs.Connected(ctx, parsed); err != nil {
		return checked{}, err
	}
	catalog, err := s.catalogs.For(ctx, parsed)
	if err != nil {
		return checked{}, err
	}
	if p, ok, err := s.unattendedProblem(ctx, parsed, in); err != nil {
		return checked{}, err
	} else if ok {
		problems = append(problems, p)
	}
	writes, _ := parsed.UnattendedWrites(catalog)
	for _, p := range writes {
		problems = append(problems, Problem{Field: "workflow" + p.Path, Code: ReasonUnattendedWrite, Message: p.Message})
	}
	for _, p := range parsed.CheckInputs(runInputs(in.Kind, in.Inputs, nil)) {
		problems = append(problems, Problem{Field: "inputs" + p.Path, Code: ReasonInputsRefused, Message: p.Message})
	}
	if len(problems) > 0 {
		return checked{}, &Refused{Problems: problems}
	}
	return checked{workflow: w, version: version, spec: parsed}, nil
}

// runInputs is what a run of the trigger starts with: its own inputs, and the event for an event trigger.
func runInputs(kind string, own json.RawMessage, fired map[string]any) []byte {
	inputs := map[string]any{}
	_ = json.Unmarshal(own, &inputs)
	for k, v := range fired {
		inputs[k] = v
	}
	if _, ok := inputs["event"]; kind == KindEvent && !ok {
		inputs["event"] = eventPlaceholder
	}
	out, _ := json.Marshal(inputs)
	return out
}

// unattendedProblem is what keeps the workflow's unattended calls from running: administrators forbid them, or the
// owner did not agree to each. A firing calls a call not agreed to a wider version, which turns the trigger off.
func (s *Service) unattendedProblem(ctx context.Context, parsed *spec.Spec, in Input) (Problem, bool, error) {
	needs := parsed.UnattendedCalls()
	if len(needs) == 0 {
		return Problem{}, false, nil
	}
	allowed, err := s.UnattendedAllowed(ctx)
	if err != nil {
		return Problem{}, false, err
	}
	if !allowed {
		return Problem{Field: "allowUnattended", Code: ReasonUnattendedForbidden, Message: Sentence(ReasonUnattendedForbidden)}, true, nil
	}
	var missing []string
	for _, call := range needs {
		if !slices.Contains(in.AllowUnattended, call) {
			missing = append(missing, call)
		}
	}
	if len(missing) == 0 {
		return Problem{}, false, nil
	}
	code := CodeUnattendedNotAllowed
	if in.Firing {
		code = ReasonUnattendedWidened
	}
	return Problem{Field: "allowUnattended", Code: code, Message: fmt.Sprintf(
		"The workflow writes through %s with no approval before it; agree to that in allowUnattended, or ask its author for an approval node.",
		strings.Join(missing, ", "))}, true, nil
}
