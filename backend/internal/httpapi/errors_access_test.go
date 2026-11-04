package httpapi

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/perm"
)

// Every refusal of roles, groups and grants reads as a sentence, even when the domain wrapped it with what it was about.
func TestAccessRefusalsAreSentences(t *testing.T) {
	for _, r := range accessRefusals {
		var apiErr *APIError
		if !errors.As(accessError(fmt.Errorf("%w: runner", r.err)), &apiErr) || apiErr.Status != r.status {
			t.Errorf("%v maps to %v, want status %d", r.err, apiErr, r.status)
			continue
		}
		if msg := apiErr.Message; !strings.HasSuffix(msg, ".") || strings.ToUpper(msg[:1]) != msg[:1] || strings.Contains(msg, ": runner") {
			t.Errorf("%v reads %q, want its own sentence", r.err, msg)
		}
	}
	for _, nf := range accessNotFound {
		if got := toAPIError(accessError(nf.err)); got.Status != 404 || !strings.HasSuffix(got.Message, ".") {
			t.Errorf("%v maps to %v, want a 404 saying where to look", nf.err, got)
		}
	}
	if got := accessError(perm.ErrNotFound); errors.Is(got, perm.ErrNotFound) {
		t.Error("a missing grant leaks the domain's error rather than the wire's")
	}
}
