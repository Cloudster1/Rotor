package hello

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGreeting(t *testing.T) {
	if diff := cmp.Diff("Hello, Rotor!", Greeting("Rotor")); diff != "" {
		t.Fatal(diff)
	}
}
