package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// armatureLike bends the fake into Armature's shape (#45): its 401 names metadata at its own root, which names the
// whole API as the resource, and its server offers token exchange.
func armatureLike(t *testing.T, f *fakeTool, pointTo string) {
	t.Helper()
	f.meta["product"] = "armature"
	f.resource["resource"] = f.base + "/api/v1"
	f.resource["scopes_supported"] = []string{"read", "issue.write", "webhooks.partner"}
	f.server["grant_types_supported"] = []string{"authorization_code", "refresh_token", GrantTokenExchange}
	root := f.resource
	f.resource = nil
	mux := http.NewServeMux()
	mux.HandleFunc("POST /t/demo/api/v1/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+pointTo+`"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("GET /t/demo/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(root)
	})
	previous := f.Config.Handler
	f.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		previous.ServeHTTP(w, r)
	})
}

func TestDiscoveryFollowsTheMCPEndpointsChallengeToAResourceHoldingIt(t *testing.T) {
	f := newFakeTool(t)
	armatureLike(t, f, f.URL+"/t/demo/.well-known/oauth-protected-resource")
	s := f.service()
	d, err := s.discover(context.Background(), f.base)
	if err != nil {
		t.Fatal(err)
	}
	if d.Resource != f.base+"/api/v1" || d.MCPURL != f.base+"/api/v1/mcp" || !slices.Contains(d.GrantTypes, GrantTokenExchange) {
		t.Errorf("discovered %+v", d)
	}
	if _, err := s.register(context.Background(), d, authBasic); err != nil {
		t.Fatal(err)
	}
	grants, _ := f.registered["grant_types"].([]any)
	if f.registered["scope"] != "read issue.write" || len(grants) != 3 || grants[2] != GrantTokenExchange {
		t.Errorf("asked to register %v, want Armature's scopes but webhooks.partner, and the exchange", f.registered)
	}
}

func TestAChallengeToAnotherOriginIsNotFollowed(t *testing.T) {
	f := newFakeTool(t)
	armatureLike(t, f, "https://elsewhere.test/.well-known/oauth-protected-resource")
	var refusal *Refusal
	if _, err := f.service().discover(context.Background(), f.base); !errors.As(err, &refusal) || !strings.Contains(refusal.Message, "protected resource metadata") {
		t.Errorf("a challenge naming another origin = %v, want the metadata missing", err)
	}
}

func TestAToolWithOneTokenPerGrantNeedsTheExchange(t *testing.T) {
	f := newFakeTool(t)
	armatureLike(t, f, f.URL+"/t/demo/.well-known/oauth-protected-resource")
	f.server["grant_types_supported"] = []string{"authorization_code", "refresh_token"}
	var refusal *Refusal
	if _, err := f.service().discover(context.Background(), f.base); !errors.As(err, &refusal) || !strings.Contains(refusal.Message, "token exchange") {
		t.Errorf("Armature without the exchange = %v, want a sentence naming it", err)
	}
}
