package hooks

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Condition operators.
const (
	OpEquals = "equals"
	OpIn     = "in"
	OpExists = "exists"
)

// Condition holds when the event's value at Path compares as Op says; Path is dotted, such as payload.review.state.
type Condition struct {
	Path  string          `json:"path"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

// Filter is which events start an event trigger: one of its topics, in one of its projects if it names any, where
// every condition holds.
type Filter struct {
	Topics     []string
	Projects   []string
	Conditions []Condition
}

var (
	topicShape   = regexp.MustCompile(`^[a-z_]+(\.[a-z_]+)+$`)
	segmentShape = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// Problems says what is wrong with a filter, keyed by the field as the API names it; none is a filter that works.
func (f Filter) Problems() map[string]string {
	out := map[string]string{}
	switch {
	case len(f.Topics) == 0:
		out["event/topics"] = "Name at least one topic, such as issue.created."
	case len(f.Topics) > config.MaxTriggerTopics:
		out["event/topics"] = fmt.Sprintf("A trigger listens to %d topics at most.", config.MaxTriggerTopics)
	}
	for i, t := range f.Topics {
		if !topicShape.MatchString(t) {
			out[fmt.Sprintf("event/topics/%d", i)] = fmt.Sprintf("%q is not a topic; topics look like review.decided.", t)
		}
	}
	if len(f.Projects) > config.MaxTriggerProjects {
		out["event/projects"] = fmt.Sprintf("A trigger names %d projects at most.", config.MaxTriggerProjects)
	}
	for i, p := range f.Projects {
		if strings.TrimSpace(p) == "" {
			out[fmt.Sprintf("event/projects/%d", i)] = "A project key cannot be empty."
		}
	}
	if len(f.Conditions) > config.MaxTriggerConditions {
		out["event/conditions"] = fmt.Sprintf("A trigger has %d conditions at most.", config.MaxTriggerConditions)
	}
	for i, c := range f.Conditions {
		if problem := c.problem(); problem != "" {
			out[fmt.Sprintf("event/conditions/%d", i)] = problem
		}
	}
	return out
}

func (c Condition) problem() string {
	parts := strings.Split(c.Path, ".")
	if len(parts) > config.MaxConditionPathDepth {
		return fmt.Sprintf("A path goes %d levels deep at most.", config.MaxConditionPathDepth)
	}
	for _, p := range parts {
		if !segmentShape.MatchString(p) {
			return fmt.Sprintf("%q is not a path into the event; write it as payload.review.state.", c.Path)
		}
	}
	var value any
	switch c.Op {
	case OpExists:
		return ""
	case OpEquals:
		if json.Unmarshal(c.Value, &value) != nil || !scalar(value) {
			return "equals compares with one value: a string, a number, true, false or null."
		}
	case OpIn:
		var values []any
		if json.Unmarshal(c.Value, &values) != nil || len(values) == 0 || slices.ContainsFunc(values, func(v any) bool { return !scalar(v) }) {
			return "in compares with a list of values: strings, numbers, true, false or null."
		}
	default:
		return fmt.Sprintf("%q is not a comparison; use equals, in or exists.", c.Op)
	}
	return ""
}

func scalar(v any) bool {
	switch v.(type) {
	case nil, string, float64, bool:
		return true
	}
	return false
}

// Matches says whether an event starts a trigger with this filter.
func (f Filter) Matches(e Event) bool {
	if !slices.Contains(f.Topics, e.Topic) {
		return false
	}
	if len(f.Projects) > 0 && !slices.Contains(f.Projects, Project(e)) {
		return false
	}
	if len(f.Conditions) == 0 {
		return true
	}
	var tree any
	encoded, _ := json.Marshal(e)
	if json.Unmarshal(encoded, &tree) != nil {
		return false
	}
	for _, c := range f.Conditions {
		if !c.holds(tree) {
			return false
		}
	}
	return true
}

func (c Condition) holds(tree any) bool {
	found, ok := at(tree, c.Path)
	switch c.Op {
	case OpExists:
		return ok
	case OpEquals:
		var want any
		return ok && json.Unmarshal(c.Value, &want) == nil && reflect.DeepEqual(found, want)
	case OpIn:
		var want []any
		return ok && json.Unmarshal(c.Value, &want) == nil && slices.ContainsFunc(want, func(v any) bool { return reflect.DeepEqual(found, v) })
	}
	return false
}

// at is the value at a dotted path, and whether the path leads anywhere.
func at(tree any, path string) (any, bool) {
	value := tree
	for _, part := range strings.Split(path, ".") {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		if value, ok = m[part]; !ok {
			return nil, false
		}
	}
	return value, true
}

// Project is the key of the project an event is about, or empty when it names none: Exciter's requirement's project,
// the prefix of Armature's issue key, Stator's page's space.
func Project(e Event) string {
	var tree any
	if json.Unmarshal(e.Payload, &tree) != nil {
		return ""
	}
	var path string
	switch e.Tool {
	case "exciter":
		path = "requirement.project.key"
	case "armature":
		if key, ok := at(tree, "issue.key"); ok {
			if s, ok := key.(string); ok {
				prefix, _, found := strings.Cut(s, "-")
				if found {
					return prefix
				}
			}
		}
		return ""
	case "stator":
		path = "page.space.key"
	default:
		return ""
	}
	if key, ok := at(tree, path); ok {
		if s, ok := key.(string); ok {
			return s
		}
	}
	return ""
}
