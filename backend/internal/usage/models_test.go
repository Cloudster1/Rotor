package usage

import (
	"context"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/providers"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// installationDefault resolves every model to the installation's default, as a provider marked default does.
type installationDefault struct{}

func (installationDefault) Resolve(context.Context, *spec.Model) (providers.Resolution, error) {
	return providers.Resolution{Model: "default-model"}, nil
}

// A workflow of code and tool nodes runs on no model, so its maxCost never asks for a model's prices (#271).
func TestOnlyAgentsBringAModelToPrice(t *testing.T) {
	s := &Service{models: installationDefault{}}
	for name, c := range map[string]struct {
		nodes string
		want  int
	}{
		"code alone": {"  - {id: a, type: code, source: 'def run(state, ctx): return {}', output: {schema: {type: object}}}\n", 0},
		"an agent":   {"  - {id: a, type: agent, prompt: Think.}\n", 1},
	} {
		sp, problems := spec.Parse([]byte("apiVersion: rotor.dev/v1\nkind: Workflow\nmetadata: {name: m}\n" +
			"limits: {maxSteps: 5, timeout: 1m, maxCost: 1}\nentry: a\nnodes:\n" + c.nodes))
		if sp == nil || len(problems) > 0 {
			t.Fatalf("%s: %v", name, problems)
		}
		if got, err := s.runModels(context.Background(), sp); err != nil || len(got) != c.want {
			t.Errorf("%s resolves the models %v (%v), want %d", name, got, err, c.want)
		}
	}
}
