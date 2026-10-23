package spec

import "testing"

// armature is Armature as the person sees it: its calls, its git proxy, and the repositories of the project CP.
var armature = catalog{"armature": {Scopes: []string{"read", "issue.write", "git.write"}, Git: true,
	Calls:        map[string]CallInfo{"list_repositories": {ReadOnly: true}, "create_issue_branch": {}, "open_pull_request": {}},
	Repositories: map[string][]string{"CP": {"acme/portal"}, "XX": {}},
}}

// A repository binding clones through a tool with git, and a fixed repository must be one the person sees there;
// one its inputs name waits for the run's start (#137).
func TestARepositoryBindingNeedsGitAndARepositoryThePersonSees(t *testing.T) {
	s := parseFixture(t, "branch-for-an-issue.yaml")
	if problems := s.CheckAgainst(armature); len(problems) > 0 {
		t.Errorf("a templated repository is checked on save: %+v", problems)
	}
	repo := s.Tools["repo"]
	for project, want := range map[string]string{"CP": "", "XX": CodeRepositoryBinding, "unknown": ""} {
		repo.Repository = &Repository{Project: project, Name: "acme/portal"}
		s.Tools["repo"] = repo
		if got := codes(s.CheckAgainst(armature))["/tools/repo/repository"]; got != want {
			t.Errorf("acme/portal in %s = %q, want %q", project, got, want)
		}
	}
	noGit := catalog{"armature": {Scopes: []string{"read", "issue.write"}, Calls: armature["armature"].Calls}}
	if got := codes(s.CheckAgainst(noGit))["/tools/repo/repository"]; got != CodeRepositoryBinding {
		t.Errorf("a repository at a tool without git = %q", got)
	}
}

// The run's inputs fill a repository's names, and an input that is missing or not text is a problem of the run.
func TestTheInputsNameTheRepository(t *testing.T) {
	s := parseFixture(t, "branch-for-an-issue.yaml")
	filled, problems := s.WithInputs([]byte(`{"project": "CP", "repository": "acme/portal", "issue": "CP-12"}`))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if r := filled.Tools["repo"].Repository; r.Project != "CP" || r.Name != "acme/portal" || r.Templated() {
		t.Errorf("filled = %+v", r)
	}
	if !s.Tools["repo"].Repository.Templated() {
		t.Error("filling changed the spec it was filled from")
	}
	if problems := filled.CheckAgainst(armature); len(problems) > 0 {
		t.Errorf("a filled repository the person sees: %+v", problems)
	}
	_, problems = s.WithInputs([]byte(`{"project": "CP", "repository": 7}`))
	if got := codes(problems); len(problems) != 1 || got["/repository"] != CodeRepositoryBinding {
		t.Errorf("a repository named by a number = %+v", problems)
	}
	hidden, _ := s.WithInputs([]byte(`{"project": "XX", "repository": "acme/portal"}`))
	inputs, rest := s.ByInputs(hidden.CheckAgainst(armature))
	if len(rest) > 0 || len(inputs) != 1 || inputs[0].Path != "/repository" || inputs[0].Code != CodeRepositoryBinding {
		t.Errorf("a repository the inputs name and the person does not see = %+v, then %+v", inputs, rest)
	}
}
