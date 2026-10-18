package oidc

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// HTTPClient reaches a provider whose public address is not the one this process reaches it at;
// rewrites maps the first to the second. Not to be confused with back-channel logout (logouttoken.go).
func HTTPClient(rewrites map[string]string) *http.Client {
	// In a compose stack the issuer is localhost:<port>, which inside the api container is the container itself;
	// the request goes to the reachable address with the public Host, so the provider keeps naming its issuer.
	if len(rewrites) == 0 {
		return &http.Client{Timeout: config.ProviderTimeout}
	}
	return &http.Client{Timeout: config.ProviderTimeout, Transport: &rewriting{rewrites: rewrites, next: http.DefaultTransport}}
}

type rewriting struct {
	rewrites map[string]string
	next     http.RoundTripper
}

func (r *rewriting) RoundTrip(req *http.Request) (*http.Response, error) {
	target, ok := r.rewrites[req.URL.Scheme+"://"+req.URL.Host]
	if !ok {
		return r.next.RoundTrip(req)
	}
	reach, err := url.Parse(target)
	if err != nil || reach.Host == "" {
		return r.next.RoundTrip(req)
	}
	clone := req.Clone(req.Context())
	clone.URL.Scheme = reach.Scheme
	clone.URL.Host = reach.Host
	// The provider answers for the name it was asked for.
	clone.Host = req.URL.Host
	return r.next.RoundTrip(clone)
}

// ParseRewrites reads "public=reachable,public=reachable" from configuration.
func ParseRewrites(spec string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(spec, ",") {
		from, to, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		from, to = strings.TrimSuffix(strings.TrimSpace(from), "/"), strings.TrimSuffix(strings.TrimSpace(to), "/")
		if from != "" && to != "" {
			out[from] = to
		}
	}
	return out
}

// providerContext puts the client on the context for both libraries, which each look for it under their own key.
func (s *Service) providerContext(ctx context.Context) context.Context {
	ctx = coreoidc.ClientContext(ctx, s.http)
	return context.WithValue(ctx, oauth2.HTTPClient, s.http)
}

// Rewrite answers where a public address is reached from inside the stack, by the first pair whose origin it has.
func Rewrite(rewrites map[string]string, address string) string {
	for public, reachable := range rewrites {
		if address == public || strings.HasPrefix(address, public+"/") {
			return reachable + strings.TrimPrefix(address, public)
		}
	}
	return address
}
