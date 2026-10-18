package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// Model is a model a workflow may name.
type Model struct {
	ProviderID   uuid.UUID
	ProviderName string
	Name         string
	Default      bool
}

// Models is every model of the enabled providers, the installation's default first.
func (s *Service) Models(ctx context.Context) ([]Model, error) {
	providers, err := s.query(ctx, s.db.Read, `SELECT `+columns+` FROM provider WHERE enabled ORDER BY name`)
	if err != nil {
		return nil, err
	}
	out := []Model{}
	for _, p := range providers {
		for _, m := range p.Models {
			out = append(out, Model{ProviderID: p.ID, ProviderName: p.Name, Name: m, Default: p.IsDefault && m == p.DefaultModel})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Default && !out[j].Default })
	return out, nil
}

// Resolution is the provider and model a run's agents use.
type Resolution struct {
	Provider Provider
	Model    string
}

// Resolve finds what a workflow's model names among the enabled providers, for the control plane: no model is the
// installation's default; a model several providers list goes to the default provider, else the first by name.
func (s *Service) Resolve(ctx context.Context, wanted *spec.Model) (Resolution, error) {
	providers, err := s.query(ctx, s.db.ReadAdmin, `SELECT `+columns+` FROM provider WHERE enabled ORDER BY is_default DESC, name`)
	if err != nil {
		return Resolution{}, err
	}
	for _, p := range providers {
		switch {
		case wanted == nil && p.IsDefault:
			return Resolution{Provider: p, Model: p.DefaultModel}, nil
		case wanted != nil && (wanted.Provider == "" || wanted.Provider == p.Name) && slices.Contains(p.Models, wanted.Name):
			return Resolution{Provider: p, Model: wanted.Name}, nil
		}
	}
	if wanted == nil {
		return Resolution{}, fmt.Errorf("%w: the workflow names no model and the installation has no default", ErrNoModel)
	}
	return Resolution{}, fmt.Errorf("%w: no enabled provider offers %s", ErrNoModel, wanted.Name)
}

// Key is a provider's key from OpenBao, for the run whose agents use it.
func (s *Service) Key(ctx context.Context, id uuid.UUID) (string, error) {
	k, _, err := s.keys.ProviderKey(ctx, id)
	return k.Key, err
}

// TestResult says whether the provider answered with the key Rotor holds, and which models it offers.
type TestResult struct {
	OK      bool
	Message string
	Models  []string
}

// Test asks the provider which models it offers, with the key Rotor holds, as an agent node's first call would.
func (s *Service) Test(ctx context.Context, id uuid.UUID) (TestResult, error) {
	var p Provider
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		p, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM provider WHERE id = $1`, id))
		return err
	})
	if err != nil {
		return TestResult{}, err
	}
	key, err := s.Key(ctx, id)
	if err != nil {
		return TestResult{Message: "Rotor holds no key for this provider. Add one, then test again."}, nil
	}
	req, err := modelsRequest(ctx, p, key)
	if err != nil {
		return TestResult{}, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return TestResult{Message: fmt.Sprintf("The provider does not answer at %s. Check its address.", req.URL.Redacted())}, nil
	}
	defer resp.Body.Close()
	var listed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return TestResult{Message: "The provider refused the key. Check it, then test again."}, nil
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, config.MaxToolDocumentBytes)).Decode(&listed) != nil {
		return TestResult{Message: fmt.Sprintf("The provider answered %d, not a list of models.", resp.StatusCode)}, nil
	}
	out := TestResult{OK: true, Message: "The provider answered with the key Rotor holds."}
	for _, m := range listed.Data {
		out.Models = append(out.Models, m.ID)
	}
	return out, nil
}

// modelsRequest is each kind's list of models, authenticated as that kind expects.
func modelsRequest(ctx context.Context, p Provider, key string) (*http.Request, error) {
	base := p.BaseURL
	var target string
	header, value := "Authorization", "Bearer "+key
	switch p.Kind {
	case KindAnthropic:
		if base == "" {
			base = config.AnthropicBaseURL
		}
		target, header, value = base+"/v1/models", "x-api-key", key
	case KindOpenAI:
		if base == "" {
			base = config.OpenAIBaseURL
		}
		target = base + "/models"
	case KindAzureOpenAI:
		target, header, value = base+"/openai/models?api-version="+p.APIVersion, "api-key", key
	case KindOpenAICompatible:
		target = base + "/models"
	default:
		return nil, errors.New("a provider of an unknown kind")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(header, value)
	if p.Kind == KindAnthropic {
		req.Header.Set("anthropic-version", config.AnthropicVersion)
	}
	return req, nil
}
