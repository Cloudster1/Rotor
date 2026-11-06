package tools

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// probeError is the OAuth error a token endpoint answers a client it does not know with (RFC 6749, section 5.2).
const probeError = "invalid_client"

// clientKnown asks the tool's token endpoint to refresh a token it never issued, with Rotor's client. The tool
// authenticates the client first: invalid_client means it forgot Rotor, while invalid_grant proves it knows it.
func (s *Service) clientKnown(ctx context.Context, tool Tool, client vault.ToolClient) (bool, error) {
	nonce := make([]byte, config.ProbeTokenBytes)
	if _, err := rand.Read(nonce); err != nil {
		return false, err
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"rotor-check-" + base64.RawURLEncoding.EncodeToString(nonce)}}
	if client.AuthMethod == authPost {
		form.Set("client_id", client.ClientID)
		form.Set("client_secret", client.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tool.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if client.AuthMethod != authPost {
		// RFC 6749 section 2.3.1: the id and secret are form-encoded before they go into the header.
		req.SetBasicAuth(url.QueryEscape(client.ClientID), url.QueryEscape(client.ClientSecret))
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: ask %s's token endpoint: %w", ErrUnreachable, tool.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusInternalServerError {
		return false, fmt.Errorf("%w: %s's token endpoint answered %d", ErrUnreachable, tool.Name, resp.StatusCode)
	}
	var answer struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, config.MaxToolDocumentBytes)).Decode(&answer)
	return answer.Error != probeError, nil
}
