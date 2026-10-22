package tools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// GrantTokenExchange is RFC 8693's grant type, which a run's token is minted by where the tool offers it (#45).
const GrantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"

// resourceMetadata is a protected resource's metadata (RFC 9728).
type resourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

// resourceMetadataParam is where a 401 names the metadata (RFC 9728 section 5.1).
var resourceMetadataParam = regexp.MustCompile(`resource_metadata="([^"]+)"`)

// readResource finds the MCP endpoint's resource metadata as an MCP client does: where the endpoint's 401 points,
// else the well-known path with the endpoint's path, else the host's own. Exciter serves the second, Armature the third.
func (s *Service) readResource(ctx context.Context, name, mcpURL string) (resourceMetadata, error) {
	var candidates []string
	if pointed := s.challenge(ctx, mcpURL); pointed != "" {
		candidates = append(candidates, pointed)
	}
	inserted, _ := wellKnown(mcpURL, resourceMeta)
	root, _ := wellKnown(origin(mcpURL), resourceMeta)
	for _, c := range []string{inserted, root} {
		if !slices.Contains(candidates, c) {
			candidates = append(candidates, c)
		}
	}
	var meta resourceMetadata
	var last error
	for _, c := range candidates {
		last = s.getJSON(ctx, c, &meta)
		if errors.Is(last, ErrUnreachable) {
			return meta, last
		}
		if last == nil && meta.Resource != "" {
			return meta, nil
		}
	}
	if last == nil {
		last = errors.New("it names no resource")
	}
	return meta, missing(title(name), "protected resource metadata (RFC 9728)", last)
}

// challenge asks the MCP endpoint without a token and answers the metadata its 401 names, on its own origin only.
func (s *Service) challenge(ctx context.Context, mcpURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, strings.NewReader(`{"jsonrpc":"2.0","id":0,"method":"ping"}`))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := s.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, config.MaxToolDocumentBytes))
	if resp.StatusCode != http.StatusUnauthorized {
		return ""
	}
	m := resourceMetadataParam.FindStringSubmatch(resp.Header.Get("WWW-Authenticate"))
	if m == nil || origin(m[1]) != origin(mcpURL) {
		return ""
	}
	return m[1]
}

// covering says whether the resource a tool names is its MCP endpoint or contains it, so a token for it reaches MCP.
func covering(resource, mcpURL string) bool {
	resource = strings.TrimSuffix(resource, "/")
	return resource == mcpURL || strings.HasPrefix(mcpURL, resource+"/")
}

// origin is an address's scheme and host, or "" for no address.
func origin(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
