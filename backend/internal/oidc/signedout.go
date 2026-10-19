package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2/clientcredentials"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// errNoAdminAPI is an issuer that is not a Keycloak realm, whose admin API Rotor cannot derive.
var errNoAdminAPI = errors.New("the issuer is not a Keycloak realm")

// providerPerson is what Rotor reads of a person at Keycloak: whether they may sign in, and the second before
// which Keycloak's sign-out of all their sessions ended every token they held (0 when never).
type providerPerson struct {
	Enabled   bool  `json:"enabled"`
	NotBefore int64 `json:"notBefore"`
}

// endSignedOutLater asks Keycloak after it has answered: its sign-out of a person names one session in a single
// logout token and stamps the person only once that request ends, which waits on this one (#53).
func (s *Service) endSignedOutLater(ctx context.Context, logout *Logout) {
	if logout.Subject == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		for _, wait := range config.ProviderSignOutChecks {
			time.Sleep(wait)
			attempt, cancel := context.WithTimeout(ctx, config.ProviderTimeout)
			s.endSignedOut(attempt, logout)
			cancel()
		}
	}()
}

// endSignedOut ends the person's sessions made before Keycloak's last sign-out of them, or all once Keycloak no
// longer lets them sign in; ending them twice ends nothing more.
func (s *Service) endSignedOut(ctx context.Context, logout *Logout) {
	person, err := s.person(ctx, logout.Subject)
	if err != nil {
		slog.WarnContext(ctx, "Keycloak did not say which of the person's sessions it signed out, so only the one its "+
			"token names ended; give the rotor client's service account the realm-management role view-users",
			"error", err)
		return
	}
	var cutoff time.Time
	switch {
	case person == nil || !person.Enabled:
		cutoff = time.Now().Add(time.Second)
	case person.NotBefore > 0:
		// Whole seconds, so a session made in the same second as the sign-out ends too.
		cutoff = time.Unix(person.NotBefore+1, 0)
	default:
		return
	}
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return endSessions(ctx, tx, `
			DELETE FROM user_session WHERE created_at < $3
			AND user_id IN (SELECT user_id FROM user_identity WHERE issuer = $1 AND subject = $2)
			RETURNING user_id::text`, map[string]any{"signedOutBefore": cutoff.UTC().Format(time.RFC3339)},
			logout.Issuer, logout.Subject, cutoff)
	})
	if err != nil {
		slog.ErrorContext(ctx, "end the sessions Keycloak signed out", "error", err)
	}
}

// person reads the person from Keycloak's admin API as the rotor client's service account; nil when Keycloak no
// longer knows them.
func (s *Service) person(ctx context.Context, subject string) (*providerPerson, error) {
	provider, err := s.discover(ctx)
	if err != nil {
		return nil, err
	}
	realm := realmOf(s.cfg.Issuer)
	if realm == "" {
		return nil, errNoAdminAPI
	}
	base := strings.TrimSuffix(strings.TrimSuffix(s.cfg.Issuer, "/"), "/realms/"+realm)
	ctx = s.providerContext(ctx)
	creds := clientcredentials.Config{ClientID: s.cfg.ClientID, ClientSecret: s.cfg.ClientSecret, TokenURL: provider.Endpoint().TokenURL}
	token, err := creds.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("sign in as the rotor client's service account: %w", err)
	}
	address := base + "/admin/realms/" + realm + "/users/" + url.PathEscape(subject)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	token.SetAuthHeader(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ask Keycloak about the person: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("the provider answered %d for the person", resp.StatusCode)
	}
	var person providerPerson
	if err := json.NewDecoder(resp.Body).Decode(&person); err != nil {
		return nil, fmt.Errorf("read the person from Keycloak: %w", err)
	}
	return &person, nil
}

// realmOf is the realm name at the end of a Keycloak issuer, empty for any other issuer.
func realmOf(issuer string) string {
	_, name, ok := strings.Cut(strings.TrimSuffix(issuer, "/"), "/realms/")
	if !ok || strings.Contains(name, "/") {
		return ""
	}
	return name
}
