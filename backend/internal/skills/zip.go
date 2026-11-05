package skills

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// ReadZip unpacks a zip of one skill, with SKILL.md at its root or in the one folder that holds everything, into the
// files the checks read. Sizes are judged before anything is unpacked, and again while it is, since a header can lie.
func ReadZip(data []byte) ([]File, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, refuseZip(Problem{Code: CodeNotZip, Path: Whole,
			Message: "This is not a zip file Rotor can read. Zip the skill's folder and send that."})
	}
	var entries []*zip.File
	for _, f := range reader.File {
		if !f.FileInfo().IsDir() {
			entries = append(entries, f)
		}
	}
	if problems := judgeEntries(entries); len(problems) > 0 {
		return nil, &Invalid{Problems: problems}
	}
	prefix, problem := skillRoot(entries)
	if problem != nil {
		return nil, &Invalid{Problems: []Problem{*problem}}
	}
	files := make([]File, 0, len(entries))
	used := 0
	for _, f := range entries {
		content, problem := unpack(f, used)
		if problem != nil {
			return nil, &Invalid{Problems: []Problem{*problem}}
		}
		used += len(content)
		files = append(files, File{Path: strings.TrimPrefix(f.Name, prefix), Content: content})
	}
	return files, nil
}

func refuseZip(p Problem) error { return &Invalid{Problems: []Problem{p}} }

// judgeEntries refuses what the zip's directory alone shows to be wrong, so nothing is unpacked for it.
func judgeEntries(entries []*zip.File) []Problem {
	var problems []Problem
	if len(entries) > config.MaxSkillFiles {
		problems = append(problems, Problem{Code: CodeTooManyFiles, Path: Whole, Message: fmt.Sprintf("A skill holds at most "+
			"%d files, SKILL.md included; this zip has %d. Merge or remove some.", config.MaxSkillFiles, len(entries))})
	}
	var declared uint64
	for _, f := range entries {
		declared += f.UncompressedSize64
		switch {
		case !f.Mode().IsRegular():
			problems = append(problems, Problem{Code: CodePath, Path: f.Name, Message: fmt.Sprintf("The entry %s is a link or "+
				"another special file. Put the real file in the zip instead.", f.Name)})
		case escapes(f.Name):
			problems = append(problems, Problem{Code: CodePath, Path: f.Name, Message: pathProblem(f.Name)})
		case f.UncompressedSize64 > config.MaxSkillFileBytes:
			problems = append(problems, Problem{Code: CodeTooLarge, Path: f.Name, Message: fmt.Sprintf("The file %s is %d bytes "+
				"unpacked, and a file of a skill holds at most %d. Split it into smaller files.", f.Name, f.UncompressedSize64,
				config.MaxSkillFileBytes)})
		}
	}
	if declared > config.MaxSkillVersionBytes {
		problems = append(problems, Problem{Code: CodeTooLarge, Path: Whole, Message: fmt.Sprintf("The zip unpacks to %d bytes; "+
			"a version holds at most %d. Make the files shorter or fewer.", declared, config.MaxSkillVersionBytes)})
	}
	return problems
}

// escapes is a name that leaves the skill's folder or is written for another system's paths.
func escapes(name string) bool {
	return strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || slices.Contains(strings.Split(name, "/"), "..")
}

// skillRoot is the folder SKILL.md sits in: the zip's root, or the one folder everything is in, as a prefix to strip.
func skillRoot(entries []*zip.File) (string, *Problem) {
	folders := map[string]bool{}
	for _, f := range entries {
		if f.Name == SkillFile {
			return "", nil
		}
		folder, _, nested := strings.Cut(f.Name, "/")
		if !nested {
			folder = ""
		}
		folders[folder] = true
	}
	if len(folders) == 1 && !folders[""] {
		for folder := range folders {
			return folder + "/", nil
		}
	}
	if len(folders) == 0 {
		return "", &Problem{Code: CodeMissing, Path: Whole, Message: "The zip holds no files. Zip a skill's folder, with a SKILL.md in it."}
	}
	return "", &Problem{Code: CodeMissing, Path: Whole, Message: "A skill needs a SKILL.md at the zip's root or in the one folder " +
		"that holds all its files. Zip a single skill's folder."}
}

// unpack reads one entry, never past what the skill may still hold, whatever its header says.
func unpack(f *zip.File, used int) (string, *Problem) {
	rc, err := f.Open()
	if err != nil {
		return "", &Problem{Code: CodeNotZip, Path: f.Name, Message: fmt.Sprintf("The entry %s cannot be read from the zip. Zip the folder again.", f.Name)}
	}
	defer rc.Close()
	room := min(config.MaxSkillFileBytes, config.MaxSkillVersionBytes-used)
	content, err := io.ReadAll(io.LimitReader(rc, int64(room)+1))
	switch {
	case len(content) > config.MaxSkillFileBytes:
		return "", &Problem{Code: CodeTooLarge, Path: f.Name, Message: fmt.Sprintf("The file %s unpacks to more than %d bytes, "+
			"and a file of a skill holds at most that. Split it into smaller files.", f.Name, config.MaxSkillFileBytes)}
	case used+len(content) > config.MaxSkillVersionBytes:
		return "", &Problem{Code: CodeTooLarge, Path: Whole, Message: fmt.Sprintf("The zip unpacks to more than %d bytes, "+
			"which is all a version holds. Make the files shorter or fewer.", config.MaxSkillVersionBytes)}
	case err != nil:
		return "", &Problem{Code: CodeNotZip, Path: f.Name, Message: fmt.Sprintf("The entry %s is damaged or unpacks to more than its header says. Zip the folder again.", f.Name)}
	}
	return string(content), nil
}

// WriteZip packs files under one folder named after the skill, SKILL.md first and the rest by path, with fixed times,
// so the same files always make the same bytes.
func WriteZip(name string, files []File) ([]byte, error) {
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b File) int {
		if (a.Path == SkillFile) != (b.Path == SkillFile) {
			if a.Path == SkillFile {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	modified := time.Unix(config.SkillZipModifiedUnix, 0).UTC()
	for _, f := range sorted {
		h := &zip.FileHeader{Name: name + "/" + f.Path, Method: zip.Deflate, Modified: modified}
		h.SetMode(config.SkillZipFileMode)
		entry, err := w.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(entry, f.Content); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
