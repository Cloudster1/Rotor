package spec

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// CodeRepositoryBinding is a repository binding Rotor cannot clone: its tool offers no git, its names read
// something other than inputs, or the person does not see the repository (#137).
const CodeRepositoryBinding = "repository_binding"

// Templated says whether the repository names a project or repository by the run's inputs, known when it starts.
func (r Repository) Templated() bool {
	return strings.Contains(r.Project, "{{") || strings.Contains(r.Name, "{{")
}

// repositorySense refuses a repository binding whose names read anything but the run's inputs: what a run read
// must never choose where its code goes.
func repositorySense(s *Spec) []Problem {
	var out []Problem
	for _, name := range sortedKeys(s.Tools) {
		r := s.Tools[name].Repository
		if r == nil {
			continue
		}
		for field, value := range map[string]string{"project": r.Project, "name": r.Name} {
			if rest := templateRef.ReplaceAllString(value, ""); strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
				out = append(out, Problem{Path: fmt.Sprintf("/tools/%s/repository/%s", escape(name), field), Code: CodeRepositoryBinding,
					Message: fmt.Sprintf("A repository's %s holds a template Rotor cannot read; write it as {{ inputs.name }}.", field)})
			}
			for _, m := range templateRef.FindAllStringSubmatch(value, -1) {
				if !strings.HasPrefix(m[1], "inputs.") {
					out = append(out, Problem{Path: fmt.Sprintf("/tools/%s/repository/%s", escape(name), field), Code: CodeRepositoryBinding,
						Message: fmt.Sprintf("A repository's %s may read the run's inputs alone, not %s; name it, or take it from an input.", field, m[1])})
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b Problem) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// checkRepository finds a repository binding its tool cannot serve: no git there, or, for fixed names, a
// repository the person does not see in the project. Templates are checked once a run knows its inputs.
func checkRepository(b Binding, base string, info ToolInfo) []Problem {
	if b.Repository == nil {
		return nil
	}
	if !info.Git {
		return []Problem{{Path: base + "/repository", Code: CodeRepositoryBinding,
			Message: fmt.Sprintf("%s offers no git, so a repository binding cannot clone through it; bind a tool that does, such as Armature with its git proxy.", b.Tool)}}
	}
	r := b.Repository
	if r.Templated() || info.Repositories == nil {
		return nil
	}
	if seen, known := info.Repositories[r.Project]; known && !slices.Contains(seen, r.Name) {
		return []Problem{{Path: base + "/repository", Code: CodeRepositoryBinding,
			Message: fmt.Sprintf("%s shows you no repository %s in the project %s; name one its project connects, as list_repositories gives it.", b.Tool, r.Name, r.Project)}}
	}
	return nil
}

// WithInputs is the spec with each repository binding's templates filled from the run's inputs, so the
// repositories it names can be checked and cloned; an input that is missing or not text is a problem.
func (s *Spec) WithInputs(inputs []byte) (*Spec, []Problem) {
	var values map[string]any
	if len(inputs) > 0 && json.Unmarshal(inputs, &values) != nil {
		values = nil
	}
	out := *s
	out.Tools = maps.Clone(s.Tools)
	var problems []Problem
	for _, name := range sortedKeys(s.Tools) {
		b := s.Tools[name]
		if b.Repository == nil || !b.Repository.Templated() {
			continue
		}
		filled := *b.Repository
		for _, field := range []struct {
			name  string
			value *string
		}{{"project", &filled.Project}, {"name", &filled.Name}} {
			text, missing := fillInputs(*field.value, values)
			if missing != "" {
				problems = append(problems, Problem{Path: inputPointer(missing), Code: CodeRepositoryBinding,
					Message: fmt.Sprintf("The repository's %s reads %s, which the run's inputs do not give as text; give it when starting the run.", field.name, missing)})
			}
			*field.value = text
		}
		b.Repository = &filled
		out.Tools[name] = b
	}
	return &out, problems
}

// fillInputs replaces each {{ inputs.x }} by its text, answering the first path that has none.
func fillInputs(value string, inputs map[string]any) (string, string) {
	missing := ""
	filled := templateRef.ReplaceAllStringFunc(value, func(match string) string {
		path := templateRef.FindStringSubmatch(match)[1]
		var found any = map[string]any{"inputs": map[string]any(inputs)}
		for _, part := range strings.Split(path, ".") {
			m, ok := found.(map[string]any)
			if !ok {
				found = nil
				break
			}
			found = m[part]
		}
		text, ok := found.(string)
		if !ok || text == "" {
			if missing == "" {
				missing = path
			}
			return ""
		}
		return text
	})
	return filled, missing
}

// inputPointer is where a state path such as inputs.repository is among the run's inputs: /repository.
func inputPointer(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "inputs."), ".")
	for i, p := range parts {
		parts[i] = escape(p)
	}
	return "/" + strings.Join(parts, "/")
}

// ByInputs splits the problems of a spec WithInputs filled: a repository its inputs named that the tool refused is
// the inputs' problem, placed at the input that named it; the rest are the workflow's.
func (s *Spec) ByInputs(problems []Problem) (inputs, rest []Problem) {
	for _, p := range problems {
		name, isRepository := strings.CutSuffix(strings.TrimPrefix(p.Path, "/tools/"), "/repository")
		b, ok := s.Tools[strings.NewReplacer("~1", "/", "~0", "~").Replace(name)]
		if p.Code != CodeRepositoryBinding || !isRepository || !ok || b.Repository == nil || !b.Repository.Templated() {
			rest = append(rest, p)
			continue
		}
		for _, value := range []string{b.Repository.Name, b.Repository.Project} {
			if m := templateRef.FindStringSubmatch(value); m != nil {
				p.Path, p.Line, p.Column = inputPointer(m[1]), 0, 0
				break
			}
		}
		inputs = append(inputs, p)
	}
	return inputs, rest
}
