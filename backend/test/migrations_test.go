//go:build integration

package test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/cloudster1/rotor/backend/migrations"
)

// schemaSQL describes every table, column and policy, so two schemas compare as text.
const schemaSQL = `
	SELECT coalesce(string_agg(line, E'\n' ORDER BY line), '') FROM (
		SELECT format('column %s.%s %s %s', table_name, column_name, data_type, is_nullable) AS line
		FROM information_schema.columns WHERE table_schema = 'public' AND table_name <> 'goose_db_version'
		UNION ALL
		SELECT format('policy %s.%s %s %s', tablename, policyname, cmd, roles) FROM pg_policies WHERE schemaname = 'public'
	) lines`

// Every migration has a working down: up, all the way down and up again leaves the same schema.
func TestMigrationsGoDownAndUpAgain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	name := "rotor_migrations_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := h.super.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := h.super.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Errorf("drop the scratch database: %v", err)
		}
	})

	scratch, err := url.Parse(os.Getenv(envSuperuser))
	if err != nil {
		t.Fatal(err)
	}
	scratch.Path = "/" + name
	pool, err := pgxpool.New(ctx, scratch.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	schema := func() string {
		var s string
		if err := pool.QueryRow(ctx, schemaSQL).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("up: %v", err)
	}
	first := schema()
	if !strings.Contains(first, "policy audit_log.audit_log_admins_read") {
		t.Fatalf("the migrated schema lacks the audit log's policies:\n%s", first)
	}
	if err := goose.DownToContext(ctx, sqlDB, ".", 0); err != nil {
		t.Fatalf("down to 0: %v", err)
	}
	if left := schema(); left != "" {
		t.Errorf("all the way down left:\n%s", left)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if again := schema(); again != first {
		t.Errorf("up again gave another schema:\n%s\nwant\n%s", again, first)
	}
}
