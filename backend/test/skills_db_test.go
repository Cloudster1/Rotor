//go:build integration

package test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// skillMDOf is the SKILL.md of a skill named name.
func skillMDOf(name string) string {
	return "---\nname: " + name + "\ndescription: Made behind the policies.\n---\nBody\n"
}

func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// addSkillVersion writes a version with its SKILL.md in the transaction given.
func addSkillVersion(ctx context.Context, tx db.DBTX, skill uuid.UUID, number int, name string, author *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `INSERT INTO skill_version (skill_id, number, front_matter, description, hash, bytes, created_by)
		VALUES ($1, $2, '{}', 'Made behind the policies.', 'h', 0, $3)`, skill, number, author); err != nil {
		return err
	}
	content := skillMDOf(name)
	_, err := tx.Exec(ctx, `INSERT INTO skill_file (skill_id, number, path, content, bytes, sha256) VALUES ($1, $2, 'SKILL.md', $3, $4, $5)`,
		skill, number, content, len(content), sha(content))
	return err
}

// ownedSkill makes a skill behind the policies' back with its first version; its owner administers it by the trigger.
func (h *harness) ownedSkill(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	name := skillName("guarded")
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO skill (name, owner_id, owner_name) VALUES ($1, $2, 'Owner') RETURNING id`, name, owner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM skill WHERE id = $1`, id) })
	if err := addSkillVersion(ctx, h.super, id, 1, name, &owner); err != nil {
		t.Fatal(err)
	}
	return id
}

// builtinSkill installs a built-in skill as rotor_admin does at the api's start, at the version given (#225).
func (h *harness) builtinSkill(t *testing.T, name string, number int) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.Must(uuid.NewV7())
	if err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO skill (id, name, builtin) VALUES ($1, $2, true)`, id, name); err != nil {
			return err
		}
		return addSkillVersion(ctx, tx, id, number, name, nil)
	}); err != nil {
		t.Fatalf("installing a built-in skill as rotor_admin: %v", err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM skill WHERE id = $1`, id) })
	return id.String()
}

// The api's checks are the database's too: each write tried straight through SQL as rotor_app is refused.
func TestTheDatabaseKeepsSkillsToTheirGrants(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, editor, reader, outsider := h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	skill := h.ownedSkill(t, owner)
	for person, role := range map[uuid.UUID]string{editor: "skill_editor", reader: "skill_reader"} {
		if _, err := h.super.Exec(ctx, `INSERT INTO role_assignment (role, skill_id, user_id) VALUES ($1, $2, $3)`, role, skill, person); err != nil {
			t.Fatal(err)
		}
	}
	as := func(person uuid.UUID, statement string, args ...any) (int64, error) {
		var touched int64
		err := asApp(ctx, h.super, person, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, statement, args...)
			touched = tag.RowsAffected()
			return err
		})
		return touched, err
	}

	var seen int
	if err := asApp(ctx, h.super, outsider, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM skill WHERE id = $1) + (SELECT count(*) FROM skill_version WHERE skill_id = $1)
			+ (SELECT count(*) FROM skill_file WHERE skill_id = $1)`, skill).Scan(&seen)
	}); err != nil || seen != 0 {
		t.Errorf("an outsider sees %d rows of a skill not shared with them (%v)", seen, err)
	}
	if err := asApp(ctx, h.super, reader, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skill).Scan(&seen)
	}); err != nil || seen != 1 {
		t.Errorf("the reader sees %d files of the skill (%v), want 1", seen, err)
	}

	_, err := as(outsider, `INSERT INTO skill (name, owner_id) VALUES ($1, $2)`, skillName("someone-elses"), owner)
	permissionDenied(t, "a skill made in another person's name", err)
	_, err = as(outsider, `INSERT INTO skill (name, builtin) VALUES ($1, true)`, skillName("posing"))
	permissionDenied(t, "a built-in skill made through rotor_app", err)
	if touched, err := as(outsider, `INSERT INTO skill (name, owner_id) VALUES ($1, $2)`, skillName("own"), outsider); err != nil || touched != 1 {
		t.Errorf("an author making a skill of their own: %d, %v", touched, err)
	}

	// The reader cannot see the skill to number a version after it, so the numbering refuses before the policy does.
	if _, err := as(reader, `INSERT INTO skill_version (skill_id, number, front_matter, description, hash, bytes, created_by)
		VALUES ($1, 2, '{}', 'd', 'h', 0, $2)`, skill, reader); err == nil {
		t.Error("a reader added a version as rotor_app")
	}
	_, err = as(editor, `INSERT INTO skill_version (skill_id, number, front_matter, description, hash, bytes, created_by)
		VALUES ($1, 2, '{}', 'd', 'h', 0, $2)`, skill, owner)
	permissionDenied(t, "an editor adding a version in the owner's name", err)
	if touched, err := as(editor, `INSERT INTO skill_version (skill_id, number, front_matter, description, hash, bytes, created_by)
		VALUES ($1, 2, '{}', 'd', 'h', 0, $2)`, skill, editor); err != nil || touched != 1 {
		t.Errorf("an editor adding the next version: %d, %v", touched, err)
	}
	if _, err := as(owner, `INSERT INTO skill_version (skill_id, number, front_matter, description, hash, bytes, created_by)
		VALUES ($1, 3, '{}', 'd', 'h', 0, $2)`, skill, owner); err == nil {
		t.Error("a version skipped a number")
	}

	_, err = as(owner, `UPDATE skill_version SET comment = 'changed' WHERE skill_id = $1`, skill)
	permissionDenied(t, "the owner rewriting a version", err)
	_, err = as(owner, `DELETE FROM skill_version WHERE skill_id = $1`, skill)
	permissionDenied(t, "the owner deleting a version", err)
	_, err = as(owner, `UPDATE skill_file SET content = 'changed' WHERE skill_id = $1`, skill)
	permissionDenied(t, "the owner rewriting a file", err)
	_, err = as(owner, `DELETE FROM skill WHERE id = $1`, skill)
	permissionDenied(t, "the owner deleting a skill", err)
	_, err = as(owner, `INSERT INTO skill_file (skill_id, number, path, content, bytes, sha256) VALUES ($1, 1, 'late.md', 'x', 1, $2)`, skill, sha("x"))
	permissionDenied(t, "the owner adding a file to a saved version", err)
	_, err = as(owner, `UPDATE skill SET name = $2 WHERE id = $1`, skill, skillName("renamed"))
	refusedBy(t, "the owner renaming a skill", err, "23514", "skill_fixed")

	if touched, err := as(reader, `UPDATE skill SET archived_at = now() WHERE id = $1`, skill); err != nil || touched != 0 {
		t.Errorf("a reader archived a skill: %d rows, %v", touched, err)
	}
	_, err = as(editor, `UPDATE skill SET archived_at = now() WHERE id = $1`, skill)
	refusedBy(t, "an editor archiving a skill", err, "42501", "skill_change_guard")
	if touched, err := as(owner, `UPDATE skill SET archived_at = now() WHERE id = $1`, skill); err != nil || touched != 1 {
		t.Errorf("the owner archiving: %d, %v", touched, err)
	}

	_, err = as(reader, `INSERT INTO role_assignment (role, skill_id, user_id) VALUES ('skill_editor', $1, $2)`, skill, reader)
	permissionDenied(t, "a reader granting themselves Skill editor", err)
	_, err = as(editor, `INSERT INTO role_assignment (role, skill_id, user_id) VALUES ('skill_reader', $1, $2)`, skill, outsider)
	permissionDenied(t, "an editor sharing the skill", err)
	if touched, err := as(owner, `INSERT INTO role_assignment (role, skill_id, user_id) VALUES ('skill_reader', $1, $2)`, skill, outsider); err != nil || touched != 1 {
		t.Errorf("the owner sharing the skill: %d, %v", touched, err)
	}
	_, err = as(owner, `INSERT INTO role_assignment (role, skill_id, user_id) VALUES ('author', $1, $2)`, skill, outsider)
	refusedBy(t, "an installation-wide role granted on a skill", err, "23514", "role_assignment_guard")
	_, err = as(owner, `INSERT INTO role_assignment (role, workflow_id, skill_id, user_id) VALUES ('skill_reader', $1, $2, $3)`,
		h.ownedWorkflow(t, owner), skill, outsider)
	refusedBy(t, "a grant on a workflow and a skill at once", err, "23514", "role_assignment_one_scope")
}

// A grant on a skill answers there alone: a role that also holds workflow words never reaches a workflow through it.
func TestASkillGrantNeverReachesAWorkflow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, person := h.addPerson(t, "member"), h.addPerson(t, "member")
	skill, workflow := h.ownedSkill(t, owner), h.ownedWorkflow(t, owner)
	key := "mixed_" + uuid.NewString()[:8]
	if _, err := h.super.Exec(ctx, `INSERT INTO role (key, name, permissions) VALUES ($1, $1, '{skill.read,workflow.read,rotor.administer}')`, key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM role WHERE key = $1`, key) })
	if _, err := h.super.Exec(ctx, `INSERT INTO role_assignment (role, skill_id, user_id) VALUES ($1, $2, $3)`, key, skill, person); err != nil {
		t.Fatal(err)
	}
	var onSkill, onWorkflow, administers bool
	if err := asApp(ctx, h.super, person, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT actor_can_skill('skill.read', $1), actor_can('workflow.read', $2), actor_can('rotor.administer', NULL)`,
			skill, workflow).Scan(&onSkill, &onWorkflow, &administers)
	}); err != nil {
		t.Fatal(err)
	}
	if !onSkill || onWorkflow || administers {
		t.Errorf("a grant on a skill answers on it %v, on a workflow %v, over the installation %v; want true, false, false",
			onSkill, onWorkflow, administers)
	}
}

func TestTheDatabaseLetsNobodyChangeABuiltinSkill(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	member, admin := h.addPerson(t, "member"), h.addPerson(t, "admin")
	name := skillName("builtin")
	skill := uuid.MustParse(h.builtinSkill(t, name, 2))

	for who, person := range map[string]uuid.UUID{"a member": member, "an administrator": admin} {
		var seen int
		if err := asApp(ctx, h.super, person, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM skill_file WHERE skill_id = $1`, skill).Scan(&seen)
		}); err != nil || seen != 1 {
			t.Errorf("%s reads %d files of a built-in skill (%v), want 1", who, seen, err)
		}
		err := asApp(ctx, h.super, person, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `UPDATE skill SET archived_at = now() WHERE id = $1`, skill)
			if err == nil && tag.RowsAffected() != 0 {
				t.Errorf("%s archived a built-in skill", who)
			}
			return err
		})
		if err != nil {
			t.Errorf("%s archiving a built-in skill: %v, want no row touched", who, err)
		}
		err = asApp(ctx, h.super, person, func(tx pgx.Tx) error {
			return addSkillVersion(ctx, tx, skill, 3, name, &person)
		})
		if err == nil {
			t.Errorf("%s added a version to a built-in skill", who)
		}
		err = asApp(ctx, h.super, person, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, skill_id, user_id) VALUES ('skill_reader', $1, $2)`, skill, person)
			return err
		})
		permissionDenied(t, who+" sharing a built-in skill", err)
	}

	// The api's start adds a built-in version above the current one, never one at or below it (#225).
	err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error { return addSkillVersion(ctx, tx, skill, 2, name, nil) })
	refusedBy(t, "a built-in version that is not above the current", err, "23514", "skill_version_in_sequence")
	if err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return addSkillVersion(ctx, tx, skill, 5, name, nil)
	}); err != nil {
		t.Errorf("a built-in version above the current: %v", err)
	}
	err = h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE skill_version SET comment = 'changed' WHERE skill_id = $1`, skill)
		return err
	})
	permissionDenied(t, "rotor_admin rewriting a built-in version", err)
}
