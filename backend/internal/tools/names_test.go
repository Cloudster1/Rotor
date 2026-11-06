package tools

import (
	"strings"
	"testing"
)

// A name an administrator gives is one lower case word a binding writes plainly, so no two differ by case alone (#267).
func TestANameIsOneLowerCaseWord(t *testing.T) {
	for _, name := range []string{"exciter", "exciter-2", "armature.staging", "stator_prod", "9lives", strings.Repeat("a", 63)} {
		if !namePattern.MatchString(name) {
			t.Errorf("%q is refused", name)
		}
	}
	for _, name := range []string{"", "Exciter", "-exciter", ".exciter", "two words", "exciter/prod", strings.Repeat("a", 64)} {
		if namePattern.MatchString(name) {
			t.Errorf("%q is taken", name)
		}
	}
	taken := (&NameTaken{Name: "exciter", Taken: "Exciter"}).Error()
	if !strings.Contains(taken, "A tool called Exciter exists already") || !strings.HasSuffix(taken, "such as exciter-2.") {
		t.Errorf("a taken name reads %q", taken)
	}
}
