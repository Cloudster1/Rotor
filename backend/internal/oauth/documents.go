package oauth

// Client ID metadata documents (#114), adapted from Exciter at 3b340ca, backend/internal/oauth/clients.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Fetcher reads a client's metadata document from its address, with how long it may be kept.
type Fetcher func(ctx context.Context, address string) (body []byte, keep time.Duration, err error)

// HTTPFetcher fetches a document with a client that refuses Rotor's own network, as netguard's does.
func HTTPFetcher(client *http.Client) Fetcher {
	// A document names itself by its address, so one that sends the client elsewhere is not the client's.
	own := *client
	own.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &own
	return func(ctx context.Context, address string) ([]byte, time.Duration, error) {
		ctx, cancel := context.WithTimeout(ctx, config.OAuthMetadataFetchTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("the document answered %s", resp.Status)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, config.OAuthMaxClientMetadataBytes+1))
		if err != nil {
			return nil, 0, err
		}
		if len(body) > config.OAuthMaxClientMetadataBytes {
			return nil, 0, fmt.Errorf("the document is larger than %d KiB", config.OAuthMaxClientMetadataBytes>>10)
		}
		return body, keepFor(resp.Header.Get("Cache-Control")), nil
	}
}

var maxAge = regexp.MustCompile(`(?:^|[,\s])max-age=(\d+)`)

// keepFor is how long cache headers let a document be kept, at most a day; no-store and no-cache keep it not at all.
func keepFor(cacheControl string) time.Duration {
	lower := strings.ToLower(cacheControl)
	if strings.Contains(lower, "no-store") || strings.Contains(lower, "no-cache") {
		return 0
	}
	m := maxAge.FindStringSubmatch(lower)
	if m == nil {
		return 0
	}
	seconds, err := strconv.Atoi(m[1])
	if err != nil {
		return config.OAuthMaxMetadataAge
	}
	return min(time.Duration(seconds)*time.Second, config.OAuthMaxMetadataAge)
}

// IsMetadataClientID says whether a client_id names a metadata document: an https address with a path.
func IsMetadataClientID(id string) bool {
	u, err := url.Parse(id)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Path != "" && u.Path != "/" && u.Fragment == "" && u.User == nil
}

// fetchClient reads a client's metadata document and keeps it, held to the rules a registration is held to.
func (s *Service) fetchClient(ctx context.Context, id string) (*client, error) {
	if s.fetch == nil {
		return nil, errUnknownClient
	}
	body, keep, err := s.fetch(ctx, id)
	if err != nil {
		return nil, refuse(ErrInvalidClient, "The client's metadata document at "+id+" could not be read: "+err.Error()+".")
	}
	var doc struct {
		ClientMetadataInput
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, refuse(ErrInvalidClient, "The client's metadata document is not JSON.")
	}
	if doc.ClientID != id {
		return nil, refuse(ErrInvalidClient, "The client's metadata document names another client_id than its own address.")
	}
	if doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != AuthNone {
		return nil, refuse(ErrInvalidClient, "A client known by its metadata document keeps no secret; its token_endpoint_auth_method is none.")
	}
	if r := checkMetadata(&doc.ClientMetadataInput); r != nil {
		return nil, refuse(ErrInvalidClient, "The client's metadata document is refused: "+r.Body.ErrorDescription)
	}
	c := client{ID: id, Name: doc.ClientName, URI: doc.ClientURI, Kind: ClientMetadata, RedirectURIs: doc.RedirectURIs,
		AuthMethod: AuthNone, GrantTypes: doc.GrantTypes}
	var kept bool
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// A registered client's id is never an https address, but the row it would overwrite is checked all the same.
		tag, err := tx.Exec(ctx, `
			INSERT INTO oauth_client (id, kind, name, uri, redirect_uris, grant_types, fresh_until)
			VALUES ($1, 'metadata', $2, $3, $4, $5, now() + make_interval(secs => $6))
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, uri = EXCLUDED.uri, redirect_uris = EXCLUDED.redirect_uris,
				grant_types = EXCLUDED.grant_types, fresh_until = EXCLUDED.fresh_until
			WHERE oauth_client.kind = 'metadata'`, c.ID, c.Name, c.URI, c.RedirectURIs, c.GrantTypes, keep.Seconds())
		kept = tag.RowsAffected() == 1
		return err
	})
	if err != nil {
		return nil, err
	}
	if !kept {
		return nil, errUnknownClient
	}
	return &c, nil
}
