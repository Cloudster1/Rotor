package catalog

import (
	"context"

	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// specCatalog answers spec.CheckAgainst from what was read before the check, by the tool names a spec binds.
type specCatalog map[string]spec.ToolInfo

func (c specCatalog) Tool(name string) (spec.ToolInfo, bool) {
	info, ok := c[name]
	return info, ok
}

// For reads what a spec's bindings need to know about their tools, as the person on the context sees them. A
// tool the person is not connected to is known by its scopes alone, so its calls and repositories wait for them.
func (s *Service) For(ctx context.Context, sp *spec.Spec) (spec.Catalog, error) {
	known, err := s.tools.List(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]tools.Tool{}
	for _, t := range known {
		if prior, taken := byName[t.Name]; !taken || (prior.Status != tools.StatusReady && t.Status == tools.StatusReady) {
			byName[t.Name] = t
		}
	}
	out := specCatalog{}
	for _, binding := range sp.Tools {
		t, ok := byName[binding.Tool]
		if !ok {
			continue
		}
		if _, done := out[binding.Tool]; done {
			continue
		}
		info := spec.ToolInfo{Scopes: t.ScopesSupported, Git: t.OffersGit()}
		if info.Git {
			info.Repositories = s.repositories(ctx, sp, t)
		}
		if c, err := s.Get(ctx, t.ID); err == nil {
			info.Calls = make(map[string]spec.CallInfo, len(c.Tools))
			for _, call := range c.Tools {
				info.Calls[call.Name] = spec.CallInfo{ReadOnly: call.ReadOnly, Destructive: call.Destructive}
			}
		}
		out[binding.Tool] = info
	}
	return out, nil
}

// freshKey marks a context whose catalog reads skip the cache (#90).
type freshKey struct{}

// Fresh is For read from the tools themselves, as a run starts: the cache may lag a tool that changed a call without
// a new build, and Valkey is not trusted to say what writes.
func (s *Service) Fresh(ctx context.Context, sp *spec.Spec) (spec.Catalog, error) {
	return s.For(context.WithValue(ctx, freshKey{}, true), sp)
}
