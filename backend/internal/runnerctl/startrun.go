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
	"github.com/cloudster1/rotor/backend/internal/providers"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// ToolFinder resolves a binding's tool by its name; *tools.Service is the real one.
type ToolFinder interface {
	ByName(ctx context.Context, name string) (tools.Tool, error)
}

// Minter mints a run's access token and hands it over wrapped; *connections.Service is the real one.
type Minter interface {
	Deliver(ctx context.Context, req connections.MintRequest) (connections.Delivery, error)
}

// Wrapper wraps a secret for a sandbox to unwrap once; *vault.Client is the real one.
type Wrapper interface {
	Wrap(ctx context.Context, data map[string]any, ttl time.Duration) (string, error)
}

// Models resolves a workflow's model and hands out its provider's key; *providers.Service is the real one.
type Models interface {
	Resolve(ctx context.Context, wanted *spec.Model) (providers.Resolution, error)
	Key(ctx context.Context, id uuid.UUID) (string, error)
}

// run is what the protocol needs of a run: whom it acts for and what it runs.
type run struct {
	ID, UserID uuid.UUID
	Status     string
	SpecYAML   string
	Inputs     []byte
	Resume     []byte
	spec       *spec.Spec
	// model is the provider and model the run's agents use; nil for a workflow without agents.
	model *providers.Resolution
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

// usesModel says whether any node of the workflow calls a language model.
func usesModel(s *spec.Spec) bool {
	for _, n := range s.Nodes {
		if n.Type == spec.TypeAgent {
			return true
		}
	}
	return false
}

// resolveModel finds the run's provider and model; a workflow whose model is not allowed starts without one, and its
// agents fail saying so.
func (s *Server) resolveModel(ctx context.Context, r *run) {
	if s.Models == nil || !usesModel(r.spec) {
		return
	}
	resolved, err := s.Models.Resolve(ctx, r.spec.Model)
	if err != nil {
		s.Log.Warn("a run starts without a model", "run", r.ID, "error", err)
		return
	}
	r.model = &resolved
}

// modelConfig is the run's model as the runner builds it, at an address a sandbox reaches; the key comes apart.
func (s *Server) modelConfig(r *run) *runnerpb.ModelConfig {
	if r.model == nil {
		return nil
	}
	base := r.model.Provider.BaseURL
	if s.Reach != nil && base != "" {
		base = s.Reach(base)
	}
	return &runnerpb.ModelConfig{ProviderId: r.model.Provider.ID.String(), Kind: r.model.Provider.Kind, Model: r.model.Model,
		BaseUrl: base, ApiVersion: r.model.Provider.APIVersion}
}

// secret is the run's provider key, wrapped, sent after every Hello since a grant is never stored to be resent.
func (s *Server) secret(ctx context.Context, r *run) (*runnerpb.ConnectResponse, error) {
	key, err := s.Models.Key(ctx, r.model.Provider.ID)
	if err != nil {
		return nil, fmt.Errorf("read the key of provider %s: %w", r.model.Provider.Name, err)
	}
	wrapped, err := s.Wrap.Wrap(ctx, map[string]any{"api_key": key}, config.WrapTTL)
	if err != nil {
		return nil, fmt.Errorf("wrap the key of provider %s: %w", r.model.Provider.Name, err)
	}
	return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_SecretGrant{SecretGrant: &runnerpb.SecretGrant{
		ProviderId: r.model.Provider.ID.String(), Secret: &runnerpb.SecretGrant_WrappingToken{WrappingToken: wrapped}}}}, nil
}

// startRun is the run's StartRun: its pinned spec, inputs, bindings and model, and the answer it resumes with, if any.
func startRun(r *run, bindings []*runnerpb.ToolBinding, model *runnerpb.ModelConfig) *runnerpb.ConnectResponse {
	start := &runnerpb.StartRun{RunId: r.ID.String(), SpecYaml: r.SpecYAML, InputsJson: string(r.Inputs), Tools: bindings, Model: model}
	if len(r.Resume) > 0 {
		start.Resume, start.ResumeJson = true, string(r.Resume)
	}
	return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_StartRun{StartRun: start}}
}

// grant mints a token for one binding as the run's person and hands it over wrapped; a refusal is said in the grant, never left unanswered.
func (s *Server) grant(ctx context.Context, r *run, bindings map[string]binding, name string) *runnerpb.ConnectResponse {
	g := &runnerpb.TokenGrant{Binding: name}
	b, ok := bindings[name]
	if !ok {
		g.Error = name + " is not one of the run's bindings"
	} else if delivered, err := s.Mint.Deliver(ctx, connections.MintRequest{UserID: r.UserID, ToolID: b.toolID, Scopes: b.scopes, RunID: r.ID.String()}); err != nil {
		g.Error = err.Error()
	} else {
		g.Token = &runnerpb.TokenGrant_WrappingToken{WrappingToken: delivered.WrappingToken}
		g.ExpiresAt = timestamppb.New(delivered.ExpiresAt)
	}
	return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_TokenGrant{TokenGrant: g}}
}
