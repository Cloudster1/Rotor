//go:build integration

package test

import (
	"net/http"
	"slices"
	"testing"
)

// grantsOf is the roles /access/me lists, as "role" or "role on workflow".
func grantsOf(answer map[string]any) []string {
	out := []string{}
	grants, _ := answer["grants"].([]any)
	for _, g := range grants {
		grant, _ := g.(map[string]any)
		role, _ := grant["role"].(string)
		if workflow, ok := grant["workflowId"].(string); ok {
			role += " on " + workflow
		}
		out = append(out, role)
	}
	return out
}

func permissionsOf(answer map[string]any) (installation []string, workflows map[string][]string) {
	perms, _ := answer["permissions"].(map[string]any)
	for _, p := range perms["installation"].([]any) {
		installation = append(installation, p.(string))
	}
	workflows = map[string][]string{}
	for id, list := range perms["workflows"].(map[string]any) {
		for _, p := range list.([]any) {
			workflows[id] = append(workflows[id], p.(string))
		}
	}
	return installation, workflows
}

func flags(t *testing.T, b *browser, id string) (canRun, canEdit, canAdminister bool) {
	t.Helper()
	status, answer := b.api(http.MethodGet, "/workflows/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("reading the workflow = %d %v", status, answer)
	}
	wf, _ := answer["workflow"].(map[string]any)
	return wf["canRun"] == true, wf["canEdit"] == true, wf["canAdminister"] == true
}

// What a person may do is resolved from their grants on each request, and the workflow responses say it.
func TestAccessIsResolvedFromTheGrantsAndShownOnWorkflows(t *testing.T) {
	alice, bob, carol := newBrowser(t), newBrowser(t), newBrowser(t)
	alice.signIn("alice")
	bob.signIn("bob")
	carol.signIn("carol")

	_, mine := alice.api(http.MethodGet, "/access/me", nil)
	if installation, _ := permissionsOf(mine); !slices.Contains(installation, "rotor.administer") || !slices.Contains(grantsOf(mine), "administrator") {
		t.Errorf("alice, in rotor-administrators, holds %v: %v", grantsOf(mine), installation)
	}
	_, theirs := bob.api(http.MethodGet, "/access/me", nil)
	if installation, _ := permissionsOf(theirs); slices.Contains(installation, "rotor.administer") || !slices.Contains(installation, "workflow.create") {
		t.Errorf("bob holds %v over the installation, want workflow.create through Everyone and no administration", installation)
	}

	id := createWorkflow(t, bob, approvalOnly(uniqueName("granted"), "Go on?"))
	if run, edit, administer := flags(t, bob, id); !run || !edit || !administer {
		t.Errorf("the creator may run %v, edit %v, administer %v; want all", run, edit, administer)
	}
	_, theirs = bob.api(http.MethodGet, "/access/me", nil)
	if _, workflows := permissionsOf(theirs); !slices.Contains(grantsOf(theirs), "workflow_administrator on "+id) ||
		!slices.Equal(workflows[id], []string{"workflow.read", "workflow.run", "workflow.edit", "workflow.administer"}) {
		t.Errorf("the creator's grants are %v, on the workflow %v", grantsOf(theirs), workflows[id])
	}

	// A runner grant to a realm group of carol's makes her its runner: she runs it, and changes nothing.
	grantOn(t, bob, id, "runner", groupID(t, bob, "quality"))
	if run, edit, administer := flags(t, carol, id); !run || edit || administer {
		t.Errorf("a runner may run %v, edit %v, administer %v; want only run", run, edit, administer)
	}
	if status, _ := carol.api(http.MethodPost, "/workflows/"+id+"/grants", map[string]any{"role": "runner", "groupId": groupID(t, bob, "")}); status != http.StatusForbidden {
		t.Errorf("a runner granting = %d, want 403", status)
	}
	if status, _ := carol.api(http.MethodDelete, "/workflows/"+id, nil); status != http.StatusForbidden {
		t.Errorf("a runner archiving = %d, want 403", status)
	}
	// Administrator holds every permission, so an administrator of Rotor now edits and shares everyone's workflows.
	if run, edit, administer := flags(t, alice, id); !run || !edit || !administer {
		t.Errorf("an administrator may run %v, edit %v, administer %v; want all", run, edit, administer)
	}
	if status, _ := alice.api(http.MethodDelete, "/workflows/"+id, nil); status != http.StatusNoContent {
		t.Errorf("an administrator archiving another's workflow = %d, want 204", status)
	}
}
