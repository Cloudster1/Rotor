//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// structured is the machine-readable answer of a tool call that succeeded.
func structured(t *testing.T, called map[string]any) map[string]any {
	t.Helper()
	if called["isError"] == true {
		t.Fatalf("the tool call failed: %v", called)
	}
	out, _ := called["structuredContent"].(map[string]any)
	return out
}

// An assistant lists, reads, checks, makes and saves skills as the person it acts for (#224).
func TestAnAssistantListsReadsChecksCreatesAndSavesASkill(t *testing.T) {
	h := newHarness(t)
	keepAssistantsOn(t, h)
	owner := h.addPerson(t, "member")
	b := h.browserFor(t, owner)
	_, access := connectHand(t, h, b, map[string]any{"allow": true})
	_, _, offered := mcpAs(t, access, "tools/list", nil)
	names := toolNames(offered)
	for _, name := range []string{"list_skills", "get_skill", "get_skill_version", "get_skill_file", "validate_skill", "create_skill", "save_skill"} {
		if !names[name] {
			t.Errorf("an assistant is offered no %s", name)
		}
	}

	name := skillName("assist")
	files := skillFiles(name, "Do it well.")
	checked := structured(t, callTool(t, access, "validate_skill", map[string]any{"files": files}))
	if checked["valid"] != true {
		t.Errorf("validate_skill = %v", checked)
	}
	bad := structured(t, callTool(t, access, "validate_skill", map[string]any{"files": []map[string]any{{"path": "SKILL.md", "content": "no front matter"}}}))
	if bad["valid"] != false || len(bad["problems"].([]any)) == 0 {
		t.Errorf("validate_skill on a bad skill = %v", bad)
	}

	made := structured(t, callTool(t, access, "create_skill", map[string]any{"files": files, "comment": "from an assistant"}))
	id := idOf(t, made, "skill")
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id) })
	if made["skill"].(map[string]any)["name"] != name {
		t.Fatalf("create_skill = %v", made)
	}
	if status, _ := b.api(http.MethodGet, "/skills/"+id, nil); status != http.StatusOK {
		t.Errorf("the person does not see the skill their assistant made: %d", status)
	}

	found := structured(t, callTool(t, access, "list_skills", map[string]any{"q": name}))
	if got := listed2(found); !slices.Contains(got, name) {
		t.Errorf("list_skills lists %v", got)
	}
	got := structured(t, callTool(t, access, "get_skill", map[string]any{"skillID": id}))
	if got["version"].(map[string]any)["number"] != float64(1) {
		t.Errorf("get_skill = %v", got)
	}
	version := structured(t, callTool(t, access, "get_skill_version", map[string]any{"skillID": id, "versionNumber": "1"}))
	if paths := listed(version, "files", "path"); !slices.Equal(paths, []string{"SKILL.md", "references/guide.md"}) {
		t.Errorf("get_skill_version lists %v", paths)
	}
	file := structured(t, callTool(t, access, "get_skill_file", map[string]any{"skillID": id, "versionNumber": "1", "path": "references/guide.md"}))
	if !strings.Contains(stringify(file), "A guide.") {
		t.Errorf("get_skill_file = %v", file)
	}

	saved := structured(t, callTool(t, access, "save_skill", map[string]any{"skillID": id, "baseVersion": 1,
		"files": skillFiles(name, "Do it better."), "comment": "sharper"}))
	if saved["version"].(map[string]any)["number"] != float64(2) {
		t.Errorf("save_skill = %v", saved)
	}
	stale := callTool(t, access, "save_skill", map[string]any{"skillID": id, "baseVersion": 1, "files": skillFiles(name, "Stale.")})
	if stale["isError"] != true {
		t.Errorf("a stale save_skill = %v", stale)
	}
}

func listed2(answer map[string]any) []string { return listed(answer, "skills", "name") }

// A grant made to read is offered the reads and refused the writes, and the in-person operations are no one's tools.
func TestAnAssistantThatMayOnlyReadSkillsCannotWriteAndNoneSharesArchivesOrImports(t *testing.T) {
	h := newHarness(t)
	keepAssistantsOn(t, h)
	owner := h.addPerson(t, "member")
	b := h.browserFor(t, owner)
	id := h.createSkill(t, b, skillName("readonly"))
	_, reader := connectHand(t, h, b, map[string]any{"allow": true, "readOnly": true})
	_, writer := connectHand(t, h, b, map[string]any{"allow": true})

	_, _, offered := mcpAs(t, reader, "tools/list", nil)
	names := toolNames(offered)
	if names["create_skill"] || names["save_skill"] || !names["list_skills"] || !names["get_skill_file"] || !names["validate_skill"] {
		t.Errorf("a reading grant is offered %v", names)
	}
	files := skillFiles(skillName("refused"), "x")
	for name, args := range map[string]map[string]any{
		"create_skill": {"files": files},
		"save_skill":   {"skillID": id, "baseVersion": 1, "files": files},
	} {
		called := callTool(t, reader, name, args)
		if called["isError"] != true || !strings.Contains(stringify(called), "read only") {
			t.Errorf("a reading grant calls %s: %v", name, called)
		}
	}
	read := callTool(t, reader, "get_skill", map[string]any{"skillID": id})
	if read["isError"] == true {
		t.Errorf("a reading grant cannot read a skill: %v", read)
	}

	_, _, all := mcpAs(t, writer, "tools/list", nil)
	for name := range toolNames(all) {
		for _, never := range []string{"share", "grant", "archive", "restore", "import", "export"} {
			if strings.Contains(name, never) {
				t.Errorf("an assistant is offered %s", name)
			}
		}
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/skills/" + id + "/grants"},
		{http.MethodDelete, "/skills/" + id + "/grants/" + id},
		{http.MethodDelete, "/skills/" + id},
		{http.MethodPost, "/skills/" + id + "/restore"},
		{http.MethodPost, "/skills/import"},
	} {
		for who, token := range map[string]string{"writer": writer, "reader": reader} {
			if status := bearerAt(t, token, c.method, c.path); status != http.StatusUnauthorized {
				t.Errorf("the %s's token sent straight to %s %s answers %d, want 401", who, c.method, c.path, status)
			}
		}
	}
	if status, answer := b.api(http.MethodGet, "/skills/"+id, nil); status != http.StatusOK || answer["skill"].(map[string]any)["archived"] == true {
		t.Errorf("the skill was changed behind the person's back: %d %v", status, answer)
	}
}
