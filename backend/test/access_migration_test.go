//go:build integration

package test

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// accessCheck is one thing a person may or may not do, asked straight through SQL as rotor_app; workflow is uuid.Nil
// for an installation-wide one.
type accessCheck struct {
	name string
	ask  func(ctx context.Context, tx pgx.Tx, person, workflow uuid.UUID) (bool, error)
}

// affected says whether a write went through and touched a row; a refusal is the answer no, not a failure.
func affected(ctx context.Context, tx pgx.Tx, statement string, args ...any) (bool, error) {
	tag, err := tx.Exec(ctx, statement, args...)
	return err == nil && tag.RowsAffected() > 0, nil
}

func counted(ctx context.Context, tx pgx.Tx, query string, args ...any) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, query, args...).Scan(&n)
	return n > 0, err
}

var workflowChecks = []accessCheck{
	{"sees", func(ctx context.Context, tx pgx.Tx, _, w uuid.UUID) (bool, error) {
		return counted(ctx, tx, `SELECT count(*) FROM workflow WHERE id = $1`, w)
	}},
	{"reads versions", func(ctx context.Context, tx pgx.Tx, _, w uuid.UUID) (bool, error) {
		return counted(ctx, tx, `SELECT count(*) FROM workflow_version WHERE workflow_id = $1`, w)
	}},
	{"saves a version", func(ctx context.Context, tx pgx.Tx, p, w uuid.UUID) (bool, error) {
		return affected(ctx, tx, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, created_by)
			VALUES ($1, 2, 'y', '{}', 'h', $2)`, w, p)
	}},
	{"shares", func(ctx context.Context, tx pgx.Tx, _, w uuid.UUID) (bool, error) {
		return affected(ctx, tx, `UPDATE workflow SET sharing = 'everyone', shared_groups = '{}' WHERE id = $1`, w)
	}},
	{"archives", func(ctx context.Context, tx pgx.Tx, _, w uuid.UUID) (bool, error) {
		return affected(ctx, tx, `UPDATE workflow SET archived_at = now() WHERE id = $1`, w)
	}},
	{"starts a run", func(ctx context.Context, tx pgx.Tx, p, w uuid.UUID) (bool, error) {
		return affected(ctx, tx, `INSERT INTO run (workflow_id, version, user_id, data_key, sealed_inputs) VALUES ($1, 1, $2, $3, $4)`,
			w, p, placeholderKey, placeholderSealed)
	}},
	{"sees others' runs", func(ctx context.Context, tx pgx.Tx, _, w uuid.UUID) (bool, error) {
		var visible bool
		err := tx.QueryRow(ctx, `SELECT run_visible(gen_random_uuid(), $1)`, w).Scan(&visible)
		return visible, err
	}},
}

var installationChecks = []accessCheck{
	{"administers Rotor", func(ctx context.Context, tx pgx.Tx, _, _ uuid.UUID) (bool, error) {
		var admin bool
		err := tx.QueryRow(ctx, `SELECT current_actor_is_admin()`).Scan(&admin)
		return admin, err
	}},
	{"reads the audit log", func(ctx context.Context, tx pgx.Tx, _, _ uuid.UUID) (bool, error) {
		return counted(ctx, tx, `SELECT count(*) FROM audit_log`)
	}},
	{"changes the usage limits", func(ctx context.Context, tx pgx.Tx, _, _ uuid.UUID) (bool, error) {
		return affected(ctx, tx, `UPDATE usage_limit SET updated_at = now()`)
	}},
	{"creates a workflow", func(ctx context.Context, tx pgx.Tx, p, _ uuid.UUID) (bool, error) {
		return affected(ctx, tx, `INSERT INTO workflow (name, owner_id) VALUES ('new', $1)`, p)
	}},
}

// accessFixture is people and workflows as an installation before #202 holds them.
type accessFixture struct {
	people    map[string]uuid.UUID
	workflows map[string]uuid.UUID
}

func seedAccessFixture(ctx context.Context, t *testing.T, pool *pgxpool.Pool) accessFixture {
	t.Helper()
	f := accessFixture{people: map[string]uuid.UUID{}, workflows: map[string]uuid.UUID{}}
	for name, person := range map[string]struct {
		role   string
		groups []string
	}{
		"admin":     {"admin", []string{"rotor-administrators", "engineering"}},
		"owner":     {"member", []string{"developers"}},
		"developer": {"member", []string{"developers", "quality"}},
		"stranger":  {"member", []string{"readers"}},
	} {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO app_user (email, name, role, groups) VALUES ($1, $2, $3, $4) RETURNING id`,
			name+"@suite.test", name, person.role, person.groups).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.people[name] = id
	}
	for name, w := range map[string]struct {
		owner, sharing string
		groups         []string
	}{
		"private":        {"owner", "private", []string{}},
		"developers":     {"owner", "groups", []string{"developers"}},
		"nobody's group": {"owner", "groups", []string{"nobody-here"}},
		"everyone":       {"owner", "everyone", []string{}},
		"the admin's":    {"admin", "private", []string{}},
	} {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO workflow (name, owner_id, sharing, shared_groups) VALUES ($1, $2, $3, $4) RETURNING id`,
			name, f.people[w.owner], w.sharing, w.groups).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, created_by)
			VALUES ($1, 1, 'x', '{}', 'h', $2)`, id, f.people[w.owner]); err != nil {
			t.Fatal(err)
		}
		f.workflows[name] = id
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_log (action, target_type, target_id) VALUES ('workflow.created', 'workflow', 'seeded')`); err != nil {
		t.Fatal(err)
	}
	return f
}

// access answers every check for every person, as "person: check on workflow".
func (f accessFixture) access(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]bool {
	t.Helper()
	return f.accessBy(t, ctx, pool, workflowChecks)
}

// accessBy is access with the workflow checks given, for a schema that cannot ask all of them.
func (f accessFixture) accessBy(t *testing.T, ctx context.Context, pool *pgxpool.Pool, checks []accessCheck) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	ask := func(key string, check accessCheck, person, workflow uuid.UUID) {
		var answer bool
		if err := asApp(ctx, pool, person, func(tx pgx.Tx) error {
			var err error
			answer, err = check.ask(ctx, tx, person, workflow)
			return err
		}); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		out[key] = answer
	}
	for name, person := range f.people {
		for _, check := range installationChecks {
			ask(fmt.Sprintf("%s: %s", name, check.name), check, person, uuid.Nil)
		}
		for workflowName, workflow := range f.workflows {
			for _, check := range checks {
				ask(fmt.Sprintf("%s: %s on %s", name, check.name, workflowName), check, person, workflow)
			}
		}
	}
	return out
}

// Each fixture person may do the same straight through SQL as rotor_app before the migration and after, but for what
// #202 means to change: Administrator holds every permission, so an administrator edits and shares every workflow.
func TestTheMigrationKeepsEveryonesAccess(t *testing.T) {
	h := newHarness(t)
	s := h.newScratch(t, lastBeforeAccess)
	ctx := context.Background()
	f := seedAccessFixture(ctx, t, s.super)

	before := f.access(t, ctx, s.super)
	for workflowName, w := range f.workflows {
		// Before #202 the api archived anyone's workflow for an administrator through rotor_admin.
		var ownedByAdmin bool
		if err := s.super.QueryRow(ctx, `SELECT owner_id = $2 FROM workflow WHERE id = $1`, w, f.people["admin"]).Scan(&ownedByAdmin); err != nil {
			t.Fatal(err)
		}
		before["admin: archives on "+workflowName] = true
		if !ownedByAdmin {
			for _, gained := range []string{"saves a version", "shares"} {
				key := fmt.Sprintf("admin: %s on %s", gained, workflowName)
				if before[key] {
					t.Errorf("before the migration %s already", key)
				}
				before[key] = true
			}
		}
	}

	s.up(accessMigrated)
	after := f.access(t, ctx, s.super)

	var changed []string
	for key, was := range before {
		if after[key] != was {
			changed = append(changed, fmt.Sprintf("%s: %v before, %v after", key, was, after[key]))
		}
	}
	sort.Strings(changed)
	for _, c := range changed {
		t.Error(c)
	}
	if len(before) != len(after) || len(before) == 0 {
		t.Errorf("%d answers before, %d after", len(before), len(after))
	}
	// The fixture asks something worth asking: the owner, a group share, an everyone share and the administrator.
	for key, want := range map[string]bool{
		"developer: starts a run on developers": true, "stranger: sees on developers": false,
		"stranger: starts a run on everyone": true, "developer: saves a version on developers": false,
		"owner: shares on private": true, "admin: administers Rotor": true, "owner: reads the audit log": false,
		"stranger: sees on nobody's group": false, "developer: sees others' runs on developers": false,
	} {
		if after[key] != want {
			t.Errorf("after the migration %s is %v, want %v", key, after[key], want)
		}
	}
}
