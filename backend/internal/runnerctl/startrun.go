package runnerctl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// ToolFinder resolves a binding's tool by its name; *tools.Service is the real one.
type ToolFinder interface {
	ByName(ctx context.Context, name string) (tools.Tool, error)
}

// Minter mints a run's access token; *connections.Service is the real one.
type Minter interface {
	Mint(ctx context.Context, req connections.MintRequest) (connections.Token, error)
}

// run is what the protocol needs of a run: whom it acts for and what it runs.
type run struct {
	ID, UserID uuid.UUID
	Status     string
	SpecYAML   string
	Inputs     []byte
	Resume     []byte
	spec       *spec.Spec
}

// binding is a spec binding resolved to a tool, for minting its tokens.
type binding struct {
	toolID uuid.UUID
	scopes []string
}

func (s *Server) loadRun(ctx context.Context, runID uuid.UUID) (*run, error) {
	r := &run{ID: runID}
	err := s.DB.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT r.user_id, r.status, v.spec_yaml, r.inputs, r.resume
			FROM run r JOIN workflow_version v ON v.workflow_id = r.workflow_id AND v.number = r.version
			WHERE r.id = $1`, runID).Scan(&r.UserID, &r.Status, &r.SpecYAML, &r.Inputs, &r.Resume)
	})
	if err != nil {
		return nil, err
	}
	parsed, problems := spec.Parse([]byte(r.SpecYAML))
	if parsed == nil || len(problems) > 0 {
		return nil, fmt.Errorf("the pinned version of run %s no longer reads: %v", runID, problems)
	}
	r.spec = parsed
	return r, nil
}

// sessionExpiry is how long a run's session token may reopen its stream: the run's own timeout, and some grace.
func (r *run) sessionExpiry() time.Time {
	return time.Now().Add(r.spec.Limits.TimeoutDuration() + config.RunnerSessionGrace)
}

// resolve finds each binding's tool, as the runner will call it.
func (s *Server) resolve(ctx context.Context, r *run) ([]*runnerpb.ToolBinding, map[string]binding, error) {
	var out []*runnerpb.ToolBinding
	resolved := map[string]binding{}
	for name, b := range r.spec.Tools {
		tool, err := s.Tools.ByName(ctx, b.Tool)
		if err != nil {
			return nil, nil, fmt.Errorf("binding %s names tool %s: %w", name, b.Tool, err)
		}
		access := b.Access
		if access == "" {
			access = spec.AccessRead
		}
		mcpURL := tool.Resource
		if s.Reach != nil {
			mcpURL = s.Reach(mcpURL)
		}
		out = append(out, &runnerpb.ToolBinding{Binding: name, ToolId: tool.ID.String(), McpUrl: mcpURL,
			Scopes: b.Scopes, Access: access, AllowTools: b.AllowTools})
		resolved[name] = binding{toolID: tool.ID, scopes: b.Scopes}
	}
	return out, resolved, nil
}

// startRun is the run's StartRun: its pinned spec, inputs and bindings, and the answer it resumes with, if any.
func startRun(r *run, bindings []*runnerpb.ToolBinding) *runnerpb.ConnectResponse {
	start := &runnerpb.StartRun{RunId: r.ID.String(), SpecYaml: r.SpecYAML, InputsJson: string(r.Inputs), Tools: bindings}
	if len(r.Resume) > 0 {
		start.Resume, start.ResumeJson = true, string(r.Resume)
	}
	return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_StartRun{StartRun: start}}
}

// grant mints a token for one binding as the run's person; a refusal is said in the grant, never left unanswered.
func (s *Server) grant(ctx context.Context, r *run, bindings map[string]binding, name string) *runnerpb.ConnectResponse {
	g := &runnerpb.TokenGrant{Binding: name}
	b, ok := bindings[name]
	if !ok {
		g.Error = name + " is not one of the run's bindings"
	} else if token, err := s.Mint.Mint(ctx, connections.MintRequest{UserID: r.UserID, ToolID: b.toolID, Scopes: b.scopes, RunID: r.ID.String()}); err != nil {
		g.Error = err.Error()
	} else {
		g.Token = &runnerpb.TokenGrant_AccessToken{AccessToken: token.Value}
		g.ExpiresAt = timestamppb.New(token.ExpiresAt)
	}
	return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_TokenGrant{TokenGrant: g}}
}
