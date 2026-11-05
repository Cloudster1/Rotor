//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// zipOf packs entries (name to text) into a zip.
func zipOf(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, e := range entries {
		f, err := w.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(e[1]))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// skillZip is a skill folder named name as a zip, its SKILL.md saying body.
func skillZip(t *testing.T, name, body string) []byte {
	return zipOf(t, [][2]string{
		{name + "/SKILL.md", "---\nname: " + name + "\ndescription: A skill the suite zipped.\n---\n\n" + body + "\n"},
		{name + "/references/guide.md", "A guide.\n"},
	})
}

// raw calls the api as the browser with a body that is not JSON, and answers the status, headers and bytes.
func (b *browser) raw(method, path, contentType string, body []byte) (int, http.Header, []byte) {
	b.t.Helper()
	req, err := http.NewRequest(method, b.app+"/api/v1"+path, bytes.NewReader(body))
	if err != nil {
		b.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, got
}

func (b *browser) importZip(path string, data []byte) (int, map[string]any) {
	b.t.Helper()
	status, _, body := b.raw(http.MethodPost, path, "application/zip", data)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return status, out
}

func TestImportingAZipMakesASkillThenSavesItsNextVersionAndExportsUnchanged(t *testing.T) {
	h := newHarness(t)
	b := h.browserFor(t, h.addPerson(t, "member"))
	name := skillName("zipped")

	status, answer := b.importZip("/skills/import?comment=from+a+zip", skillZip(t, name, "Version one."))
	skill, _ := answer["skill"].(map[string]any)
	version, _ := answer["version"].(map[string]any)
	if status != http.StatusCreated || answer["created"] != true || skill["name"] != name || version["number"] != float64(1) ||
		version["comment"] != "from a zip" {
		t.Fatalf("importing a new skill = %d %v", status, answer)
	}
	id, _ := skill["id"].(string)
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id) })
	var data string
	if err := h.super.QueryRow(context.Background(), `SELECT data::text FROM audit_log WHERE action = 'skill.imported' AND target_id = $1`,
		id).Scan(&data); err != nil || !strings.Contains(data, `"created": true`) || !strings.Contains(data, `"version": 1`) {
		t.Errorf("the import was audited as %q (%v)", data, err)
	}

	// The same files again, with SKILL.md at the root this time and a change.
	changed := zipOf(t, [][2]string{
		{"SKILL.md", "---\nname: " + name + "\ndescription: A skill the suite zipped.\n---\n\nVersion two.\n"},
		{"references/guide.md", "A guide.\n"}})
	status, answer = b.importZip("/skills/import", changed)
	version, _ = answer["version"].(map[string]any)
	if status != http.StatusOK || answer["created"] != false || version["number"] != float64(2) {
		t.Fatalf("importing over the skill = %d %v", status, answer)
	}
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE data->>'created' = 'false')::text
		FROM audit_log WHERE action = 'skill.imported' AND target_id = $1`, id).Scan(&data); err != nil || data != "1" {
		t.Errorf("the second import was audited %q times as a new version (%v)", data, err)
	}

	status, header, exported := b.raw(http.MethodGet, "/skills/"+id+"/versions/1/export", "", nil)
	if status != http.StatusOK || header.Get("Content-Type") != "application/zip" ||
		header.Get("Content-Disposition") != `attachment; filename="`+name+`-v1.zip"` {
		t.Fatalf("exporting version 1 = %d %v", status, header)
	}
	_, again, second := b.raw(http.MethodGet, "/skills/"+id+"/versions/1/export", "", nil)
	if !bytes.Equal(exported, second) || again.Get("Content-Length") == "" {
		t.Error("the same version exported to different bytes")
	}
	_, answer = b.api(http.MethodGet, "/skills/"+id+"/versions/1", nil)
	hash, _ := answer["hash"].(string)
	// Version 1 imported again becomes version 3, carrying version 1's hash.
	status, answer = b.importZip("/skills/import", exported)
	version, _ = answer["version"].(map[string]any)
	if status != http.StatusOK || version["number"] != float64(3) || version["hash"] != hash {
		t.Errorf("importing the export = %d %v, want version 3 with hash %s", status, answer, hash)
	}
	if status, _, _ := b.raw(http.MethodGet, "/skills/"+id+"/versions/9/export", "", nil); status != http.StatusNotFound {
		t.Errorf("exporting a version that does not exist = %d, want 404", status)
	}
}

func TestImportingOverSomeoneElsesSkillIsRefusedAsASaveWouldBe(t *testing.T) {
	h := newHarness(t)
	owner, reader, outsider := h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	ob, rb, xb := h.browserFor(t, owner), h.browserFor(t, reader), h.browserFor(t, outsider)
	name := skillName("held")
	id := h.createSkill(t, ob, name)
	if status, answer := ob.api(http.MethodPost, "/skills/"+id+"/grants", map[string]any{"role": "skill_reader", "userId": reader.String()}); status != http.StatusCreated {
		t.Fatalf("granting Skill reader = %d %v", status, answer)
	}
	data := skillZip(t, name, "Not yours.")
	if status, answer := xb.importZip("/skills/import", data); status != http.StatusNotFound {
		t.Errorf("importing over a skill the caller cannot read = %d %v, want 404", status, answer)
	}
	if status, answer := rb.importZip("/skills/import", data); status != http.StatusForbidden {
		t.Errorf("importing over a skill the caller only reads = %d %v, want 403", status, answer)
	}
	if _, answer := ob.api(http.MethodGet, "/skills/"+id, nil); answer["skill"].(map[string]any)["currentVersion"] != float64(1) {
		t.Error("a refused import still saved a version")
	}
}

func TestAnImportIsRefusedWithTheProblemsOfTheFilesAndWhenTooLarge(t *testing.T) {
	h := newHarness(t)
	b := h.browserFor(t, h.addPerson(t, "member"))

	status, answer := b.importZip("/skills/import", []byte("not a zip"))
	if got := problemCodes(answer); status != http.StatusUnprocessableEntity || len(got) != 1 || got[0] != "skill_not_zip" {
		t.Errorf("importing text = %d %v", status, answer)
	}
	escape := zipOf(t, [][2]string{{"SKILL.md", "x"}, {"../evil.md", "x"}})
	status, answer = b.importZip("/skills/import", escape)
	if got := problemCodes(answer); status != http.StatusUnprocessableEntity || len(got) != 1 || got[0] != "skill_path" {
		t.Errorf("importing a path out of the folder = %d %v", status, answer)
	}
	status, _, body := b.raw(http.MethodPost, "/skills/import", "application/zip", bytes.Repeat([]byte{0}, 2<<20+1))
	if status != http.StatusRequestEntityTooLarge || !strings.Contains(string(body), "too_large") {
		t.Errorf("importing over 2 MiB = %d %s, want 413", status, body)
	}
}
