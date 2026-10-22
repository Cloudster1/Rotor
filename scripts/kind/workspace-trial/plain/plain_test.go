// Package plain has a test and no dependencies, as a fixture of the gate must, having no network to fetch them.
package plain

import (
	"strings"
	"testing"
)

func TestUpper(t *testing.T) {
	if got := strings.ToUpper("rotor"); got != "ROTOR" {
		t.Fatalf("got %q", got)
	}
}
