// Command suite-seed sets the suite's Armature up for Rotor's tests and connects the suite's Exciter to it (#130).
// make suite-up runs it on the suite's network every time; a second run leaves what is right alone.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
)

// Where each product answers inside the suite's network, and the accounts the suite signs in with.
const (
	armatureAPI = "http://suite-armature-api:8080/api/v1"
	exciterAPI  = "http://suite-exciter-api:8080/api/v1"

	// The owner sets the installation up and is its administrator; nobody signs in as them through Keycloak.
	ownerEmail, ownerName, ownerPassword = "admin@armature.test", "Armature Admin", "armature admin password"
	// exciterAdmin is the administrator deploy/suite/exciter.yml leaves Exciter's own seed to make.
	exciterAdmin, exciterPassword = "admin@exciter.test", "exciter admin password"
	// armatureClient and its secret are the realm's, deploy/keycloak/realm.json.
	armatureClient, armatureSecret = "armature", "armature-dev-secret"

	// The project the tests file stories and bugs in.
	projectKey, projectName = "CP", "Customer portal"
	// The built-in roles a member joins with, and the one that sees a project and changes nothing.
	userRole, readerRole = "user", "reader"
	// listLimit is the most Armature lists at once, so one page holds everybody a name matches.
	listLimit = "200"
)

// person is somebody of the realm, let into Armature with the realm's password; a reader may change nothing.
type person struct {
	email, name, role, password string
	reader                      bool
}

var people = []person{
	{"alice@suite.test", "Alice", "admin", "alice password", false},
	{"bob@suite.test", "Bob", "member", "bob password", false},
	{"dave@suite.test", "Dave", "member", "dave password", true},
}

// The issue types the story workflow tells apart; Armature makes both for every installation.
var wantTypes = []string{"Story", "Bug"}

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("the suite's Armature could not be set up", "error", err)
		os.Exit(1)
	}
}

func env(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("a setting is missing; run this through make suite-up", "setting", key)
		os.Exit(2)
	}
	return v
}

func run(ctx context.Context) error {
	owner, err := ownerSession(ctx)
	if err != nil {
		return err
	}
	if err := owner.send(ctx, http.MethodPut, "/oidc-provider", map[string]any{
		"issuer": env("SUITE_KEYCLOAK_ISSUER"), "clientId": armatureClient, "clientSecret": armatureSecret,
		"groupsClaim": "groups", "scopes": "openid email profile", "createGroups": false, "admitByGroup": false, "enabled": true,
	}, nil); err != nil {
		return fmt.Errorf("point Armature at Rotor's Keycloak: %w", err)
	}
	for _, p := range people {
		if err := letIn(ctx, owner, p); err != nil {
			return err
		}
	}
	if err := ensureProject(ctx, owner); err != nil {
		return err
	}
	slog.Info("Armature is set up", "project", projectKey)

	exciter, err := signIn(ctx, exciterAPI, exciterAdmin, exciterPassword)
	if err != nil {
		return fmt.Errorf("sign in to Exciter as its administrator: %w", err)
	}
	// The same address keeps the tokens people stored; Exciter checks that Armature answers before it saves.
	if err := exciter.send(ctx, http.MethodPut, "/armature/connection", map[string]any{"baseUrl": env("SUITE_ARMATURE_URL")}, nil); err != nil {
		return fmt.Errorf("connect Exciter to Armature: %w", err)
	}
	slog.Info("Exciter is connected to Armature")
	return nil
}

// ownerSession sets the installation up as its owner when nobody has, and signs the owner in otherwise.
func ownerSession(ctx context.Context) (*session, error) {
	s, err := newSession(armatureAPI)
	if err != nil {
		return nil, err
	}
	var state struct {
		First bool `json:"first"`
	}
	if err := s.send(ctx, http.MethodGet, "/auth/signup", nil, &state); err != nil {
		return nil, fmt.Errorf("ask Armature whether it is set up: %w", err)
	}
	if state.First {
		err := s.send(ctx, http.MethodPost, "/auth/signup", map[string]any{"email": ownerEmail, "name": ownerName, "password": ownerPassword}, nil)
		if err != nil {
			return nil, fmt.Errorf("set Armature up as %s: %w", ownerEmail, err)
		}
		slog.Info("set Armature up", "owner", ownerEmail)
		return s, nil
	}
	if err := s.send(ctx, http.MethodPost, "/auth/login", map[string]any{"email": ownerEmail, "password": ownerPassword}, nil); err != nil {
		return nil, fmt.Errorf("sign in to Armature as %s; if somebody else set it up, run make suite-clean first: %w", ownerEmail, err)
	}
	return s, nil
}

// letIn makes the person a member with the role they need, and a reader everywhere when they may change nothing.
func letIn(ctx context.Context, owner *session, p person) error {
	err := owner.send(ctx, http.MethodPost, "/users", map[string]any{"email": p.email, "name": p.name, "role": p.role, "password": p.password}, nil)
	if err != nil && !isConflict(err) {
		return fmt.Errorf("let %s into Armature: %w", p.email, err)
	}
	if !p.reader {
		return nil
	}
	var listed struct {
		Users []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"users"`
	}
	if err := owner.send(ctx, http.MethodGet, "/users?"+url.Values{"q": {p.name}, "limit": {listLimit}}.Encode(), nil, &listed); err != nil {
		return fmt.Errorf("find %s in Armature: %w", p.email, err)
	}
	at := slices.IndexFunc(listed.Users, func(u struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}) bool {
		return u.Email == p.email
	})
	if at < 0 {
		return fmt.Errorf("armature lists no %s after letting them in", p.email)
	}
	return makeReader(ctx, owner, listed.Users[at].ID, p)
}

// assignment is one role a person holds; no project key means everywhere.
type assignment struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	ProjectKey string `json:"projectKey"`
	UserID     string `json:"userId"`
}

// makeReader takes the role a member joins with away from the person and gives them the reader's, everywhere.
func makeReader(ctx context.Context, owner *session, userID string, p person) error {
	var listed struct {
		Assignments []assignment `json:"assignments"`
	}
	if err := owner.send(ctx, http.MethodGet, "/role-assignments?"+url.Values{"q": {p.name}, "limit": {listLimit}}.Encode(), nil, &listed); err != nil {
		return fmt.Errorf("read %s's roles: %w", p.email, err)
	}
	reader := false
	for _, a := range listed.Assignments {
		if a.UserID != userID || a.ProjectKey != "" {
			continue
		}
		switch a.Role {
		case readerRole:
			reader = true
		case userRole:
			if err := owner.send(ctx, http.MethodDelete, "/role-assignments/"+a.ID, nil, nil); err != nil {
				return fmt.Errorf("take the %s role from %s: %w", userRole, p.email, err)
			}
		}
	}
	if reader {
		return nil
	}
	err := owner.send(ctx, http.MethodPost, "/role-assignments", map[string]any{"role": readerRole, "projectKey": "", "userId": userID, "groupId": nil}, nil)
	if err != nil {
		return fmt.Errorf("make %s a reader: %w", p.email, err)
	}
	return nil
}

// ensureProject makes the project when it is missing, and checks the issue types the tests file are there.
func ensureProject(ctx context.Context, owner *session) error {
	err := owner.send(ctx, http.MethodGet, "/projects/"+projectKey, nil, nil)
	var r *refusal
	switch {
	case errors.As(err, &r) && r.status == http.StatusNotFound:
		if err := owner.send(ctx, http.MethodPost, "/projects", map[string]any{"key": projectKey, "name": projectName}, nil); err != nil {
			return fmt.Errorf("make the project %s: %w", projectKey, err)
		}
	case err != nil:
		return fmt.Errorf("look for the project %s: %w", projectKey, err)
	}
	var types struct {
		IssueTypes []struct {
			Name string `json:"name"`
		} `json:"issueTypes"`
	}
	if err := owner.send(ctx, http.MethodGet, "/issue-types", nil, &types); err != nil {
		return fmt.Errorf("read Armature's issue types: %w", err)
	}
	for _, want := range wantTypes {
		if !slices.ContainsFunc(types.IssueTypes, func(t struct {
			Name string `json:"name"`
		}) bool {
			return t.Name == want
		}) {
			return fmt.Errorf("armature has no issue type %s, which the story workflow's tests file", want)
		}
	}
	return nil
}
