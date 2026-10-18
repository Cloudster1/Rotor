package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Paths Rotor reads under a tool's base URL, and the capability it needs there.
const (
	metaPath      = "/api/v1/meta"
	mcpPath       = "/api/v1/mcp"
	mcpCapability = "mcp@1"
	resourceMeta  = "/.well-known/oauth-protected-resource"
	serverMeta    = "/.well-known/oauth-authorization-server"
	methodS256    = "S256"
	authBasic     = "client_secret_basic"
	authPost      = "client_secret_post"
)

// discovery is everything Rotor learns about a tool before it registers there.
type discovery struct {
	BaseURL, Name, Resource, Issuer string
	Capabilities, ScopesSupported   []string
	AuthorizationEndpoint           string
	TokenEndpoint                   string
	RevocationEndpoint              string
	RegistrationEndpoint            string
	// AuthMethods are the client authentications the token endpoint accepts.
	AuthMethods []string
}

// normalizeBaseURL refuses an address Rotor should not call and returns it without a trailing slash.
func normalizeBaseURL(raw string, allowHTTP bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", refuse("%q is not a tool's address. Give the address you open the tool at, such as https://exciter.example.com.", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", refuse("Give the tool's address without a query, a fragment or credentials.")
	}
	if u.Scheme == "http" && !allowHTTP && !isLoopback(u.Hostname()) {
		return "", refuse("Rotor sends tokens to a tool only over HTTPS. Give the tool's https:// address.")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String(), nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// wellKnown inserts a well-known path between the host and the path of target, as RFC 8414 and RFC 9728 do.
func wellKnown(target, suffix string) (string, error) {
	u, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	u.Path = suffix + strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// discover reads the tool's meta, its protected resource metadata and its authorization server's metadata.
func (s *Service) discover(ctx context.Context, base string) (discovery, error) {
	d := discovery{BaseURL: base, Resource: base + mcpPath}

	var meta struct {
		Product      string            `json:"product"`
		Capabilities []json.RawMessage `json:"capabilities"`
	}
	if err := s.getJSON(ctx, base+metaPath, &meta); err != nil {
		return d, notATool(base, err)
	}
	d.Name, d.Capabilities = meta.Product, capabilityNames(meta.Capabilities)
	if d.Name == "" {
		return d, refuse("The address %s answers %s without naming its product, so it is not a tool of the suite.", base, metaPath)
	}
	if !slices.Contains(d.Capabilities, mcpCapability) {
		return d, refuse("%s does not offer %s, so Rotor cannot act in it. Upgrade it to a version with an MCP server.", title(d.Name), mcpCapability)
	}

	var resource struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	resourceURL, _ := wellKnown(d.Resource, resourceMeta)
	if err := s.getJSON(ctx, resourceURL, &resource); err != nil {
		return d, missing(title(d.Name), "protected resource metadata (RFC 9728)", err)
	}
	if strings.TrimSuffix(resource.Resource, "/") != d.Resource {
		return d, refuse("%s describes its MCP endpoint as %q, not %q. Check the address you gave.", title(d.Name), resource.Resource, d.Resource)
	}
	if len(resource.AuthorizationServers) == 0 {
		return d, refuse("%s names no authorization server, so people cannot connect to it with OAuth. Switch on its OAuth connections.", title(d.Name))
	}
	d.Issuer = resource.AuthorizationServers[0]

	var server struct {
		Issuer                string   `json:"issuer"`
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint         string   `json:"token_endpoint"`
		RevocationEndpoint    string   `json:"revocation_endpoint"`
		RegistrationEndpoint  string   `json:"registration_endpoint"`
		ScopesSupported       []string `json:"scopes_supported"`
		ResponseTypes         []string `json:"response_types_supported"`
		CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
		AuthMethods           []string `json:"token_endpoint_auth_methods_supported"`
	}
	serverURL, err := wellKnown(d.Issuer, serverMeta)
	if err != nil {
		return d, refuse("%s names %q as its authorization server, which is no address.", title(d.Name), d.Issuer)
	}
	if err := s.getJSON(ctx, serverURL, &server); err != nil {
		return d, missing(title(d.Name), "authorization server metadata (RFC 8414)", err)
	}
	switch {
	case server.Issuer != d.Issuer:
		return d, refuse("%s's authorization server calls itself %q but is named %q; Rotor trusts neither. Fix the tool's address settings.", title(d.Name), server.Issuer, d.Issuer)
	case server.AuthorizationEndpoint == "" || server.TokenEndpoint == "":
		return d, refuse("%s's authorization server names no authorization or token endpoint.", title(d.Name))
	case !slices.Contains(server.CodeChallengeMethods, methodS256):
		return d, refuse("%s's authorization server does not offer PKCE with S256, which Rotor requires.", title(d.Name))
	case len(server.ResponseTypes) > 0 && !slices.Contains(server.ResponseTypes, "code"):
		return d, refuse("%s's authorization server does not offer the authorization code flow.", title(d.Name))
	}
	d.AuthorizationEndpoint, d.TokenEndpoint = server.AuthorizationEndpoint, server.TokenEndpoint
	d.RevocationEndpoint, d.RegistrationEndpoint = server.RevocationEndpoint, server.RegistrationEndpoint
	d.AuthMethods = server.AuthMethods
	d.ScopesSupported = resource.ScopesSupported
	if len(d.ScopesSupported) == 0 {
		d.ScopesSupported = server.ScopesSupported
	}
	return d, nil
}

// authMethod picks how Rotor authenticates at the token endpoint: Basic unless only the form is offered.
func (d discovery) authMethod() (string, error) {
	// RFC 8414 makes client_secret_basic the default when the server lists nothing.
	if len(d.AuthMethods) == 0 || slices.Contains(d.AuthMethods, authBasic) {
		return authBasic, nil
	}
	if slices.Contains(d.AuthMethods, authPost) {
		return authPost, nil
	}
	return "", refuse("%s's token endpoint accepts no client secret, so Rotor cannot be its confidential client.", title(d.Name))
}

// capabilityNames reads a meta's capabilities, which the siblings list as objects with a name or as strings.
func capabilityNames(raw []json.RawMessage) []string {
	out := []string{}
	for _, item := range raw {
		var name string
		if json.Unmarshal(item, &name) == nil {
			out = append(out, name)
			continue
		}
		var named struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(item, &named) == nil && named.Name != "" {
			out = append(out, named.Name)
		}
	}
	slices.Sort(out)
	return out
}

// errStatus is a tool answering with something other than success.
type errStatus struct{ code int }

func (e errStatus) Error() string { return fmt.Sprintf("answered %d", e.code) }

// getJSON reads one bounded JSON document; never with a token, which the tools refuse outside /mcp.
func (s *Service) getJSON(ctx context.Context, target string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("%w: %s %w", ErrUnreachable, target, errStatus{resp.StatusCode})
	}
	if resp.StatusCode != http.StatusOK {
		return errStatus{resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, config.MaxToolDocumentBytes)).Decode(into); err != nil {
		return fmt.Errorf("not JSON: %w", err)
	}
	return nil
}

// notATool explains a base URL whose meta cannot be read: unreachable, or not a tool.
func notATool(base string, err error) error {
	if errors.Is(err, ErrUnreachable) {
		return err
	}
	return refuse("The address %s does not answer %s as a tool of the suite does (%v). Check the address you gave.", base, metaPath, err)
}

// missing explains metadata a tool does not serve, which means it offers no OAuth.
func missing(name, what string, err error) error {
	if errors.Is(err, ErrUnreachable) {
		return err
	}
	return refuse("%s serves no %s (%v), so people cannot connect to it with OAuth. Switch on its OAuth connections, or upgrade it.", name, what, err)
}
