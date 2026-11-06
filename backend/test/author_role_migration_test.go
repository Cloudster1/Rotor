//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/perm"
)

// authorSaysSkills is the version once 01420_author_role_skills.sql has run; skillAccessMigrated is the one before it.
const (
	authorSaysSkills    = 1420
	skillAccessMigrated = 1410
)

// authorWords is how the database describes the built-in Author role.
func authorWords(t *testing.T, s *scratch) string {
	t.Helper()
	var words string
	if err := s.super.QueryRow(context.Background(), `SELECT description FROM role WHERE key = 'author'`).Scan(&words); err != nil {
		t.Fatal(err)
	}
	return words
}

// The Author role says it creates skills once the migration runs, going back down puts the old words back, and an
// administrator's own words for it are kept either way.
func TestTheAuthorRoleSaysItCreatesSkills(t *testing.T) {
	h := newHarness(t)
	var want string
	for _, d := range perm.Builtins() {
		if d.Key == perm.Author {
			want = d.Description
		}
	}
	const shipped, own = "Creates workflows of their own.", "Writes workflows and skills."

	s := h.newScratch(t, skillAccessMigrated)
	if got := authorWords(t, s); got != shipped {
		t.Fatalf("before the migration Author reads %q", got)
	}
	s.up(authorSaysSkills)
	if got := authorWords(t, s); got != want {
		t.Errorf("after the migration Author reads %q, want %q", got, want)
	}
	s.down(skillAccessMigrated)
	if got := authorWords(t, s); got != shipped {
		t.Errorf("going back down Author reads %q, want %q", got, shipped)
	}

	if _, err := s.super.Exec(context.Background(), `UPDATE role SET description = $1 WHERE key = 'author'`, own); err != nil {
		t.Fatal(err)
	}
	s.up(authorSaysSkills)
	if got := authorWords(t, s); got != own {
		t.Errorf("an administrator's words for Author became %q", got)
	}

	var installed string
	if err := h.super.QueryRow(context.Background(), `SELECT description FROM role WHERE key = 'author'`).Scan(&installed); err != nil || installed != want {
		t.Errorf("the installation's Author reads %q (%v), want %q", installed, err, want)
	}
}
