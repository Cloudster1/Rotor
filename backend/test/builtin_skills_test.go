//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/skills"
)

// offeredShippedSkill fails unless the stub model's i-th request listed the shipped skill at version 1 and offered
// read_skill, as an agent the default workflows pin it on sees it.
func offeredShippedSkill(t *testing.T, stub *toolStub, i int, name string) {
	t.Helper()
	system, tools, _ := chatRequest(t, stub, i)
	if !strings.Contains(system, "- "+name+" (1): ") || !slices.Contains(tools, "read_skill") {
		t.Errorf("the agent was not given %s: its instructions are %q and its tools %v", name, system, tools)
	}
}

// installedState is how many versions the shipped skills have and how many times installing them was audited.
func (h *harness) installedState(t *testing.T) (versions, audited int) {
	t.Helper()
	ctx := context.Background()
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM skill_version v JOIN skill s ON s.id = v.skill_id
		WHERE s.builtin AND s.name IN ('requirements-engineering', 'test-engineering')`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'skill.installed'`).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	return versions, audited
}

func TestTheShippedSkillsAreInstalledAtStartReadByEveryoneAndChangedByNobody(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	member := h.browserFor(t, h.addPerson(t, "member"))
	shipped, err := skills.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	for _, one := range shipped {
		name := one.Draft.Name
		_, answer := member.api(http.MethodGet, "/skills?q="+name, nil)
		var id string
		for _, s := range answer["skills"].([]any) {
			if s.(map[string]any)["name"] == name {
				id, _ = s.(map[string]any)["id"].(string)
			}
		}
		if id == "" {
			t.Fatalf("a member does not find %s listed: %v", name, answer)
		}
		status, answer := member.api(http.MethodGet, "/skills/"+id, nil)
		skill, _ := answer["skill"].(map[string]any)
		if status != http.StatusOK || skill["builtin"] != true || skill["owner"] != nil || skill["canEdit"] != false ||
			skill["currentVersion"] != float64(one.Version) {
			t.Errorf("a member reading %s = %d %v", name, status, answer)
		}
		var hash, comment string
		var author *string
		if err := h.super.QueryRow(ctx, `SELECT hash, comment, created_by::text FROM skill_version WHERE skill_id = $1 AND number = $2`,
			id, one.Version).Scan(&hash, &comment, &author); err != nil || hash != one.Draft.Hash || comment != skills.InstalledComment || author != nil {
			t.Errorf("%s version %d holds the hash %s, comment %q and author %v (%v), want what ships", name, one.Version, hash, comment, author, err)
		}
		if status, _ := member.api(http.MethodGet, "/skills/"+id+"/versions/1/file?path=references/templates.md", nil); status != http.StatusOK {
			t.Errorf("a member reading %s's templates = %d", name, status)
		}
		files := []map[string]any{}
		for _, f := range one.Draft.Files {
			files = append(files, map[string]any{"path": f.Path, "content": f.Content + "\nChanged.\n"})
		}
		if status, _ := member.api(http.MethodPut, "/skills/"+id, map[string]any{"baseVersion": one.Version, "files": files}); status != http.StatusForbidden {
			t.Errorf("a member saving %s = %d, want 403", name, status)
		}
	}

	// Installing again, as every start does and as several replicas may at once, adds and audits nothing.
	versions, audited := h.installedState(t)
	if versions < len(shipped) {
		t.Fatalf("the shipped skills have %d versions, want at least %d", versions, len(shipped))
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { errs[i] = skills.NewService(h.cluster).InstallBuiltins(ctx, slog.New(slog.DiscardHandler)) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Errorf("installing again: %v", err)
		}
	}
	if v, a := h.installedState(t); v != versions || a != audited {
		t.Errorf("installing again left %d versions and %d audit entries, want %d and %d", v, a, versions, audited)
	}
}
