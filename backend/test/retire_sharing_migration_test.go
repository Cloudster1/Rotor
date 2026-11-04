//go:build integration

package test

import (
	"context"
	"slices"
	"sort"
	"testing"
)

// sharingRetired is the version once 01340_retire_sharing.sql has run; lastWithSharing is the one before it.
const (
	sharingRetired  = 1340
	lastWithSharing = 1330
)

// Dropping the sharing columns and the role word changes nobody's access, straight through SQL as rotor_app: a
// workflow shared with a group stays the group's, and going back down rebuilds both from the grants.
func TestRetiringSharingKeepsEveryonesAccess(t *testing.T) {
	h := newHarness(t)
	s := h.newScratch(t, lastBeforeAccess)
	ctx := context.Background()
	f := seedAccessFixture(ctx, t, s.super)
	s.up(lastWithSharing)
	// Sharing is asked of the columns, which go, so the comparison leaves it out; the grants it made are kept.
	checks := slices.DeleteFunc(slices.Clone(workflowChecks), func(c accessCheck) bool { return c.name == "shares" })
	before := f.accessBy(t, ctx, s.super, checks)

	s.up(sharingRetired)
	after := f.accessBy(t, ctx, s.super, checks)
	compareAccess(t, "after the columns go", before, after)
	for key, want := range map[string]bool{
		"developer: sees on developers": true, "developer: starts a run on developers": true,
		"stranger: sees on developers": false, "stranger: starts a run on everyone": true, "admin: administers Rotor": true,
		"admin: reads the audit log": true, "owner: administers Rotor": false,
	} {
		if after[key] != want {
			t.Errorf("after the columns go %s is %v, want %v", key, after[key], want)
		}
	}

	s.down(lastWithSharing)
	compareAccess(t, "back down", before, f.accessBy(t, ctx, s.super, checks))
	for name, want := range map[string]struct {
		sharing string
		groups  []string
	}{
		"private": {"private", []string{}}, "developers": {"groups", []string{"developers"}},
		"nobody's group": {"groups", []string{"nobody-here"}}, "everyone": {"everyone", []string{}},
		"the admin's": {"private", []string{}},
	} {
		var sharing string
		var groups []string
		if err := s.super.QueryRow(ctx, `SELECT sharing, shared_groups FROM workflow WHERE id = $1`, f.workflows[name]).Scan(&sharing, &groups); err != nil {
			t.Fatal(err)
		}
		if sharing != want.sharing || !slices.Equal(groups, want.groups) {
			t.Errorf("back down %s is shared %s with %v, want %s with %v", name, sharing, groups, want.sharing, want.groups)
		}
	}
	for name, want := range map[string]string{"admin": "admin", "owner": "member", "developer": "member", "stranger": "member"} {
		var role string
		if err := s.super.QueryRow(ctx, `SELECT role FROM app_user WHERE id = $1`, f.people[name]).Scan(&role); err != nil {
			t.Fatal(err)
		}
		if role != want {
			t.Errorf("back down %s's role is %s, want %s", name, role, want)
		}
	}
}

// compareAccess fails for every answer that differs between two runs of the checks.
func compareAccess(t *testing.T, when string, before, after map[string]bool) {
	t.Helper()
	var changed []string
	for key, was := range before {
		if after[key] != was {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	for _, key := range changed {
		t.Errorf("%s %s is %v, was %v", when, key, after[key], before[key])
	}
	if len(before) != len(after) || len(before) == 0 {
		t.Errorf("%s: %d answers before, %d after", when, len(before), len(after))
	}
}
