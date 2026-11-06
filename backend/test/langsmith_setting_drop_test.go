//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// keptTraces is the version once 01500_keep_traces.sql has run, the last before 01520_trace_destinations.sql;
// langSmithDropped is the one once 01540_drop_langsmith_setting.sql has.
const (
	keptTraces           = 1500
	destinationsMigrated = 1520
	langSmithDropped     = 1540
)

// carriedRow is the destination migration 01520 made of the LangSmith setting.
type carriedRow struct {
	id               uuid.UUID
	name, endpoint   string
	headerNames      []string
	headersSetAt     *time.Time
	enabled, content bool
	acknowledgedBy   *uuid.UUID
	acknowledgedAt   *time.Time
}

func carriedOf(t *testing.T, s *scratch) (carriedRow, bool) {
	t.Helper()
	var r carriedRow
	err := s.super.QueryRow(context.Background(), `SELECT id, name, endpoint, header_names, headers_set_at, enabled, content,
		content_acknowledged_by, content_acknowledged_at FROM trace_destination WHERE carried_over`).Scan(&r.id,
		&r.name, &r.endpoint, &r.headerNames, &r.headersSetAt, &r.enabled, &r.content, &r.acknowledgedBy, &r.acknowledgedAt)
	return r, err == nil
}

// langSmithBefore sets LangSmith as an installation before #246 had it: on, with a key, as person set it.
func langSmithBefore(t *testing.T, s *scratch) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var person uuid.UUID
	if err := s.super.QueryRow(ctx, `INSERT INTO app_user (email, name, standing) VALUES ($1, 'Smith admin', 'member') RETURNING id`,
		"smith-"+uuid.NewString()+"@suite.test").Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := s.super.Exec(ctx, `UPDATE langsmith_setting SET enabled = true, project = 'carried', key_set_at = now(), changed_by = $1`,
		person); err != nil {
		t.Fatal(err)
	}
	return person
}

func tableExists(t *testing.T, s *scratch, name string) bool {
	t.Helper()
	var found bool
	if err := s.super.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&found); err != nil {
		t.Fatal(err)
	}
	return found
}

// LangSmith becomes a destination as it was set (#246), and its setting is dropped (#256) only once #246's start has
// moved the key out of OpenBao, which SQL cannot do; going back down gives #246 a row with nothing left to move.
func TestTheLangSmithSettingIsDroppedOnlyOnceCarriedOver(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	s := h.newScratch(t, keptTraces)
	person := langSmithBefore(t, s)
	s.up(destinationsMigrated)
	row, ok := carriedOf(t, s)
	if !ok || row.name != "LangSmith" || row.endpoint != "https://api.smith.langchain.com/otel/v1/traces" || !row.enabled ||
		!row.content || row.acknowledgedBy == nil || *row.acknowledgedBy != person || row.acknowledgedAt == nil ||
		row.headersSetAt != nil || strings.Join(row.headerNames, ",") != "Langsmith-Project,x-api-key" {
		t.Fatalf("the migration carried over %+v (%v)", row, ok)
	}

	err := s.tryUp(langSmithDropped)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "55000" || !strings.Contains(pg.Message, "Start the previous version") {
		t.Fatalf("dropping the setting before its key moved = %v, want it refused", err)
	}
	if !tableExists(t, s, "langsmith_setting") {
		t.Fatal("the refused migration dropped the setting anyway")
	}

	// What #246's start leaves once it moved the key.
	if _, err := s.super.Exec(ctx, `UPDATE langsmith_setting SET carried_at = now()`); err != nil {
		t.Fatal(err)
	}
	s.up(langSmithDropped)
	if tableExists(t, s, "langsmith_setting") || tableExists(t, s, "trace_destination_one_carried") {
		t.Error("the migration left the LangSmith setting or the index its routes read by")
	}
	if after, ok := carriedOf(t, s); !ok || after.id != row.id || !after.enabled {
		t.Errorf("dropping the setting changed its destination to %+v (%v)", after, ok)
	}

	s.down(toolNamesMigrated)
	var rows int
	var carried *time.Time
	if err := s.super.QueryRow(ctx, `SELECT count(*), max(carried_at) FROM langsmith_setting`).Scan(&rows, &carried); err != nil ||
		rows != 1 || carried == nil {
		t.Errorf("going back down left %d rows carried at %v (%v), want one carried over", rows, carried, err)
	}
	s.up(langSmithDropped)

	// An installation that never gave LangSmith a key has nothing to move, so the setting goes at once.
	never := h.newScratch(t, toolNamesMigrated)
	if _, ok := carriedOf(t, never); ok {
		t.Error("a LangSmith never given a key became a destination")
	}
	never.up(langSmithDropped)
}
