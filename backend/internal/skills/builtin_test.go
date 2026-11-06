package skills

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// shippedSource is skills/ at the repository's root, the single source of the embedded copy.
var shippedSource = filepath.Join("..", "..", "..", "skills")

// shippedVersion is the version each shipped skill declares today; the default workflows pin it.
const shippedVersion = 1

// typographic are the en and em dash and the typographic quotes, which the repository's style keeps out.
var typographic = []rune{0x2013, 0x2014, 0x2018, 0x2019, 0x201c, 0x201d}

func TestEachShippedSkillPassesTheLibrarysChecks(t *testing.T) {
	shipped, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, one := range shipped {
		names = append(names, one.Draft.Name)
		if one.Version != shippedVersion {
			t.Errorf("%s declares version %d, want %d", one.Draft.Name, one.Version, shippedVersion)
		}
		if one.Draft.FrontMatter["license"] != "Apache-2.0" {
			t.Errorf("%s carries the license %v, want Rotor's", one.Draft.Name, one.Draft.FrontMatter["license"])
		}
		var paths []string
		for _, f := range one.Draft.Files {
			paths = append(paths, f.Path)
			if i := strings.IndexFunc(f.Content, func(r rune) bool { return slices.Contains(typographic, r) }); i >= 0 {
				t.Errorf("%s/%s holds a typographic dash or quote at byte %d", one.Draft.Name, f.Path, i)
			}
		}
		if !slices.Equal(paths, []string{"SKILL.md", "references/templates.md"}) {
			t.Errorf("%s ships %v", one.Draft.Name, paths)
		}
	}
	if !slices.Equal(names, []string{"requirements-engineering", "test-engineering"}) {
		t.Errorf("Rotor ships %v", names)
	}
}

// The skills installed are the ones in skills/, which people read and change.
func TestTheEmbeddedSkillsAreTheOnesInSkills(t *testing.T) {
	read := func(fsys fs.FS) map[string][]byte {
		out := map[string][]byte{}
		err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			content, err := fs.ReadFile(fsys, p)
			out[p] = content
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	embedded, err := fs.Sub(builtinFS, builtinRoot)
	if err != nil {
		t.Fatal(err)
	}
	inRepo, inBackend := read(os.DirFS(shippedSource)), read(embedded)
	if len(inRepo) == 0 {
		t.Fatal("no skills found in skills/")
	}
	for path, content := range inRepo {
		if embeddedContent, ok := inBackend[path]; !ok || !bytes.Equal(content, embeddedContent) {
			t.Errorf("backend/internal/skills/builtin/%s differs from skills/; run make skills-builtin", path)
		}
	}
	for path := range inBackend {
		if _, ok := inRepo[path]; !ok {
			t.Errorf("backend/internal/skills/builtin/%s is not in skills/; run make skills-builtin", path)
		}
	}
}

func TestAShippedSkillIsHeldToTheLibrarysRulesAndDeclaresItsVersion(t *testing.T) {
	skill := func(frontMatter string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("---\n" + frontMatter + "\n---\n\nDo it.\n")}
	}
	versioned := "name: good\ndescription: Good.\nmetadata:\n  version: "
	for why, c := range map[string]struct {
		files fstest.MapFS
		want  string
	}{
		"no version":                        {fstest.MapFS{"s/good/SKILL.md": skill("name: good\ndescription: Good.")}, "declares no metadata.version"},
		"a version that is no whole number": {fstest.MapFS{"s/good/SKILL.md": skill(versioned + `"1.0"`)}, "declares no metadata.version"},
		"a version of 0":                    {fstest.MapFS{"s/good/SKILL.md": skill(versioned + `"0"`)}, "declares no metadata.version"},
		"a folder that is not its name":     {fstest.MapFS{"s/other/SKILL.md": skill(versioned + `"1"`)}, "name its folder after it"},
		"a hidden file": {fstest.MapFS{
			"s/good/SKILL.md": skill(versioned + `"1"`),
			"s/good/.notes":   &fstest.MapFile{Data: []byte("x")},
		}, "fails the library's checks"},
		"no description": {fstest.MapFS{"s/good/SKILL.md": skill("name: good\nmetadata:\n  version: \"1\"")}, "fails the library's checks"},
	} {
		if _, err := readShipped(c.files, "s"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", why, err, c.want)
		}
	}
	shipped, err := readShipped(fstest.MapFS{"s/good/SKILL.md": skill(versioned + `"7"`)}, "s")
	if err != nil || len(shipped) != 1 || shipped[0].Version != 7 || shipped[0].Draft.Hash == "" {
		t.Errorf("a good skill read as %+v (%v)", shipped, err)
	}
}
