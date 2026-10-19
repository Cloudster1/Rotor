package tools

import (
	"errors"
	"slices"
)

// What Rotor needs of a tool, by the names the tool lists among its capabilities on GET /api/v1/meta (#43).
// Partner OAuth is read from the tool's metadata instead (RFC 9728, RFC 8414), so it has no name here.
const (
	CapabilityMCP      = "mcp@1"
	CapabilityWebhooks = "webhooks@1"
)

// needs says what Rotor cannot do without each capability, in the words of the sentence that says so. Adapted from
// Exciter's checks of Armature (see NOTICE).
var needs = []struct{ capability, feature string }{
	{CapabilityMCP, "act in it and connect people to it"},
	{CapabilityWebhooks, "start workflows from its events"},
}

// Needs lists the capabilities Rotor checks for, for the docs and the tests.
func Needs() []string {
	out := make([]string, 0, len(needs))
	for _, n := range needs {
		out = append(out, n.capability)
	}
	return out
}

// ErrLacks is a tool that does not offer a capability the feature needs; the Missing error says which.
var ErrLacks = errors.New("the tool does not offer what the feature needs")

// Missing is a capability a tool does not list, and what Rotor cannot do without it.
type Missing struct {
	Capability string
	Message    string
}

func (m *Missing) Error() string { return m.Message }
func (m *Missing) Unwrap() error { return ErrLacks }

// Offered is what a tool listed when last asked. Nil is not asked yet, which counts as offering everything.
type Offered []string

// Offers says whether the capability is listed, by its exact name@version.
func (o Offered) Offers(capability string) bool { return o == nil || slices.Contains(o, capability) }

// Lacks is nil when the tool offers the capability, else why the feature that needs it cannot run there.
func (o Offered) Lacks(product, capability string) *Missing {
	if o.Offers(capability) {
		return nil
	}
	tool := title(product)
	for _, n := range needs {
		if n.capability == capability {
			return &Missing{Capability: capability, Message: tool + " does not offer " + capability + ", which Rotor needs to " +
				n.feature + ". Update " + tool + " to a release that offers it, or ask its administrator to."}
		}
	}
	return &Missing{Capability: capability, Message: tool + " does not offer " + capability + ". Update " + tool +
		" to a release that offers it, or ask its administrator to."}
}

// Missing lists every capability Rotor needs that the tool does not offer, in the order of needs.
func (o Offered) Missing(product string) []Missing {
	out := []Missing{}
	for _, n := range needs {
		if m := o.Lacks(product, n.capability); m != nil {
			out = append(out, *m)
		}
	}
	return out
}

// Missing is what Rotor needs that the tool did not list when last asked.
func (t Tool) Missing() []Missing { return Offered(t.Capabilities).Missing(t.Name) }
