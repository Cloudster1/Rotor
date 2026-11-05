package skills

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/audit"
)

// fakeSkill is a skill row as the fake library keeps it.
type fakeSkill struct {
	id      uuid.UUID
	builtin bool
	current int
}

// fakeLibrary answers the statements installing a shipped skill makes, as the tables would.
type fakeLibrary struct {
	skills   map[string]*fakeSkill
	hashes   map[string]string
	files    int
	versions int
	audited  []string
}

func newFakeLibrary() *fakeLibrary {
	return &fakeLibrary{skills: map[string]*fakeSkill{}, hashes: map[string]string{}}
}

func fakeVersionKey(id any, number any) string { return fmt.Sprint(id, "/", number) }

func (f *fakeLibrary) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(sql, "INSERT INTO skill_version"):
		f.versions++
		f.hashes[fakeVersionKey(args[0], args[1])] = args[4].(string)
		for _, k := range f.skills {
			if k.id == args[0] {
				k.current = args[1].(int)
			}
		}
	case strings.Contains(sql, "INSERT INTO skill_file"):
		f.files++
	case strings.Contains(sql, "INSERT INTO audit_log"):
		f.audited = append(f.audited, args[1].(string))
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeLibrary) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("installing reads no rows")
}

func (f *fakeLibrary) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "FROM skill WHERE name"):
		if k, ok := f.skills[args[0].(string)]; ok {
			return fakeRow{values: []any{k.id, k.builtin, k.current}}
		}
	case strings.Contains(sql, "INSERT INTO skill "):
		k := &fakeSkill{id: uuid.New(), builtin: true}
		f.skills[args[0].(string)] = k
		return fakeRow{values: []any{k.id}}
	case strings.Contains(sql, "SELECT hash FROM skill_version"):
		if hash, ok := f.hashes[fakeVersionKey(args[0], args[1])]; ok {
			return fakeRow{values: []any{hash}}
		}
	}
	return fakeRow{err: pgx.ErrNoRows}
}

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		switch d := d.(type) {
		case *uuid.UUID:
			*d = r.values[i].(uuid.UUID)
		case *bool:
			*d = r.values[i].(bool)
		case *int:
			*d = r.values[i].(int)
		case *string:
			*d = r.values[i].(string)
		}
	}
	return nil
}

// shippedSkill is a shipped skill named name at version, whose body tells two shipments apart.
func shippedSkill(t *testing.T, name string, version int, body string) Shipped {
	t.Helper()
	draft, problems := Check([]File{
		{Path: SkillFile, Content: fmt.Sprintf("---\nname: %s\ndescription: Ships.\nmetadata:\n  version: \"%d\"\n---\n\n%s\n", name, version, body)},
		{Path: "references/guide.md", Content: "A guide.\n"},
	})
	if draft == nil {
		t.Fatal(problems)
	}
	return Shipped{Draft: draft, Version: version}
}

func TestAShippedSkillIsMadeThenItsVersionAddedOnceWithItsFiles(t *testing.T) {
	ctx, lib := context.Background(), newFakeLibrary()
	one := shippedSkill(t, "requirements-engineering", 1, "Write well.")
	if outcome, err := installOne(ctx, lib, one); err != nil || outcome != Added {
		t.Fatalf("installing = %s (%v), want added", outcome, err)
	}
	if !lib.skills["requirements-engineering"].builtin || lib.versions != 1 || lib.files != 2 ||
		len(lib.audited) != 1 || lib.audited[0] != audit.ActionSkillInstalled {
		t.Fatalf("after installing: %+v", lib)
	}
	if outcome, err := installOne(ctx, lib, one); err != nil || outcome != Present {
		t.Errorf("installing again = %s (%v), want present", outcome, err)
	}
	if lib.versions != 1 || lib.files != 2 || len(lib.audited) != 1 {
		t.Errorf("installing again wrote something: %+v", lib)
	}
	// An upgrade declares the next version, which joins the first.
	if outcome, err := installOne(ctx, lib, shippedSkill(t, "requirements-engineering", 2, "Write better.")); err != nil || outcome != Added {
		t.Errorf("installing version 2 = %s (%v), want added", outcome, err)
	}
	if lib.versions != 2 || lib.skills["requirements-engineering"].current != 2 {
		t.Errorf("after the upgrade: %+v", lib)
	}
	// An older Rotor starting after a newer one leaves the newer version current.
	older := shippedSkill(t, "test-engineering", 1, "Test.")
	lib.skills["test-engineering"] = &fakeSkill{id: uuid.New(), builtin: true, current: 3}
	if outcome, err := installOne(ctx, lib, older); err != nil || outcome != Superseded {
		t.Errorf("installing below the current version = %s (%v), want superseded", outcome, err)
	}
}

func TestAShippedSkillChangedWithoutANewVersionIsRefused(t *testing.T) {
	ctx, lib := context.Background(), newFakeLibrary()
	if _, err := installOne(ctx, lib, shippedSkill(t, "test-engineering", 1, "Test.")); err != nil {
		t.Fatal(err)
	}
	_, err := installOne(ctx, lib, shippedSkill(t, "test-engineering", 1, "Test differently."))
	if !errors.Is(err, ErrShippedChanged) || !strings.Contains(err.Error(), "test-engineering") {
		t.Fatalf("installing changed files under the same version = %v", err)
	}
	if lib.versions != 1 || len(lib.audited) != 1 {
		t.Errorf("the refused install wrote something: %+v", lib)
	}
}

func TestAShippedSkillNeverTakesOverASkillSomebodyMade(t *testing.T) {
	ctx, lib := context.Background(), newFakeLibrary()
	lib.skills["test-engineering"] = &fakeSkill{id: uuid.New(), current: 4}
	if outcome, err := installOne(ctx, lib, shippedSkill(t, "test-engineering", 1, "Test.")); err != nil || outcome != NameTaken {
		t.Errorf("installing over a person's skill = %s (%v), want name taken", outcome, err)
	}
	if lib.versions != 0 || len(lib.audited) != 0 {
		t.Errorf("installing over a person's skill wrote something: %+v", lib)
	}
}
