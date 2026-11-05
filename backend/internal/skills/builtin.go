package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
)

// builtinFS is the copy of skills/ that make skills-builtin keeps beside this file (#225).
//
//go:embed builtin
var builtinFS embed.FS

// builtinRoot is the folder of builtinFS that holds one folder per shipped skill.
const builtinRoot = "builtin"

// Shipped is a skill Rotor ships: its checked files and the version its metadata.version declares.
type Shipped struct {
	Draft   *Draft
	Version int
}

// Builtins reads the shipped skills and holds each to the checks a save makes, so Rotor ships none a person could not save.
func Builtins() ([]Shipped, error) {
	return readShipped(builtinFS, builtinRoot)
}

// readShipped reads each folder below root as one skill.
func readShipped(fsys fs.FS, root string) ([]Shipped, error) {
	folders, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, err
	}
	var out []Shipped
	for _, folder := range folders {
		if !folder.IsDir() {
			continue
		}
		one, err := readOneShipped(fsys, path.Join(root, folder.Name()), folder.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	slices.SortFunc(out, func(a, b Shipped) int { return strings.Compare(a.Draft.Name, b.Draft.Name) })
	return out, nil
}

// readOneShipped reads one shipped skill's folder; its name must be the folder's, so skills/ reads as the library does.
func readOneShipped(fsys fs.FS, dir, folder string) (Shipped, error) {
	var files []File
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		files = append(files, File{Path: strings.TrimPrefix(p, dir+"/"), Content: string(content)})
		return nil
	})
	if err != nil {
		return Shipped{}, err
	}
	draft, problems := Check(files)
	if len(problems) > 0 {
		return Shipped{}, fmt.Errorf("the shipped skill %s fails the library's checks: %s", folder, problems[0].Message)
	}
	if draft.Name != folder {
		return Shipped{}, fmt.Errorf("the shipped skill in skills/%s is named %s; name its folder after it", folder, draft.Name)
	}
	version, ok := declaredVersion(draft.FrontMatter)
	if !ok {
		return Shipped{}, fmt.Errorf("the shipped skill %s declares no metadata.version; give it a whole number from 1, quoted", folder)
	}
	return Shipped{Draft: draft, Version: version}, nil
}

// declaredVersion is metadata.version as a whole number from 1, which a built-in skill's versions are numbered by.
func declaredVersion(frontMatter map[string]any) (int, bool) {
	metadata, _ := frontMatter["metadata"].(map[string]any)
	text, _ := metadata["version"].(string)
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || strconv.Itoa(n) != text {
		return 0, false
	}
	return n, true
}
