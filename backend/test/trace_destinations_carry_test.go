//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/tracedest"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// keptTraces is the version once 01500_keep_traces.sql has run, the last before 01520_trace_destinations.sql.
const (
	keptTraces           = 1500
	destinationsMigrated = 1520
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
	carriedOver      bool
}

func carriedOf(t *testing.T, s *scratch) (carriedRow, bool) {
	t.Helper()
	var r carriedRow
	err := s.super.QueryRow(context.Background(), `SELECT id, name, endpoint, header_names, headers_set_at, enabled, content,
		content_acknowledged_by, content_acknowledged_at, carried_over FROM trace_destination WHERE carried_over`).Scan(&r.id,
		&r.name, &r.endpoint, &r.headerNames, &r.headersSetAt, &r.enabled, &r.content, &r.acknowledgedBy, &r.acknowledgedAt, &r.carriedOver)
	if err != nil {
		return r, false
	}
	return r, true
}

// langSmithBefore sets LangSmith as an installation before #246 had it: on, with a key, as person set it.
func langSmithBefore(t *testing.T, s *scratch, enabled bool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var person uuid.UUID
	if err := s.super.QueryRow(ctx, `INSERT INTO app_user (email, name, standing) VALUES ($1, 'Smith admin', 'member') RETURNING id`,
		"smith-"+uuid.NewString()+"@suite.test").Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := s.super.Exec(ctx, `UPDATE langsmith_setting SET enabled = $1, project = 'carried', key_set_at = now(), changed_by = $2`,
		enabled, person); err != nil {
		t.Fatal(err)
	}
	return person
}

// The migration makes a destination of LangSmith as it was set, and the api's start moves its key once, however many
// replicas start together; without a key the destination waits, off.
func TestTheLangSmithSettingIsCarriedOverIntoADestination(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	secrets, _ := newVault(t, os.Getenv(envOpenBao), config.DefaultOpenBaoRole)
	endpoint := os.Getenv(envToolStubInternal) + "/langsmith/otel/v1/traces"
	t.Cleanup(func() { _ = secrets.DeleteLangSmithKey(context.Background()) })

	s := h.newScratch(t, keptTraces)
	person := langSmithBefore(t, s, true)
	s.up(destinationsMigrated)
	row, ok := carriedOf(t, s)
	if !ok || row.name != tracedest.LangSmithName || row.endpoint != config.DefaultLangSmithEndpoint || !row.enabled || !row.content ||
		row.acknowledgedBy == nil || *row.acknowledgedBy != person || row.acknowledgedAt == nil || row.headersSetAt != nil ||
		strings.Join(row.headerNames, ",") != "Langsmith-Project,x-api-key" {
		t.Fatalf("the migration carried over %+v (%v)", row, ok)
	}
	t.Cleanup(func() { _ = secrets.DeleteDestinationHeaders(context.Background(), row.id) })

	key := "lsv2_pt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, version, err := secrets.LangSmithKey(ctx)
	if err != nil && !errors.Is(err, vault.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := secrets.PutLangSmithKey(ctx, vault.LangSmithKey{Key: key, SetAt: time.Now().UTC()}, version); err != nil {
		t.Fatal(err)
	}
	destinations := tracedest.NewService(s.cluster, secrets, endpoint, slog.New(slog.DiscardHandler))
	// Two replicas starting together: the lock lets one carry it over and the other find it done.
	var wg sync.WaitGroup
	var mu sync.Mutex
	carried := 0
	for range 2 {
		wg.Go(func() {
			done, err := destinations.CarryOver(ctx, secrets)
			if err != nil {
				t.Errorf("carrying over: %v", err)
			}
			if done {
				mu.Lock()
				carried++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if carried != 1 {
		t.Errorf("%d starts carried the setting over, want 1", carried)
	}
	row, _ = carriedOf(t, s)
	if row.endpoint != endpoint || row.headersSetAt == nil || !row.enabled || !row.content {
		t.Errorf("after the start the destination reads %+v", row)
	}
	held, err := secrets.DestinationHeaders(ctx, row.id)
	if err != nil || held.Headers[tracedest.LangSmithKeyHeader] != key || held.Headers[tracedest.LangSmithProjectHeader] != "carried" {
		t.Errorf("the destination's headers are %v (%v)", held.Headers, err)
	}
	if _, _, err := secrets.LangSmithKey(ctx); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("the old key is still held: %v", err)
	}
	var entries string
	if err := s.super.QueryRow(ctx, `SELECT coalesce(string_agg(data::text, ' '), '') FROM audit_log
		WHERE action = 'trace_destination.carried_over'`).Scan(&entries); err != nil || !strings.Contains(entries, `"enabled": true`) ||
		strings.Contains(entries, key) || strings.Count(entries, "{") != 1 {
		t.Errorf("the audit log holds %q (%v)", entries, err)
	}
	if done, err := destinations.CarryOver(ctx, secrets); done || err != nil {
		t.Errorf("a later start carried it over again: %v %v", done, err)
	}

	// An installation whose key OpenBao no longer holds keeps the destination, off, for an administrator to finish.
	lost := h.newScratch(t, keptTraces)
	langSmithBefore(t, lost, true)
	lost.up(destinationsMigrated)
	if done, err := tracedest.NewService(lost.cluster, secrets, endpoint, slog.New(slog.DiscardHandler)).CarryOver(ctx, secrets); !done || err != nil {
		t.Fatalf("carrying over without a key = %v %v", done, err)
	}
	if row, ok := carriedOf(t, lost); !ok || row.enabled || row.content || row.headersSetAt != nil {
		t.Errorf("without a key the destination reads %+v (%v)", row, ok)
	}

	// LangSmith never given a key becomes nothing.
	never := h.newScratch(t, 0)
	if _, ok := carriedOf(t, never); ok {
		t.Error("a LangSmith never given a key became a destination")
	}
	if done, err := tracedest.NewService(never.cluster, secrets, endpoint, slog.New(slog.DiscardHandler)).CarryOver(ctx, secrets); done || err != nil {
		t.Errorf("carrying over nothing = %v %v", done, err)
	}
}
