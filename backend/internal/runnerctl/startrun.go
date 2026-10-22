package runnerctl

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
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
	// dataKey is the run's wrapped data key, which opens what is stored of it (#41).
	dataKey string
	// Resume answers the interrupt ResumeID names: the latest approved approval's decision, if any.
	Resume   []byte
	ResumeID string
	// Traceparent is the run's trace, which its runner's spans join (#37); TraceContent lets them carry text.
	Traceparent  string
	TraceContent bool
	spec         *spec.Spec
	// triggerID is the trigger that started the run, whose unattended writes are audited (#128); nil for a person's.
	triggerID *uuid.UUID
	// model is the provider and model the run's agents use; nil for a workflow without agents.
	model *providers.Resolution
	// agents are the models of the agents that name their own, by node id (#133); one that resolves to none is missing.
	agents map[string]providers.Resolution
}

// binding is a spec binding resolved to a tool, for minting its tokens.
type binding struct {
	toolID uuid.UUID
	scopes []string
}

func (s *Server) loadRun(ctx context.Context, runID uuid.UUID) (*run, error) {
	r := &run{ID: runID}
	var sealed []byte
	var resume *approved
	err := s.DB.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT r.user_id, r.status, v.spec_yaml, r.data_key, r.sealed_inputs, r.traceparent, r.trigger_id
			FROM run r JOIN workflow_version v ON v.workflow_id = r.workflow_id AND v.number = r.version
			WHERE r.id = $1`, runID).Scan(&r.UserID, &r.Status, &r.SpecYAML, &r.dataKey, &sealed, &r.Traceparent, &r.triggerID)
		if err != nil {
			return err
		}
		resume, err = latestApproved(ctx, tx, runID)
		return err
	})
	if err != nil {
		return nil, err
	}
	box, err := s.Keys.For(ctx, runID, r.dataKey)
	if err != nil {
		return nil, err
	}
	if r.Inputs, err = box.Open(envelope.ColumnInputs, "", sealed); err != nil {
		return nil, err
	}
	if resume != nil {
		if r.Resume, err = resume.answer(box); err != nil {
			return nil, err
		}
		r.ResumeID = resume.interrupt
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
		scopes := b.Scopes
		if access == spec.AccessRead && len(scopes) == 0 && tool.OffersExchange() {
			// An exchanged token carries only what is asked, so a reading binding asks for reading alone.
			scopes = tool.Profile().ReadScopes
		}
		mcpURL := tool.MCPURL
		if s.Reach != nil {
			mcpURL = s.Reach(mcpURL)
		}
		out = append(out, &runnerpb.ToolBinding{Binding: name, ToolId: tool.ID.String(), McpUrl: mcpURL,
			Scopes: scopes, Access: access, AllowTools: b.AllowTools, LinkHosts: tool.LinkHosts()})
		resolved[name] = binding{toolID: tool.ID, scopes: scopes}
	}
	return out, resolved, nil
}

// resolveModel finds the run's provider and model, and each agent's own (#133); a model that is not allowed leaves
// its agents without one, and they fail saying so.
func (s *Server) resolveModel(ctx context.Context, r *run) {
	if s.Models == nil {
		return
	}
	if r.spec.UsesWorkflowModel() {
		resolved, err := s.Models.Resolve(ctx, r.spec.Model)
		if err != nil {
			s.Log.Warn("a run starts without a model", "run", r.ID, "error", err)
		} else {
			r.model = &resolved
		}
	}
	r.agents = map[string]providers.Resolution{}
	for node, wanted := range r.spec.AgentModels() {
		resolved, err := s.Models.Resolve(ctx, &wanted)
		if err != nil {
			s.Log.Warn("an agent starts without its model", "run", r.ID, "node", node, "error", err)
			continue
		}
		r.agents[node] = resolved
	}
}

// keyProviders are the distinct providers whose keys the run's agents need.
func (r *run) keyProviders() []providers.Provider {
	var out []providers.Provider
	seen := map[uuid.UUID]bool{}
	add := func(p providers.Provider) {
		if !seen[p.ID] {
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	if r.model != nil {
		add(r.model.Provider)
	}
	for _, node := range sortedNodes(r.agents) {
		add(r.agents[node].Provider)
	}
	return out
}

func sortedNodes(m map[string]providers.Resolution) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// modelConfig is one model as the runner builds it, at an address a sandbox reaches; the key comes apart.
func (s *Server) modelConfig(m *providers.Resolution) *runnerpb.ModelConfig {
	if m == nil {
		return nil
	}
	base := m.Provider.BaseURL
	if s.Reach != nil && base != "" {
		base = s.Reach(base)
	}
	return &runnerpb.ModelConfig{ProviderId: m.Provider.ID.String(), Kind: m.Provider.Kind, Model: m.Model,
		BaseUrl: base, ApiVersion: m.Provider.APIVersion}
}

// agentConfigs are the agents' own models as the runner builds them, by node id.
func (s *Server) agentConfigs(r *run) map[string]*runnerpb.ModelConfig {
	out := map[string]*runnerpb.ModelConfig{}
	for node, m := range r.agents {
		out[node] = s.modelConfig(&m)
	}
	return out
}

// secret is one provider's key, wrapped, sent after every Hello since a grant is never stored to be resent.
func (s *Server) secret(ctx context.Context, p providers.Provider) (*runnerpb.ConnectResponse, error) {
	key, err := s.Models.Key(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("read the key of provider %s: %w", p.Name, err)
	}
	wrapped, err := s.Wrap.Wrap(ctx, map[string]any{"api_key": key}, config.WrapTTL)
	if err != nil {
		return nil, fmt.Errorf("wrap the key of provider %s: %w", p.Name, err)
	}
	return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_SecretGrant{SecretGrant: &runnerpb.SecretGrant{
		ProviderId: p.ID.String(), Secret: &runnerpb.SecretGrant_WrappingToken{WrappingToken: wrapped}}}}, nil
}

// startRun is the run's StartRun: its pinned spec, inputs, bindings and models, and the answer it resumes with, if any.
func startRun(r *run, bindings []*runnerpb.ToolBinding, model *runnerpb.ModelConfig, agents map[string]*runnerpb.ModelConfig) *runnerpb.ConnectResponse {
	start := &runnerpb.StartRun{RunId: r.ID.String(), SpecYaml: r.SpecYAML, InputsJson: string(r.Inputs), Tools: bindings, Model: model,
		AgentModels: agents, Traceparent: r.Traceparent, TraceContent: r.TraceContent}
	if r.ResumeID != "" {
		start.Resume, start.ResumeJson, start.ResumeInterruptId = true, string(r.Resume), r.ResumeID
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
		if s.MintFailed != nil {
			s.MintFailed(mintFailure(err))
		}
	} else {
		g.Token = &runnerpb.TokenGrant_WrappingToken{WrappingToken: delivered.WrappingToken}
		g.ExpiresAt = timestamppb.New(delivered.ExpiresAt)
	}
	return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_TokenGrant{TokenGrant: g}}
}
