package skills

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// skillMD is a SKILL.md with the front matter given and a short body.
func skillMD(frontMatter string) File {
	return File{Path: SkillFile, Content: "---\n" + frontMatter + "\n---\n\n# Body\n\nDo the thing.\n"}
}

func valid() File {
	return skillMD("name: requirements-engineering\ndescription: Write requirements. Use when asked to.")
}

// codes lists each problem as code@path, in the order Check found them.
func codes(problems []Problem) []string {
	out := []string{}
	for _, p := range problems {
		out = append(out, p.Code+"@"+p.Path)
	}
	return out
}

func refusedWith(t *testing.T, files []File, want string) {
	t.Helper()
	draft, problems := Check(files)
	if draft != nil || !slices.Contains(codes(problems), want) {
		t.Errorf("Check = %v, want it refused with %s", codes(problems), want)
	}
	for _, p := range problems {
		if !strings.HasSuffix(p.Message, ".") || p.Message[0] < 'A' || p.Message[0] > 'Z' {
			t.Errorf("%s says %q, which is not a sentence", p.Code, p.Message)
		}
	}
}

func TestAWellFormedSkillPassesAndKeepsItsFrontMatter(t *testing.T) {
	files := []File{
		{Path: "references/guide.md", Content: "Guide\n"},
		skillMD("name: requirements-engineering\ndescription: Write requirements.\nlicense: Apache-2.0\n" +
			"compatibility: Any agent\nallowed-tools: Read Grep\nmetadata:\n  version: \"2\""),
	}
	draft, problems := Check(files)
	if len(problems) > 0 {
		t.Fatalf("a good skill is refused: %v", codes(problems))
	}
	if draft.Name != "requirements-engineering" || draft.Description != "Write requirements." {
		t.Errorf("the draft is %q, %q", draft.Name, draft.Description)
	}
	for key, want := range map[string]any{"license": "Apache-2.0", "compatibility": "Any agent", "allowed-tools": "Read Grep"} {
		if draft.FrontMatter[key] != want {
			t.Errorf("%s is %v, want it kept as %v", key, draft.FrontMatter[key], want)
		}
	}
	if meta, _ := draft.FrontMatter["metadata"].(map[string]any); meta["version"] != "2" {
		t.Errorf("metadata is %v, want version 2 kept", draft.FrontMatter["metadata"])
	}
	if draft.Files[0].Path != SkillFile || draft.Bytes != len(files[0].Content)+len(files[1].Content) || len(draft.Hash) != 64 {
		t.Errorf("the draft's files are %v, %d bytes, hash %q", draft.Files, draft.Bytes, draft.Hash)
	}
}

func TestFrontMatterWrittenWithWindowsLineEndingsIsRead(t *testing.T) {
	f := File{Path: SkillFile, Content: "---\r\nname: crlf\r\ndescription: Read on Windows.\r\n---\r\nBody\r\n"}
	if draft, problems := Check([]File{f}); draft == nil || draft.Name != "crlf" {
		t.Errorf("Check = %v", codes(problems))
	}
}

func TestEveryProblemTheFilesAloneTellIsNamedWhereItIs(t *testing.T) {
	deep := strings.Repeat("a/", config.MaxSkillPathDepth+1) + "x.md"
	cases := map[string]struct {
		files []File
		want  string
	}{
		"no SKILL.md":               {[]File{{Path: "README.md", Content: "x"}}, "skill_missing@-"},
		"SKILL.md in a folder only": {[]File{{Path: "docs/SKILL.md", Content: valid().Content}}, "skill_missing@-"},
		"no front matter":           {[]File{{Path: SkillFile, Content: "# Just a body\n"}}, "skill_front_matter@SKILL.md"},
		"front matter not closed":   {[]File{{Path: SkillFile, Content: "---\nname: x\ndescription: y\n"}}, "skill_front_matter@SKILL.md"},
		"front matter not YAML":     {[]File{skillMD("name: [unclosed")}, "skill_front_matter@SKILL.md"},
		"front matter a list":       {[]File{skillMD("- name\n- description")}, "skill_front_matter@SKILL.md"},
		"metadata not text":         {[]File{skillMD("name: x\ndescription: y\nmetadata:\n  version: 2")}, "skill_front_matter@SKILL.md"},
		"metadata a list":           {[]File{skillMD("name: x\ndescription: y\nmetadata: [a]")}, "skill_front_matter@SKILL.md"},
		"no name":                   {[]File{skillMD("description: y")}, "skill_name@SKILL.md"},
		"a name in capitals":        {[]File{skillMD("name: Requirements\ndescription: y")}, "skill_name@SKILL.md"},
		"a name that is a number":   {[]File{skillMD("name: 12\ndescription: y")}, "skill_name@SKILL.md"},
		"no description":            {[]File{skillMD("name: x")}, "skill_description@SKILL.md"},
		"a blank description":       {[]File{skillMD("name: x\ndescription: '   '")}, "skill_description@SKILL.md"},
		"a description too long": {[]File{skillMD("name: x\ndescription: " +
			strings.Repeat("d", config.MaxSkillDescriptionChars+1))}, "skill_description@SKILL.md"},
		"an absolute path":            {[]File{valid(), {Path: "/etc/passwd", Content: "x"}}, "skill_path@/etc/passwd"},
		"a backslash":                 {[]File{valid(), {Path: `refs\a.md`, Content: "x"}}, `skill_path@refs\a.md`},
		"climbing out":                {[]File{valid(), {Path: "refs/../../a.md", Content: "x"}}, "skill_path@refs/../../a.md"},
		"a hidden file":               {[]File{valid(), {Path: "refs/.env", Content: "x"}}, "skill_path@refs/.env"},
		"a hidden folder":             {[]File{valid(), {Path: ".git/config", Content: "x"}}, "skill_path@.git/config"},
		"an empty folder name":        {[]File{valid(), {Path: "refs//a.md", Content: "x"}}, "skill_path@refs//a.md"},
		"a trailing slash":            {[]File{valid(), {Path: "refs/", Content: "x"}}, "skill_path@refs/"},
		"no path":                     {[]File{valid(), {Path: "", Content: "x"}}, "skill_path@"},
		"a control character":         {[]File{valid(), {Path: "a\tb.md", Content: "x"}}, "skill_path@a\tb.md"},
		"too deep":                    {[]File{valid(), {Path: deep, Content: "x"}}, "skill_path@" + deep},
		"one path twice":              {[]File{valid(), {Path: "a.md", Content: "x"}, {Path: "a.md", Content: "y"}}, "skill_path@a.md"},
		"a NUL byte":                  {[]File{valid(), {Path: "a.md", Content: "a\x00b"}}, "skill_binary@a.md"},
		"not UTF-8":                   {[]File{valid(), {Path: "logo.png", Content: "\x89PNG\xff\xfe"}}, "skill_binary@logo.png"},
		"a SKILL.md that is not text": {[]File{{Path: SkillFile, Content: "\xff"}}, "skill_binary@SKILL.md"},
		"a file too large": {[]File{valid(), {Path: "big.md", Content: strings.Repeat("x", config.MaxSkillFileBytes+1)}},
			"skill_too_large@big.md"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { refusedWith(t, c.files, c.want) })
	}
}

func TestTheWholeVersionIsBoundedInSizeAndFiles(t *testing.T) {
	files := []File{valid()}
	for i := 0; len(files) <= config.MaxSkillVersionBytes/config.MaxSkillFileBytes; i++ {
		files = append(files, File{Path: fmt.Sprintf("part-%d.md", i), Content: strings.Repeat("x", config.MaxSkillFileBytes)})
	}
	refusedWith(t, files, "skill_too_large@-")

	many := []File{valid()}
	for i := 1; i <= config.MaxSkillFiles; i++ {
		many = append(many, File{Path: fmt.Sprintf("f%02d.md", i), Content: "x"})
	}
	refusedWith(t, many, "skill_too_many_files@-")
	if draft, problems := Check(many[:config.MaxSkillFiles]); draft == nil {
		t.Errorf("exactly %d files are refused: %v", config.MaxSkillFiles, codes(problems))
	}
}

func TestTheLimitsThemselvesAreAllowed(t *testing.T) {
	files := []File{
		skillMD("name: " + strings.Repeat("a", config.MaxSkillNameChars) + "\ndescription: " +
			strings.Repeat("d", config.MaxSkillDescriptionChars)),
		{Path: strings.Repeat("a/", config.MaxSkillPathDepth) + "deepest.md", Content: "x"},
		{Path: "largest.md", Content: strings.Repeat("x", config.MaxSkillFileBytes)},
	}
	if draft, problems := Check(files); draft == nil {
		t.Errorf("a skill at every limit is refused: %v", codes(problems))
	}
}

func TestTheNameRuleIsAgentSkills(t *testing.T) {
	for name, want := range map[string]bool{
		"a": true, "pdf": true, "requirements-engineering": true, "v2-notes": true, "9lives": true,
		strings.Repeat("a", config.MaxSkillNameChars): true, strings.Repeat("a", config.MaxSkillNameChars+1): false,
		"": false, "Upper": false, "-lead": false, "trail-": false, "dou--ble": false, "under_score": false, "dot.ted": false,
		"sp ace": false, "umlaut-ä": false,
	} {
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestTheHashIsTheFilesWhateverTheirOrder(t *testing.T) {
	a := []File{{Path: SkillFile, Content: "one"}, {Path: "b.md", Content: "two"}}
	b := []File{{Path: "b.md", Content: "two"}, {Path: SkillFile, Content: "one"}}
	if Hash(a) != Hash(b) {
		t.Error("the same files in another order hash differently")
	}
	// Pinned, so a change to how the hash is made, which would make every stored hash wrong, is seen.
	if got := Hash([]File{{Path: "a", Content: "b"}}); got != "8fb20ef63ced4145fc2e983ffe597d1dcff39154c3bf21f0fa9dde6a0c50fdc9" {
		t.Errorf("the hash is %q", got)
	}
	for name, other := range map[string][]File{
		"a changed content": {{Path: SkillFile, Content: "one!"}, {Path: "b.md", Content: "two"}},
		"a renamed file":    {{Path: SkillFile, Content: "one"}, {Path: "c.md", Content: "two"}},
		"a moved boundary":  {{Path: SkillFile, Content: "on"}, {Path: "eb.md", Content: "two"}},
		"a file fewer":      {{Path: SkillFile, Content: "one"}},
	} {
		if Hash(other) == Hash(a) {
			t.Errorf("%s hashes as the original", name)
		}
	}
}

func TestADiffNamesEachFileAddedRemovedOrChanged(t *testing.T) {
	from := []File{{Path: SkillFile, Content: "a\nb\n"}, {Path: "gone.md", Content: "old\n"}, {Path: "same.md", Content: "s\n"}}
	to := []File{{Path: SkillFile, Content: "a\nc\n"}, {Path: "new.md", Content: "fresh\n"}, {Path: "same.md", Content: "s\n"}}
	got := DiffFiles(from, to)
	var seen []string
	for _, f := range got {
		seen = append(seen, f.Status+" "+f.Path)
	}
	want := []string{"changed SKILL.md", "removed gone.md", "added new.md"}
	if !slices.Equal(seen, want) {
		t.Fatalf("the diff names %v, want %v", seen, want)
	}
	if lines := got[1].Hunks[0].Lines; len(lines) != 1 || lines[0].Op != "delete" || lines[0].Text != "old" {
		t.Errorf("a removed file's hunk is %v", got[1].Hunks)
	}
	if lines := got[2].Hunks[0].Lines; len(lines) != 1 || lines[0].Op != "insert" || lines[0].Text != "fresh" {
		t.Errorf("an added file's hunk is %v", got[2].Hunks)
	}
	if len(DiffFiles(from, from)) != 0 {
		t.Error("a version compared with itself shows changes")
	}
}
