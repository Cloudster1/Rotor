//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/migrations"
)

// accessMigrated is the version once 01300_access_grants.sql and 01310_access_policies.sql have run; lastBeforeAccess
// is the one before them.
const (
	accessMigrated   = 1310
	lastBeforeAccess = 1260
)

// scratch is a database of its own, for what the shared one cannot show: a state before a migration, or an
// installation with exactly one administrator grant.
type scratch struct {
	super   *pgxpool.Pool
	cluster *db.Cluster
	up      func(version int64)
	down    func(version int64)
}

// newScratch makes an empty database migrated up to version, or all the way for 0, dropped when the test ends,
// reached as the superuser, and as rotor_app and rotor_admin through a cluster like the api's.
func (h *harness) newScratch(t *testing.T, version int64) *scratch {
	t.Helper()
	ctx := context.Background()
	name := "rotor_access_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := h.super.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := h.super.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Errorf("drop the scratch database: %v", err)
		}
	})
	at := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + name
		return u.String()
	}
	super, err := pgxpool.New(ctx, at(os.Getenv(envSuperuser)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(super.Close)
	sqlDB := stdlib.OpenDBFromPool(super)
	t.Cleanup(func() { _ = sqlDB.Close() })
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	s := &scratch{super: super}
	s.up = func(version int64) {
		t.Helper()
		migrate := func() error { return goose.UpToContext(ctx, sqlDB, ".", version) }
		if version == 0 {
			migrate = func() error { return goose.UpContext(ctx, sqlDB, ".") }
		}
		if err := migrate(); err != nil {
			t.Fatalf("migrate the scratch database to %d: %v", version, err)
		}
	}
	s.down = func(version int64) {
		t.Helper()
		if err := goose.DownToContext(ctx, sqlDB, ".", version); err != nil {
			t.Fatalf("migrate the scratch database down to %d: %v", version, err)
		}
	}
	s.up(version)
	cfg := h.cfg.DB
	cfg.URL, cfg.AdminURL = at(cfg.URL), at(cfg.AdminURL)
	cluster, err := db.Open(ctx, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open the scratch cluster: %v", err)
	}
	t.Cleanup(cluster.Close)
	s.cluster = cluster
	return s
}

// asApp runs fn straight through SQL as rotor_app acting for person, in a transaction it always rolls back, so a
// check leaves nothing behind for the next one.
func asApp(ctx context.Context, pool *pgxpool.Pool, person uuid.UUID, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE rotor_app`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, person.String()); err != nil {
		return err
	}
	return fn(tx)
}

// refusedBy fails unless err is the database refusing with the SQLSTATE given, and, when named, that constraint.
func refusedBy(t *testing.T, what string, err error, code, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || (constraint != "" && pgErr.ConstraintName != constraint) {
		t.Errorf("%s = %v, want the database to refuse it with %s %s", what, err, code, constraint)
	}
}
