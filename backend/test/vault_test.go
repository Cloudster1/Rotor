//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	openbao "github.com/openbao/openbao/api/v2"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/httpapi"
	"github.com/cloudster1/rotor/backend/internal/oidc"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Where the suite finds OpenBao, set by make test-integration; the secret is the realm's, from realm.json.
const (
	envOpenBao       = "ROTOR_TEST_OPENBAO_URL"
	envOpenBaoSealed = "ROTOR_TEST_OPENBAO_SEALED_URL"
	envOpenBaoRoot   = "ROTOR_TEST_OPENBAO_ROOT_TOKEN"
	controlPlaneID   = "rotor-control-plane"
	controlPlaneKey  = "rotor-control-plane-dev-secret"
)

// lockedBuffer collects a client's log lines from the goroutine that renews its token.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) count(s string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buf.String(), s)
}

// newVault is the api's OpenBao client, signing in as the realm's control plane through role.
func newVault(t *testing.T, addr, role string) (*vault.Client, *lockedBuffer) {
	t.Helper()
	for _, key := range []string{envOpenBao, envKeycloak, envKeycloakPublic} {
		if os.Getenv(key) == "" {
			t.Skipf("%s is not set; run the suite with make test-integration", key)
		}
	}
	logs := &lockedBuffer{}
	client, err := vault.New(vault.Config{
		OpenBao: config.OpenBao{Addr: addr, Role: role, Mount: config.DefaultOpenBaoMount, ClientID: controlPlaneID, ClientSecret: controlPlaneKey},
		Issuer:  os.Getenv(envKeycloakPublic) + "/realms/" + realm,
		HTTP:    oidc.HTTPClient(map[string]string{os.Getenv(envKeycloakPublic): os.Getenv(envKeycloak)}),
		Log:     slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, logs
}

// rootBao arranges what the test needs behind the policies, as an operator would.
func rootBao(t *testing.T) *openbao.Client {
	t.Helper()
	settings := openbao.DefaultConfig()
	settings.Address = os.Getenv(envOpenBao)
	client, err := openbao.NewClient(settings)
	if err != nil {
		t.Fatal(err)
	}
	client.SetToken(os.Getenv(envOpenBaoRoot))
	return client
}

func TestTheControlPlaneKeepsSecretsWithCheckAndSet(t *testing.T) {
	client, _ := newVault(t, os.Getenv(envOpenBao), config.DefaultOpenBaoRole)
	ctx := context.Background()
	person, tool := uuid.New(), uuid.New()
	t.Cleanup(func() { _ = client.DeleteConnection(ctx, person, tool); _ = client.DeleteToolClient(ctx, tool) })

	first := vault.ConnectionSecret{RefreshToken: "rt-" + uuid.NewString(), Scope: "mcp", ClientID: "c1", ObtainedAt: time.Now().UTC().Truncate(time.Second)}
	version, err := client.PutConnection(ctx, person, tool, first, 0)
	if err != nil || version != 1 {
		t.Fatalf("the first write = version %d, %v", version, err)
	}
	got, read, err := client.Connection(ctx, person, tool)
	if err != nil || read != 1 || got != first {
		t.Fatalf("read back %+v version %d, %v; want %+v", got, read, err, first)
	}
	if _, err := client.PutConnection(ctx, person, tool, first, 0); !errors.Is(err, vault.ErrConflict) {
		t.Errorf("a second first write = %v, want ErrConflict", err)
	}
	second := first
	second.RefreshToken = "rt-" + uuid.NewString()
	if version, err := client.PutConnection(ctx, person, tool, second, read); err != nil || version != 2 {
		t.Errorf("a write over the version read = %d, %v", version, err)
	}
	if _, err := client.PutConnection(ctx, person, tool, first, read); !errors.Is(err, vault.ErrConflict) {
		t.Errorf("a write over a stale version = %v, want ErrConflict", err)
	}

	if err := client.DeleteConnection(ctx, person, tool); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Connection(ctx, person, tool); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("a deleted connection reads %v, want ErrNotFound", err)
	}
	if err := client.DeleteConnection(ctx, person, tool); err != nil {
		t.Errorf("deleting what is gone = %v, want no error", err)
	}

	secret := vault.ToolClient{ClientID: "rotor-at-tool", ClientSecret: "cs-" + uuid.NewString(), AuthMethod: "client_secret_basic"}
	if _, err := client.PutToolClient(ctx, tool, secret, 0); err != nil {
		t.Fatal(err)
	}
	if got, _, err := client.ToolClient(ctx, tool); err != nil || got.ClientSecret != secret.ClientSecret {
		t.Errorf("the tool client reads %+v, %v", got, err)
	}
	if _, _, err := client.Connection(ctx, uuid.Nil, tool); err == nil {
		t.Error("a connection without a person was looked up")
	}
}

// The policy is the guard, not the client's typed API: a token of the role itself is refused elsewhere.
func TestTheControlPlaneIsRefusedOutsideConnectionsAndTools(t *testing.T) {
	newVault(t, os.Getenv(envOpenBao), config.DefaultOpenBaoRole)
	token := controlPlaneToken(t)
	raw := rootBao(t)
	raw.SetToken(token)
	ctx := context.Background()

	if _, err := raw.Logical().WriteWithContext(ctx, "rotor/data/connections/"+uuid.NewString()+"/"+uuid.NewString(),
		map[string]any{"options": map[string]any{"cas": 0}, "data": map[string]any{"refresh_token": "x"}}); err != nil {
		t.Fatalf("the role cannot write a connection: %v", err)
	}
	for what, call := range map[string]func() error{
		"another KV path": func() error {
			_, err := raw.Logical().WriteWithContext(ctx, "rotor/data/checkpoints/x", map[string]any{"options": map[string]any{"cas": 0}, "data": map[string]any{"a": "b"}})
			return err
		},
		"the transit key's settings": func() error {
			_, err := raw.Logical().WriteWithContext(ctx, "transit/keys/"+config.RunDataTransitKey+"/config", map[string]any{"deletion_allowed": true})
			return err
		},
		"encrypting with the run data key": func() error {
			_, err := raw.Logical().WriteWithContext(ctx, "transit/encrypt/"+config.RunDataTransitKey, map[string]any{"plaintext": "eA=="})
			return err
		},
		"data keys of another transit key": func() error {
			_, err := raw.Logical().WriteWithContext(ctx, "transit/datakey/plaintext/another", map[string]any{"bits": "256"})
			return err
		},
		"the policies": func() error {
			_, err := raw.Logical().ReadWithContext(ctx, "sys/policies/acl/rotor-control-plane")
			return err
		},
		"the auth config": func() error {
			_, err := raw.Logical().WriteWithContext(ctx, "auth/jwt/role/rotor-control-plane", map[string]any{"token_ttl": "999h"})
			return err
		},
	} {
		var refused *openbao.ResponseError
		if err := call(); !errors.As(err, &refused) || refused.StatusCode != http.StatusForbidden {
			t.Errorf("the control plane reached %s: %v", what, err)
		}
	}
}

// controlPlaneToken logs in as the role the api uses and returns the OpenBao token, for probing the policy.
func controlPlaneToken(t *testing.T) string {
	t.Helper()
	form := strings.NewReader("grant_type=client_credentials&client_id=" + controlPlaneID + "&client_secret=" + controlPlaneKey)
	req, _ := http.NewRequest(http.MethodPost, os.Getenv(envKeycloak)+"/realms/"+realm+"/protocol/openid-connect/token", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil || answer.AccessToken == "" {
		t.Fatalf("the realm's token for the control plane: %d, %v", resp.StatusCode, err)
	}
	secret, err := rootBao(t).Logical().Write("auth/jwt/login", map[string]any{"role": config.DefaultOpenBaoRole, "jwt": answer.AccessToken})
	if err != nil || secret.Auth == nil {
		t.Fatalf("log in as the control plane: %v", err)
	}
	return secret.Auth.ClientToken
}

func TestTheControlPlaneRenewsItsTokenAndSignsInAgainWhenItEnds(t *testing.T) {
	role := "rotor-control-plane-short-" + uuid.NewString()[:8]
	root := rootBao(t)
	if os.Getenv(envOpenBaoRoot) == "" {
		t.Skipf("%s is not set; run the suite with make test-integration", envOpenBaoRoot)
	}
	if _, err := root.Logical().Write("auth/jwt/role/"+role, map[string]any{
		"role_type": "jwt", "bound_audiences": []string{"openbao"}, "bound_claims": map[string]any{"azp": controlPlaneID},
		"user_claim": "azp", "token_policies": []string{"rotor-control-plane"}, "token_ttl": "2s", "token_max_ttl": "5s",
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.Logical().Delete("auth/jwt/role/" + role) })

	client, logs := newVault(t, os.Getenv(envOpenBao), role)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)

	person, tool := uuid.New(), uuid.New()
	t.Cleanup(func() { _ = client.DeleteConnection(context.Background(), person, tool) })
	const renewed, ended = "renewed the OpenBao token", "at the end of its life"
	version, writesAfterEnd := 0, 0
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && writesAfterEnd < 3 {
		var err error
		if version, err = client.PutConnection(ctx, person, tool, vault.ConnectionSecret{RefreshToken: "x"}, version); err != nil {
			t.Fatalf("a write after %d renewals and %d ends failed: %v", logs.count(renewed), logs.count(ended), err)
		}
		if logs.count(ended) > 0 {
			writesAfterEnd++
		}
		time.Sleep(500 * time.Millisecond)
	}
	if logs.count(renewed) < 1 || writesAfterEnd < 3 {
		t.Fatalf("within 15 seconds the client renewed %d times and its token ended %d times; want a renewal, an end and writes after it",
			logs.count(renewed), logs.count(ended))
	}
}

func TestReadinessFollowsOpenBao(t *testing.T) {
	h := newHarness(t)
	if os.Getenv(envOpenBaoSealed) == "" {
		t.Skipf("%s is not set; run the suite with make test-integration", envOpenBaoSealed)
	}
	for name, tc := range map[string]struct {
		addr   string
		err    error
		status int
	}{
		"unsealed":    {os.Getenv(envOpenBao), nil, http.StatusOK},
		"sealed":      {os.Getenv(envOpenBaoSealed), vault.ErrSealed, http.StatusServiceUnavailable},
		"unreachable": {"http://127.0.0.1:1", vault.ErrUnreachable, http.StatusServiceUnavailable},
	} {
		client, _ := newVault(t, tc.addr, config.DefaultOpenBaoRole)
		if err := client.Ready(context.Background()); !errors.Is(err, tc.err) {
			t.Errorf("%s: Ready = %v, want %v", name, err, tc.err)
		}
		server := &httpapi.Server{DB: h.cluster, Vault: client, Log: slog.New(slog.DiscardHandler)}
		rec := httptest.NewRecorder()
		server.Routes(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != tc.status {
			t.Errorf("%s: /readyz = %d, want %d", name, rec.Code, tc.status)
		}
	}
}

// The development OpenBao keeps an audit log, declared in its configuration as an installation's must be (#92).
func TestOpenBaoAuditsEveryRequest(t *testing.T) {
	devices, err := rootBao(t).Sys().ListAudit()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) == 0 {
		t.Fatal("OpenBao keeps no audit log, so nobody could tell who read Rotor's secrets")
	}
	for path, device := range devices {
		if device.Type != "file" {
			t.Errorf("the audit device %s is a %s, not the declared file", path, device.Type)
		}
	}
}
