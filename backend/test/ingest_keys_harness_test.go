//go:build integration

package test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// madeKey is an ingest key as the api answered it once: its row as listed, and the secret a service sends.
type madeKey struct {
	id, secret string
	key        map[string]any
}

// makeKey has the administrator make an ingest key through the api, and removes it after the test.
func makeKey(t *testing.T, h *harness, admin *browser, body map[string]any) madeKey {
	t.Helper()
	status, answer := admin.api(http.MethodPost, "/settings/ingest-keys", body)
	if status != http.StatusCreated {
		t.Fatalf("make an ingest key: %d %v", status, answer)
	}
	key, _ := answer["key"].(map[string]any)
	secret, _ := answer["secret"].(string)
	id, _ := key["id"].(string)
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM ingest_key WHERE id = $1`, id) })
	return madeKey{id: id, secret: secret, key: key}
}

// sendWith posts one span of a fresh trace with a credential and answers the status and the body.
func sendWith(t *testing.T, credential string) (int, []byte) {
	t.Helper()
	status, _, body := sendSpans(t, otlpCall{token: credential, contentType: mediaJSON,
		body: jsonOf(t, appSpan(randomTraceID(), "LangGraph", nil))})
	return status, body
}

// callWith calls a route of the api with a bearer credential and answers the status and the body.
func callWith(t *testing.T, method, path, credential, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, apiURL(t)+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(answer)
}
