package triggers

import (
	"fmt"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/runs"
)

// A firing on an installation without sandboxes ends refused once, instead of being retried forever.
func TestAFiringWithoutSandboxesIsRefusedNotRetried(t *testing.T) {
	reason, ok := refusal(fmt.Errorf("start: %w", runs.ErrNoSandboxes))
	if !ok || reason != ReasonRunsOff {
		t.Errorf("refusal = %q, %v; want %q, true", reason, ok, ReasonRunsOff)
	}
	if Sentence(ReasonRunsOff) == "" {
		t.Error("the reason has no sentence")
	}
}
