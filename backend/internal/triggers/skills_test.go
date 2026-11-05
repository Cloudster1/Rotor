package triggers

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/runs"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// A firing whose owner may not read a skill turns the trigger off, naming the skill, instead of being retried.
func TestAFiringWithASkillItsOwnerMayNotReadIsRefused(t *testing.T) {
	err := fmt.Errorf("start: %w", &runs.SkillUnreadable{Node: "analyse", Skill: spec.SkillRef{Name: "secret-method", Version: 1}})
	reason, ok := refusal(err)
	if !ok || reason != ReasonSkillUnreadable {
		t.Errorf("refusal = %q, %v; want %q, true", reason, ok, ReasonSkillUnreadable)
	}
	if Sentence(ReasonSkillUnreadable) == "" {
		t.Error("the reason has no sentence")
	}
	if detail := refusalDetail(err); !strings.Contains(detail, "secret-method") {
		t.Errorf("the detail %q does not name the skill", detail)
	}
}
