//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// skillFiles is a skill named name whose SKILL.md body is body, with one reference file.
func skillFiles(name, body string) []map[string]any {
	return []map[string]any{
		{"path": "references/guide.md", "content": "A guide.\n"},
		{"path": "SKILL.md", "content": "---\nname: " + name + "\ndescription: A skill the suite made.\n---\n\n" + body + "\n"},
	}
}

// createSkill makes a skill as b and answers its id, removing it behind the policies' back after the test, before the
// person who made it goes.
func (h *harness) createSkill(t *testing.T, b *browser, name string) string {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/skills", map[string]any{"files": skillFiles(name, "Do it well."), "comment": "first"})
	if status != http.StatusCreated {
		t.Fatalf("creating a skill = %d %v", status, answer)
	}
	id := idOf(t, answer, "skill")
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id) })
	return id
}

// problemCodes are the codes a refusal or a check answered.
func problemCodes(answer map[string]any) []string {
	if inner, ok := answer["error"].(map[string]any); ok {
		answer = inner
	}
	return listed(answer, "problems", "code")
}

// skillName is a name the name rule allows and no other test uses.
func skillName(prefix string) string { return prefix + "-" + uuid.NewString()[:8] }

func TestASkillIsCreatedSavedAndVersioned(t *testing.T) {
	h := newHarness(t)
	owner := h.addPerson(t, "member")
	b := h.browserFor(t, owner)
	name := skillName("versions")
	id := h.createSkill(t, b, name)

	status, answer := b.api(http.MethodGet, "/skills/"+id, nil)
	skill, _ := answer["skill"].(map[string]any)
	version, _ := answer["version"].(map[string]any)
	if status != http.StatusOK || skill["name"] != name || skill["currentVersion"] != float64(1) || skill["canEdit"] != true ||
		skill["canAdminister"] != true || skill["builtin"] != false || skill["description"] != "A skill the suite made." {
		t.Fatalf("the owner reading their skill = %d %v", status, answer)
	}
	if got := listed(version, "files", "path"); !slices.Equal(got, []string{"SKILL.md", "references/guide.md"}) {
		t.Errorf("version 1 lists %v, want SKILL.md first", got)
	}
	if front, _ := version["frontMatter"].(map[string]any); front["name"] != name {
		t.Errorf("the front matter is %v", version["frontMatter"])
	}

	status, answer = b.api(http.MethodPut, "/skills/"+id, map[string]any{"baseVersion": 1, "files": skillFiles(name, "Do it better."),
		"comment": "sharper"})
	if version, _ := answer["version"].(map[string]any); status != http.StatusOK || version["number"] != float64(2) || version["comment"] != "sharper" {
		t.Fatalf("saving version 2 = %d %v", status, answer)
	}
	status, answer = b.api(http.MethodPut, "/skills/"+id, map[string]any{"baseVersion": 1, "files": skillFiles(name, "Stale.")})
	if inner, _ := answer["error"].(map[string]any); status != http.StatusConflict || inner["code"] != "skill_changed" {
		t.Errorf("saving on a stale version = %d %v, want 409 skill_changed", status, answer)
	}
	status, answer = b.api(http.MethodPut, "/skills/"+id, map[string]any{"baseVersion": 2, "files": skillFiles(skillName("renamed"), "x")})
	if status != http.StatusUnprocessableEntity || !slices.Contains(problemCodes(answer), "skill_name_changed") {
		t.Errorf("renaming the skill in a save = %d %v, want 422 skill_name_changed", status, answer)
	}
	status, answer = b.api(http.MethodPost, "/skills", map[string]any{"files": skillFiles(name, "Mine too.")})
	if status != http.StatusUnprocessableEntity || !slices.Contains(problemCodes(answer), "skill_name_taken") {
		t.Errorf("a second skill of the same name = %d %v, want 422 skill_name_taken", status, answer)
	}
	status, answer = b.api(http.MethodPost, "/skills", map[string]any{"files": []map[string]any{{"path": "../x.md", "content": "x"}}})
	if got := problemCodes(answer); status != http.StatusUnprocessableEntity || !slices.Contains(got, "skill_missing") ||
		!slices.Contains(got, "skill_path") {
		t.Errorf("a skill without SKILL.md and with a path climbing out = %d %v", status, answer)
	}

	_, answer = b.api(http.MethodGet, "/skills/"+id+"/versions", nil)
	if got := listed(answer, "versions", "comment"); !slices.Equal(got, []string{"sharper", "first"}) {
		t.Errorf("the versions are %v, want newest first", got)
	}
	status, answer = b.api(http.MethodGet, "/skills/"+id+"/versions/1/file?path=SKILL.md", nil)
	if content, _ := answer["content"].(string); status != http.StatusOK || content == "" || answer["sha256"] == "" {
		t.Errorf("reading version 1's SKILL.md = %d %v", status, answer)
	}
	if status, _ := b.api(http.MethodGet, "/skills/"+id+"/versions/1/file?path=nothing.md", nil); status != http.StatusNotFound {
		t.Errorf("reading a file the version lacks = %d, want 404", status)
	}
	_, answer = b.api(http.MethodGet, "/skills/"+id+"/diff?from=1&to=2", nil)
	if got := listed(answer, "files", "status"); !slices.Equal(got, []string{"changed"}) || !slices.Equal(listed(answer, "files", "path"), []string{"SKILL.md"}) {
		t.Errorf("the diff of 1 and 2 is %v", answer)
	}

	_, answer = b.api(http.MethodPost, "/skills/validate", map[string]any{"files": []map[string]any{{"path": "SKILL.md", "content": "no front matter"}}})
	if answer["valid"] != false || !slices.Equal(problemCodes(answer), []string{"skill_front_matter"}) {
		t.Errorf("validating a SKILL.md without front matter = %v", answer)
	}
	_, answer = b.api(http.MethodPost, "/skills/validate", map[string]any{"files": skillFiles(skillName("fine"), "ok")})
	if answer["valid"] != true {
		t.Errorf("validating a good skill = %v", answer)
	}

	var actions []string
	rows, err := h.super.Query(context.Background(), `SELECT action FROM audit_log WHERE target_type = 'skill' AND target_id = $1 ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		actions = append(actions, a)
	}
	rows.Close()
	if !slices.Equal(actions, []string{"skill.created", "skill.changed"}) {
		t.Errorf("the audit log holds %v for the skill", actions)
	}
}

func TestASkillIsReadByItsGranteesAloneAndArchivedByItsAdministrator(t *testing.T) {
	h := newHarness(t)
	owner, reader, outsider := h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	ob, rb, xb := h.browserFor(t, owner), h.browserFor(t, reader), h.browserFor(t, outsider)
	name := skillName("shared")
	id := h.createSkill(t, ob, name)
	reads := func(b *browser) bool {
		status, _ := b.api(http.MethodGet, "/skills/"+id, nil)
		return status == http.StatusOK
	}
	listedFor := func(b *browser) bool {
		_, answer := b.api(http.MethodGet, "/skills?q="+name, nil)
		return slices.Contains(listed(answer, "skills", "id"), id)
	}

	if reads(xb) || listedFor(xb) {
		t.Fatal("a skill nobody was granted is read by someone else")
	}
	if status, _ := xb.api(http.MethodGet, "/skills/"+id+"/versions", nil); status != http.StatusNotFound {
		t.Errorf("an outsider listing the versions = %d, want 404", status)
	}
	status, answer := ob.api(http.MethodPost, "/skills/"+id+"/grants", map[string]any{"role": "skill_reader", "userId": reader.String()})
	grant := idOf(t, answer, "grant")
	if made, _ := answer["grant"].(map[string]any); status != http.StatusCreated || made["skillId"] != id || made["skillName"] != name {
		t.Errorf("granting Skill reader = %d %v", status, answer)
	}
	if status, _ := ob.api(http.MethodPost, "/skills/"+id+"/grants", map[string]any{"role": "editor", "userId": reader.String()}); status != http.StatusUnprocessableEntity {
		t.Errorf("granting a workflow role on a skill = %d, want 422", status)
	}
	if !reads(rb) || !listedFor(rb) || reads(xb) {
		t.Errorf("after the grant the reader reads it %v, the outsider %v", reads(rb), reads(xb))
	}
	_, answer = rb.api(http.MethodGet, "/skills/"+id, nil)
	if skill, _ := answer["skill"].(map[string]any); skill["canEdit"] != false || skill["canAdminister"] != false {
		t.Errorf("the reader may %v", skill)
	}
	if status, _ := rb.api(http.MethodPut, "/skills/"+id, map[string]any{"baseVersion": 1, "files": skillFiles(name, "Mine now.")}); status != http.StatusForbidden {
		t.Errorf("the reader saving = %d, want 403", status)
	}
	if status, _ := rb.api(http.MethodDelete, "/skills/"+id, nil); status != http.StatusForbidden {
		t.Errorf("the reader archiving = %d, want 403", status)
	}
	if status, _ := rb.api(http.MethodPost, "/skills/"+id+"/grants", map[string]any{"role": "skill_reader", "userId": outsider.String()}); status != http.StatusForbidden {
		t.Errorf("the reader sharing = %d, want 403", status)
	}
	if _, answer := rb.api(http.MethodGet, "/skills/"+id+"/grants", nil); !slices.Contains(listed(answer, "grants", "id"), grant) {
		t.Errorf("the reader sees the grants %v", answer)
	}
	if _, answer := rb.api(http.MethodGet, "/access/me", nil); !slices.Contains(listed(answer, "grants", "skillId"), id) {
		t.Errorf("the reader's access does not name the skill: %v", answer)
	}

	if status, _ := ob.api(http.MethodDelete, "/skills/"+id, nil); status != http.StatusNoContent {
		t.Fatalf("the owner archiving = %d", status)
	}
	if listedFor(ob) {
		t.Error("an archived skill is listed")
	}
	if _, answer := ob.api(http.MethodGet, "/skills?archived=true&q="+name, nil); !slices.Contains(listed(answer, "skills", "id"), id) {
		t.Error("an archived skill is not listed among the archived")
	}
	if status, _ := ob.api(http.MethodPut, "/skills/"+id, map[string]any{"baseVersion": 1, "files": skillFiles(name, "After.")}); status != http.StatusConflict {
		t.Errorf("saving an archived skill = %d, want 409", status)
	}
	if status, _ := rb.api(http.MethodPost, "/skills/"+id+"/restore", nil); status != http.StatusForbidden {
		t.Errorf("the reader restoring = %d, want 403", status)
	}
	if status, _ := ob.api(http.MethodPost, "/skills/"+id+"/restore", nil); status != http.StatusOK || !listedFor(ob) {
		t.Errorf("the owner restoring = %d", status)
	}

	if status, _ := ob.api(http.MethodDelete, "/skills/"+id+"/grants/"+grant, nil); status != http.StatusNoContent {
		t.Fatalf("revoking = %d", status)
	}
	if reads(rb) {
		t.Error("the reader still reads a skill whose grant was taken away")
	}
}

func TestABuiltinSkillIsReadByEveryoneAndChangedByNobody(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	member, admin := h.addPerson(t, "member"), h.addPerson(t, "admin")
	name := skillName("shipped")
	id := h.builtinSkill(t, name, 3)

	for who, b := range map[string]*browser{"a member": h.browserFor(t, member), "an administrator": h.browserFor(t, admin)} {
		status, answer := b.api(http.MethodGet, "/skills/"+id, nil)
		skill, _ := answer["skill"].(map[string]any)
		if status != http.StatusOK || skill["builtin"] != true || skill["owner"] != nil || skill["canEdit"] != false ||
			skill["canAdminister"] != false || skill["currentVersion"] != float64(3) {
			t.Errorf("%s reading a built-in skill = %d %v", who, status, answer)
		}
		if _, answer := b.api(http.MethodGet, "/skills?q="+name, nil); !slices.Contains(listed(answer, "skills", "id"), id) {
			t.Errorf("%s does not find the built-in skill listed", who)
		}
		if status, _ := b.api(http.MethodPut, "/skills/"+id, map[string]any{"baseVersion": 3, "files": skillFiles(name, "Changed.")}); status != http.StatusForbidden {
			t.Errorf("%s saving a built-in skill = %d, want 403", who, status)
		}
		if status, _ := b.api(http.MethodDelete, "/skills/"+id, nil); status != http.StatusForbidden {
			t.Errorf("%s archiving a built-in skill = %d, want 403", who, status)
		}
		if status, _ := b.api(http.MethodPost, "/skills/"+id+"/grants", map[string]any{"role": "skill_reader", "userId": member.String()}); status != http.StatusForbidden {
			t.Errorf("%s sharing a built-in skill = %d, want 403", who, status)
		}
	}
	var archived bool
	if err := h.super.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM skill WHERE id = $1`, id).Scan(&archived); err != nil || archived {
		t.Errorf("the built-in skill is archived %v (%v)", archived, err)
	}
}
