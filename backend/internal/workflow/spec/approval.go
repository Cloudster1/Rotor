package spec

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// ReservedFormFields are the keys an approval's output carries besides its form's values, so no field may take them.
var ReservedFormFields = []string{"approved", "comment", "decidedBy"}

// approvalSense checks an approval node: an expiry within the longest wait, and a form an approver can fill.
func approvalSense(n Node, base string, known func(string) bool) []Problem {
	var out []Problem
	add := func(path, code, format string, args ...any) {
		out = append(out, Problem{Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
	}
	if d, err := time.ParseDuration(n.ExpiresIn); err == nil && d > config.MaxApprovalExpiry {
		add(base+"/expiresIn", CodeApprovalExpiry, "An approval may wait %s at most, not %s.", hours(config.MaxApprovalExpiry), n.ExpiresIn)
	}
	if n.Form == nil {
		return out
	}
	properties, ok := FormFields(n.Form.Schema)
	if !ok {
		add(base+"/form/schema", CodeApprovalForm,
			"The form's schema must be a JSON Schema of type object with at least one field under properties.")
		return out
	}
	for _, name := range sortedKeys(properties) {
		if slices.Contains(ReservedFormFields, name) {
			add(base+"/form/schema/properties/"+escape(name), CodeApprovalForm,
				"%s is a name the approval's output keeps for itself; call the field something else.", name)
		}
	}
	var values map[string]json.RawMessage
	_ = json.Unmarshal(n.Form.Values, &values)
	for _, name := range sortedKeys(values) {
		if _, ok := properties[name]; !ok {
			add(base+"/form/values/"+escape(name), CodeApprovalForm,
				"%s is not a field of the form; add it under the schema's properties or remove it.", name)
		}
	}
	for _, ref := range argumentRefs(n.Form.Values, base+"/form/values") {
		if !known(ref.node) {
			add(ref.path, CodeUnknownNode, "The value reads %s, which is not a node.", ref.node)
		}
	}
	return out
}

// FormFields is a form schema's properties, or false when it is no object schema with fields that compiles.
func FormFields(schema json.RawMessage) (map[string]json.RawMessage, bool) {
	var doc struct {
		Type       any                        `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &doc) != nil || doc.Type != "object" || len(doc.Properties) == 0 {
		return nil, false
	}
	if _, err := compileSchema(schema); err != nil {
		return nil, false
	}
	return doc.Properties, true
}

// reviewDeciders refuses a binding that allows a call deciding a review: that stays a person's act in the tool.
func reviewDeciders(s *Spec) []Problem {
	var out []Problem
	for _, name := range sortedKeys(s.Tools) {
		for j, call := range s.Tools[name].AllowTools {
			if config.ReviewDecidingCalls.MatchString(call) {
				out = append(out, Problem{Path: fmt.Sprintf("/tools/%s/allowTools/%d", escape(name), j), Code: CodeDecidesReview,
					Message: fmt.Sprintf("%s would decide a review in %s. A person decides a review in the tool itself, so no workflow may call it.",
						call, s.Tools[name].Tool)})
			}
		}
	}
	return out
}

// unattendedOutsideAllow refuses an unattended call the binding does not allow, which no run could make anyway.
func unattendedOutsideAllow(s *Spec) []Problem {
	var out []Problem
	for _, name := range sortedKeys(s.Tools) {
		b := s.Tools[name]
		for j, call := range b.Unattended {
			if !slices.Contains(b.AllowTools, call) {
				out = append(out, Problem{Path: fmt.Sprintf("/tools/%s/unattended/%d", escape(name), j), Code: CodeUnattendedCall,
					Message: fmt.Sprintf("%s is not in the allowTools of %s, so it cannot be unattended; add it there or take it out.", call, name)})
			}
		}
	}
	return out
}

func hours(d time.Duration) string { return fmt.Sprintf("%dh", int(d.Hours())) }

// ExpiresAfter is how long the node's approval waits: its own expiresIn, or def.
func (n Node) ExpiresAfter(def time.Duration) time.Duration {
	if d, err := time.ParseDuration(n.ExpiresIn); err == nil && d > 0 {
		return d
	}
	return def
}
