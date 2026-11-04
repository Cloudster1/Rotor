//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// browserFor is a browser signed in as a person addPerson made, who has no account in the realm: the session is opened
// as sign-in opens one, and its cookie is set as the callback would set it.
func (h *harness) browserFor(t *testing.T, person uuid.UUID) *browser {
	t.Helper()
	b := newBrowser(t)
	var token string
	sessions := auth.NewService(h.cluster, h.cfg.Auth.SessionTTL)
	if err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		token, err = sessions.Open(ctx, tx, auth.SessionInput{UserID: person})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	app, err := url.Parse(b.app)
	if err != nil {
		t.Fatal(err)
	}
	b.client.Jar.SetCookies(app, []*http.Cookie{{Name: h.cfg.Auth.SessionCookie, Value: token, Path: "/"}})
	return b
}

// idOf is the id the api gave something it answered with under key.
func idOf(t *testing.T, answer map[string]any, key string) string {
	t.Helper()
	thing, _ := answer[key].(map[string]any)
	id, _ := thing["id"].(string)
	if id == "" {
		t.Fatalf("no %s id in %v", key, answer)
	}
	return id
}

// signedInID is the id of the person a browser is signed in as.
func signedInID(t *testing.T, b *browser) uuid.UUID {
	t.Helper()
	status, me := b.api(http.MethodGet, "/auth/me", nil)
	user, _ := me["user"].(map[string]any)
	id, err := uuid.Parse(func() string { s, _ := user["id"].(string); return s }())
	if status != http.StatusOK || err != nil {
		t.Fatalf("who is signed in = %d %v", status, me)
	}
	return id
}

// listed is the values of field across the objects a listing answered with under key.
func listed(answer map[string]any, key, field string) []string {
	out := []string{}
	items, _ := answer[key].([]any)
	for _, item := range items {
		thing, _ := item.(map[string]any)
		if value, ok := thing[field].(string); ok {
			out = append(out, value)
		}
	}
	return out
}
