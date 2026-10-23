package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// ListRepositoriesTool is Armature's tool that names the repositories a project connects, as the person sees them.
const ListRepositoriesTool = "list_repositories"

// repositories reads, as the person on the context, the repositories of each project the spec's fixed repository
// bindings at tool name. A project the tool refuses shows none; nil is a person Rotor cannot ask as.
func (s *Service) repositories(ctx context.Context, sp *spec.Spec, tool tools.Tool) map[string][]string {
	var projects []string
	for _, b := range sp.Tools {
		if b.Tool == tool.Name && b.Repository != nil && !b.Repository.Templated() && !slices.Contains(projects, b.Repository.Project) {
			projects = append(projects, b.Repository.Project)
		}
	}
	p := auth.PrincipalFrom(ctx)
	if len(projects) == 0 || p == nil {
		return nil
	}
	token, err := s.minter.Mint(ctx, connections.MintRequest{UserID: p.UserID, ToolID: tool.ID, Scopes: tool.Profile().ReadScopes, RunID: "repositories"})
	if err != nil {
		return nil
	}
	defer func() {
		if err := s.minter.Revoke(context.WithoutCancel(ctx), tool.ID, token.Value); err != nil {
			s.log.Warn("cannot revoke the repositories' token", "tool", tool.Name, "error", err)
		}
	}()
	out := map[string][]string{}
	for _, project := range projects {
		var result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Repositories []struct {
					Name string `json:"name"`
				} `json:"repositories"`
			} `json:"structuredContent"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		err := s.call(ctx, tool, token.Value, "tools/call", map[string]any{"name": ListRepositoriesTool,
			"arguments": map[string]any{"projectKey": project}}, &result)
		switch {
		case errors.Is(err, ErrRefusedCall), err == nil && result.IsError:
			out[project] = []string{}
			continue
		case err != nil:
			// A tool that did not answer is not a repository the person lacks; the run's start asks again.
			return nil
		}
		listed := result.StructuredContent.Repositories
		if listed == nil && len(result.Content) > 0 {
			_ = json.Unmarshal([]byte(result.Content[0].Text), &result.StructuredContent)
			listed = result.StructuredContent.Repositories
		}
		names := []string{}
		for _, r := range listed {
			names = append(names, r.Name)
		}
		out[project] = names
	}
	return out
}
