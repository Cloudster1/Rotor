package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/ingestkeys"
)

// fakeIngestKeys answers err for every key it is asked about, and counts the keys it looked up.
type fakeIngestKeys struct {
	IngestKeys
	err   error
	asked int
}

func (f *fakeIngestKeys) Authenticate(context.Context, string) (ingestkeys.Sending, error) {
	f.asked++
	return ingestkeys.Sending{}, f.err
}

func messageOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	msg, _ := errorOf(t, body)["message"].(string)
	return msg
}

// A key is looked up at the OTLP endpoint alone; every other row, MCP and the public ones included, refuses it with
// 401 before any lookup, so a key never reads or administers anything.
func TestAnIngestKeyIsRefusedAtEveryRouteButTheOTLPEndpoint(t *testing.T) {
	s, _, _ := serviceAccountServer(t)
	s.OAuth = &fakeOAuth{}
	keys := &fakeIngestKeys{err: ingestkeys.ErrUnknown}
	s.IngestKeys = keys
	h := s.Routes(nil)
	key, err := ingestkeys.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range operations {
		path := op.path
		if !op.root {
			path = APIPrefix + op.path
		}
		keys.asked = 0
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, bearer(op.method, concrete(path), key.Value, `{}`))
		row := op.method + " " + op.path
		if op.tokensOnly {
			if keys.asked != 1 || rec.Code != http.StatusUnauthorized || !strings.Contains(messageOf(t, rec), "no such ingest key") {
				t.Errorf("%s with an unknown key = %d %q, looked up %d times", row, rec.Code, rec.Body.String(), keys.asked)
			}
			continue
		}
		if keys.asked != 0 || rec.Code != http.StatusUnauthorized || !strings.Contains(messageOf(t, rec), "sends spans to "+otlpPath+" alone") {
			t.Errorf("%s with a key = %d %q, looked up %d times; want 401 before any lookup", row, rec.Code, rec.Body.String(), keys.asked)
		}
	}
}

// What the endpoint answers a key the store refuses, in the words the contract gives.
func TestTheOTLPEndpointSaysWhyAKeyWasRefused(t *testing.T) {
	s, _, _ := serviceAccountServer(t)
	keys := &fakeIngestKeys{}
	s.IngestKeys = keys
	h := s.Routes(nil)
	key, _ := ingestkeys.New()
	for name, c := range map[string]struct {
		err    error
		status int
		says   string
	}{
		"unknown":          {ingestkeys.ErrUnknown, http.StatusUnauthorized, "revoked or rotated"},
		"expired":          {ingestkeys.ErrExpired, http.StatusUnauthorized, "has expired"},
		"not let in":       {ingestkeys.ErrNotLetIn, http.StatusForbidden, "no longer let into Rotor"},
		"switched off":     {auth.ErrSwitchedOff, http.StatusForbidden, "switched off"},
		"a database fault": {errors.New("down"), http.StatusInternalServerError, ""},
	} {
		keys.err = c.err
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, bearer(http.MethodPost, otlpPath, key.Value, `{}`))
		if rec.Code != c.status || !strings.Contains(messageOf(t, rec), c.says) {
			t.Errorf("a key %s = %d %q, want %d saying %q", name, rec.Code, rec.Body.String(), c.status, c.says)
		}
		if strings.Contains(rec.Body.String(), key.Value) {
			t.Errorf("a key %s is answered with the key in the body", name)
		}
	}
}
