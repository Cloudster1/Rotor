package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Token types of RFC 8693 section 3.
const tokenTypeAccess = "urn:ietf:params:oauth:token-type:access_token"

// exchangeToken trades the grant's access token for a narrower, shorter one of the run's own (RFC 8693), which the
// run's end revokes alone; the grant's token is never handed out, so ending a run never ends the grant (#45).
func (s *Service) exchangeToken(ctx context.Context, tool tools.Tool, client vault.ToolClient, subject string, scopes []string) (tokens, error) {
	form := url.Values{
		"grant_type": {tools.GrantTokenExchange}, "subject_token": {subject}, "subject_token_type": {tokenTypeAccess},
		"requested_token_type": {tokenTypeAccess}, "resource": {tool.Resource},
	}
	if len(scopes) > 0 {
		form.Set("scope", strings.Join(scopes, " "))
	}
	if param := tool.Profile().LifetimeParam; param != "" {
		form.Set(param, strconv.Itoa(int(config.ExchangedTokenLifetime/time.Second)))
	}
	if client.AuthMethod == "client_secret_post" {
		form.Set("client_id", client.ClientID)
		form.Set("client_secret", client.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tool.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if client.AuthMethod != "client_secret_post" {
		// RFC 6749 section 2.3.1: the id and secret are form-encoded before they go into the header.
		req.SetBasicAuth(url.QueryEscape(client.ClientID), url.QueryEscape(client.ClientSecret))
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return tokens{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	var answer struct {
		AccessToken      string `json:"access_token"`
		ExpiresIn        int64  `json:"expires_in"`
		Scope            string `json:"scope"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, config.MaxToolDocumentBytes)).Decode(&answer)
	switch {
	case resp.StatusCode >= http.StatusInternalServerError:
		return tokens{}, fmt.Errorf("%w: %s answered %d to the exchange", ErrUnreachable, tool.Name, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return tokens{}, &oauthError{Code: answer.Error, Description: answer.ErrorDescription}
	case decodeErr != nil || answer.AccessToken == "":
		return tokens{}, fmt.Errorf("%s answered the exchange without a token", tool.Name)
	}
	out := tokens{Access: answer.AccessToken, Scope: answer.Scope}
	if answer.ExpiresIn > 0 {
		out.Expiry = time.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)
	}
	return out, nil
}
