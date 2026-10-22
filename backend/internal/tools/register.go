package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/tools/profiles"
	"github.com/cloudster1/rotor/backend/internal/vault"
	"github.com/cloudster1/rotor/backend/internal/version"
)

// clientName is what a person sees on the tool's consent page.
const clientName = "Rotor"

// RequestedScopes is every scope the tool supports but those Rotor never uses there, in the tool's order.
func (t Tool) RequestedScopes() []string { return t.Profile().Requested(t.ScopesSupported) }

// registration is the RFC 7591 request Rotor sends.
type registration struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope,omitempty"`
	SoftwareID              string   `json:"software_id"`
	SoftwareVersion         string   `json:"software_version"`
}

// register makes Rotor a confidential client of the tool by RFC 7591.
func (s *Service) register(ctx context.Context, d discovery, method string) (vault.ToolClient, error) {
	if d.RegistrationEndpoint == "" {
		return vault.ToolClient{}, refuse("%s offers no client registration, so give the client id and secret an administrator of %s registered for Rotor, with the redirect URI %s.", title(d.Name), title(d.Name), s.redirectURL)
	}
	grants := []string{"authorization_code", "refresh_token"}
	if exchangesGrantTokens(d.Name, d.GrantTypes) {
		// Asked for at registration, since a tool such as Armature leaves it out of a client that does not.
		grants = append(grants, GrantTokenExchange)
	}
	body, err := json.Marshal(registration{
		ClientName: clientName, RedirectURIs: []string{s.redirectURL}, TokenEndpointAuthMethod: method,
		GrantTypes: grants, ResponseTypes: []string{"code"}, SoftwareID: "rotor", SoftwareVersion: version.Current().Version,
		Scope: strings.Join(profiles.For(d.Name).Requested(d.ScopesSupported), " "),
	})
	if err != nil {
		return vault.ToolClient{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return vault.ToolClient{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return vault.ToolClient{}, fmt.Errorf("%w: register at %s: %w", ErrUnreachable, d.Name, err)
	}
	defer resp.Body.Close()
	var answer struct {
		ClientID         string `json:"client_id"`
		ClientSecret     string `json:"client_secret"`
		AuthMethod       string `json:"token_endpoint_auth_method"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, config.MaxToolDocumentBytes)).Decode(&answer)
	switch {
	case resp.StatusCode >= http.StatusInternalServerError:
		return vault.ToolClient{}, fmt.Errorf("%w: %s answered %d to the registration", ErrUnreachable, d.Name, resp.StatusCode)
	case resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK:
		return vault.ToolClient{}, refuse("%s refused to register Rotor: %s. Ask an administrator of %s, or give a client registered by hand.",
			title(d.Name), describe(answer.Error, answer.ErrorDescription, resp.StatusCode), title(d.Name))
	case decodeErr != nil || answer.ClientID == "" || answer.ClientSecret == "":
		return vault.ToolClient{}, refuse("%s registered Rotor without a client id and secret, so Rotor cannot be its confidential client.", title(d.Name))
	}
	if answer.AuthMethod != "" {
		method = answer.AuthMethod
	}
	return vault.ToolClient{ClientID: answer.ClientID, ClientSecret: answer.ClientSecret, AuthMethod: method, RegisteredAt: time.Now().UTC()}, nil
}

// describe is an OAuth error in words, for a refusal the administrator reads.
func describe(code, description string, status int) string {
	switch {
	case description != "":
		return description
	case code != "":
		return code
	}
	return fmt.Sprintf("it answered %d", status)
}
