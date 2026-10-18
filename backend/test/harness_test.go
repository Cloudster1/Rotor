//go:build integration

// Package test holds the integration suite. It runs against the stack's real Postgres and api,
// because row level security is a property of the database that cannot be faked.
package test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Environment the harness needs, set by make test-integration.
const (
	envDB        = "ROTOR_DB_URL"
	envSuperuser = "ROTOR_TEST_SUPERUSER_URL"
	envAPI       = "ROTOR_TEST_API_URL"
)

// harness bundles the cluster under test, connected as rotor_app and rotor_admin,
// with a superuser pool used only to arrange and clean up behind the policies.
type harness struct {
	cfg     config.Config
	cluster *db.Cluster
	super   *pgxpool.Pool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	if os.Getenv(envDB) == "" || os.Getenv(envSuperuser) == "" {
		t.Skipf("%s and %s are not set; run the suite with make test-integration", envDB, envSuperuser)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	ctx := context.Background()
	cluster, err := db.Open(ctx, cfg.DB, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open cluster: %v", err)
	}
	super, err := pgxpool.New(ctx, os.Getenv(envSuperuser))
	if err != nil {
		cluster.Close()
		t.Fatalf("connect superuser: %v", err)
	}
	t.Cleanup(func() {
		super.Close()
		cluster.Close()
	})
	return &harness{cfg: cfg, cluster: cluster, super: super}
}

// apiURL is the stack's running api, as the suite reaches it on the stack's network.
func apiURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv(envAPI)
	if url == "" {
		t.Skipf("%s is not set; run the suite with make test-integration", envAPI)
	}
	return strings.TrimSuffix(url, "/")
}

// addPerson makes a person behind the policies' back and removes them, and what names them, after the test.
func (h *harness) addPerson(t *testing.T, role string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	email := "person-" + uuid.NewString() + "@suite.test"
	if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name, role) VALUES ($1, $2, $3) RETURNING id`,
		email, "Test person", role).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := h.super.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, id); err != nil {
			t.Errorf("remove the test person: %v", err)
		}
	})
	return id
}

// permissionDenied fails unless err is the database refusing the privilege itself.
func permissionDenied(t *testing.T, what string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("%s = %v, want permission denied (42501)", what, err)
	}
}
