package oauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/netguard"
)

// Adapted from Exciter at 3b340ca, backend/internal/oauth/service_test.go.
func TestADocumentIsKeptAsItsCacheHeadersAllowAtMostADay(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"":                           0,
		"max-age=300":                5 * time.Minute,
		"public, max-age=604800":     config.OAuthMaxMetadataAge,
		"no-store, max-age=300":      0,
		"no-cache":                   0,
		"s-maxage=60":                0,
		"private, MAX-AGE=60, other": time.Minute,
	} {
		if got := keepFor(header); got != want {
			t.Errorf("%q keeps it %v, want %v", header, got, want)
		}
	}
}

func TestADocumentsAddressIsHttpsWithAPath(t *testing.T) {
	for id, want := range map[string]bool{
		"https://client.example/oauth/metadata.json": true,
		"https://client.example":                     false,
		"https://client.example/":                    false,
		"http://client.example/meta":                 false,
		"https://user@client.example/meta":           false,
		"https://client.example/meta#x":              false,
		"0193a5c4":                                   false,
	} {
		if got := IsMetadataClientID(id); got != want {
			t.Errorf("%s is a document's address: %v, want %v", id, got, want)
		}
	}
}

// documentServer serves what each path answers, over TLS on the loopback, and a fetcher that trusts it; allow
// names what the guard lets through.
func documentServer(t *testing.T, allow string, routes map[string]http.HandlerFunc) (*httptest.Server, Fetcher) {
	t.Helper()
	mux := http.NewServeMux()
	for path, handler := range routes {
		mux.HandleFunc(path, handler)
	}
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	transport := netguard.Transport(netguard.ParseAllow(allow))
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return server, HTTPFetcher(netguard.WithTransport(config.OAuthMetadataFetchTimeout, transport))
}

func TestADocumentIsFetchedOnceItsAddressIsAllowedAndHeldToItsLimits(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"/client.json": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Cache-Control", "max-age=60")
			_, _ = w.Write([]byte(`{"client_id":"x"}`))
		},
		"/moved.json": func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/client.json", http.StatusFound) },
		"/large.json": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", config.OAuthMaxClientMetadataBytes+1)))
		},
	}
	server, fetch := documentServer(t, "127.0.0.0/8, ::1/128", routes)
	body, keep, err := fetch(context.Background(), server.URL+"/client.json")
	if err != nil || string(body) != `{"client_id":"x"}` || keep != time.Minute {
		t.Fatalf("an allowed document reads %q kept %v: %v", body, keep, err)
	}
	for path, says := range map[string]string{"/moved.json": "302", "/large.json": "larger than 5 KiB", "/missing.json": "404"} {
		if _, _, err := fetch(context.Background(), server.URL+path); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%s answers %v, want an error saying %q", path, err, says)
		}
	}

	// Without the operator naming it, the loopback is Rotor's own network, and is never reached.
	guarded, fetchGuarded := documentServer(t, "", routes)
	if _, _, err := fetchGuarded(context.Background(), guarded.URL+"/client.json"); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("a document on the loopback was fetched: %v", err)
	}
}
