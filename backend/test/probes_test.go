//go:build integration

package test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/httpapi"
)

// get fetches a path of the stack's running api and returns its status and body.
func get(t *testing.T, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(apiURL(t) + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

func TestTheRunningAPIIsReadyAgainstItsDatabase(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		status, body := get(t, path)
		if status != http.StatusOK || string(body) != "{\"status\":\"ok\"}\n" {
			t.Errorf("GET %s = %d %s", path, status, body)
		}
	}
}

// What the running api answers is what the document promises, and names the build the checkout made.
func TestMetaAnswersAsTheDocumentSays(t *testing.T) {
	status, body := get(t, httpapi.APIPrefix+"/meta")
	if status != http.StatusOK {
		t.Fatalf("meta = %d %s", status, body)
	}
	doc := httpapi.Spec()
	schema := doc.Paths["/meta"]["get"].Responses["200"].Content["application/json"].Schema
	if err := doc.ValidateJSON(schema, body); err != nil {
		t.Errorf("meta does not match the document: %v\n%s", err, body)
	}
	var meta struct {
		Product string `json:"product"`
		Build   struct {
			Version string `json:"version"`
		} `json:"build"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Product != httpapi.Product {
		t.Errorf("product = %q", meta.Product)
	}
	if want := os.Getenv("ROTOR_TEST_BUILD_VERSION"); want != "" && meta.Build.Version != want {
		t.Errorf("the api says it is build %q, the checkout made %q", meta.Build.Version, want)
	}
}
