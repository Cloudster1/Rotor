package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Problem codes, as docs/api-contract-m17.md lists them.
const (
	CodeMissing       = "skill_missing"
	CodeFrontMatter   = "skill_front_matter"
	CodeName          = "skill_name"
	CodeNameTaken     = "skill_name_taken"
	CodeNameChanged   = "skill_name_changed"
	CodeDescription   = "skill_description"
	CodePath          = "skill_path"
	CodeBinary        = "skill_binary"
	CodeTooLarge      = "skill_too_large"
	CodeTooManyFiles  = "skill_too_many_files"
	CodeNotZip        = "skill_not_zip"
	CodeCommentLength = "comment_too_long"
)

// SkillFile is the file every skill has at its root, which names and describes it.
const SkillFile = "SKILL.md"

// Whole is the path a problem of the whole version carries rather than one file's.
const Whole = "-"

// frontMatterFence opens and closes the YAML at the top of SKILL.md.
const frontMatterFence = "---"

// nameRule is Agent Skills' own: lowercase letters, digits and single dashes, starting and ending with a letter or digit.
var nameRule = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// File is one file of a skill as somebody saves it: a path below the skill's root and its text.
type File struct {
	Path    string
	Content string
}

// Problem is one thing a skill is refused for; Path is the file, or Whole for the version.
type Problem struct {
	Code    string
	Path    string
	Message string
}

// Draft is a version that passed the checks, ready to save.
type Draft struct {
	Name        string
	Description string
	FrontMatter map[string]any
	// Files are sorted by path, which is the order the hash reads them in.
	Files []File
	Bytes int
	Hash  string
}

// ValidName reports whether a name follows the name rule.
func ValidName(name string) bool {
	return utf8.RuneCountInString(name) <= config.MaxSkillNameChars && nameRule.MatchString(name)
}

// Check reads a version's files as a save would and lists everything wrong; the draft is nil unless nothing is.
func Check(files []File) (*Draft, []Problem) {
	var problems []Problem
	add := func(code, path, format string, args ...any) {
		problems = append(problems, Problem{Code: code, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if len(files) > config.MaxSkillFiles {
		add(CodeTooManyFiles, Whole, "A skill holds at most %d files, SKILL.md included; this one has %d. Merge or remove some.",
			config.MaxSkillFiles, len(files))
	}
	sorted := slices.Clone(files)
	slices.SortStableFunc(sorted, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	total := 0
	var skillMD *File
	for i, f := range sorted {
		total += len(f.Content)
		if i > 0 && sorted[i-1].Path == f.Path {
			add(CodePath, f.Path, "The path %s is given twice. Keep one file under each path.", f.Path)
			continue
		}
		if why := pathProblem(f.Path); why != "" {
			add(CodePath, f.Path, "%s", why)
			continue
		}
		if !utf8.ValidString(f.Content) || strings.ContainsRune(f.Content, 0) {
			add(CodeBinary, f.Path, "The file %s is not UTF-8 text. A skill holds text files alone, so leave images and programs out.", f.Path)
			continue
		}
		if len(f.Content) > config.MaxSkillFileBytes {
			add(CodeTooLarge, f.Path, "The file %s is %d bytes, and a file of a skill holds at most %d. Split it into smaller files.",
				f.Path, len(f.Content), config.MaxSkillFileBytes)
		}
		if f.Path == SkillFile {
			skillMD = &sorted[i]
		}
	}
	if total > config.MaxSkillVersionBytes {
		add(CodeTooLarge, Whole, "The skill's files come to %d bytes; a version holds at most %d. Make them shorter or fewer.",
			total, config.MaxSkillVersionBytes)
	}
	draft := &Draft{Files: sorted, Bytes: total}
	if skillMD == nil {
		add(CodeMissing, Whole, "A skill needs a SKILL.md at its root, with a name and a description in its front matter.")
	} else {
		problems = append(problems, draft.readFrontMatter(skillMD.Content)...)
	}
	if len(problems) > 0 {
		return nil, problems
	}
	draft.Hash = Hash(sorted)
	return draft, nil
}

// pathProblem says what is wrong with a path, or nothing when it follows the path rule.
func pathProblem(path string) string {
	switch {
	case path == "":
		return "A file has no path. Give each file its path below the skill's root, such as references/guide.md."
	case strings.HasPrefix(path, "/"):
		return fmt.Sprintf("The path %s starts with a slash. Give it relative to the skill's root.", path)
	case strings.Contains(path, `\`):
		return fmt.Sprintf("The path %s holds a backslash. Separate folders with a forward slash.", path)
	case !utf8.ValidString(path) || strings.ContainsFunc(path, func(r rune) bool { return r < ' ' }):
		return "A path holds a character that cannot be part of a file name. Rename the file."
	}
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		switch {
		case segment == "":
			return fmt.Sprintf("The path %s has an empty folder name. Remove the doubled or trailing slash.", path)
		case segment == "..":
			return fmt.Sprintf("The path %s climbs out of the skill. Keep every file below the skill's root.", path)
		case strings.HasPrefix(segment, "."):
			return fmt.Sprintf("The path %s has a name starting with a dot. Rename it, since hidden files are not part of a skill.", path)
		}
	}
	if depth := len(segments) - 1; depth > config.MaxSkillPathDepth {
		return fmt.Sprintf("The file %s sits %d folders deep, and a skill's files sit at most %d below its root. Move it up.",
			path, depth, config.MaxSkillPathDepth)
	}
	return ""
}

// readFrontMatter fills the draft from SKILL.md's front matter and answers what is wrong with it.
func (d *Draft) readFrontMatter(content string) []Problem {
	refuse := func(code, message string) []Problem {
		return []Problem{{Code: code, Path: SkillFile, Message: message}}
	}
	raw, ok := frontMatter(content)
	if !ok {
		return refuse(CodeFrontMatter, "SKILL.md must begin with front matter: a line of ---, YAML with a name and a "+
			"description, and another line of ---.")
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return refuse(CodeFrontMatter, "The front matter of SKILL.md is not a YAML mapping. Write it as key: value lines.")
	}
	fields, _ = plain(fields).(map[string]any)
	if metadata, present := fields["metadata"]; present {
		values, isMap := metadata.(map[string]any)
		for _, v := range values {
			if _, isString := v.(string); !isString {
				isMap = false
			}
		}
		if !isMap {
			return refuse(CodeFrontMatter, "The metadata in SKILL.md maps names to text. Quote each value, as in version: \"2\".")
		}
	}
	var problems []Problem
	name, _ := fields["name"].(string)
	if !ValidName(name) {
		problems = append(problems, Problem{Code: CodeName, Path: SkillFile, Message: fmt.Sprintf("The name in SKILL.md must be "+
			"lowercase letters, digits and single dashes, at most %d characters, starting and ending with a letter or digit, "+
			"such as requirements-engineering.", config.MaxSkillNameChars)})
	}
	description, _ := fields["description"].(string)
	description = strings.TrimSpace(description)
	if description == "" || utf8.RuneCountInString(description) > config.MaxSkillDescriptionChars {
		problems = append(problems, Problem{Code: CodeDescription, Path: SkillFile, Message: fmt.Sprintf("The description in "+
			"SKILL.md must say what the skill does and when to use it, in 1 to %d characters.", config.MaxSkillDescriptionChars)})
	}
	d.Name, d.Description, d.FrontMatter = name, description, fields
	return problems
}

// frontMatter is the YAML between the fences at the very top of SKILL.md.
func frontMatter(content string) (string, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	first, rest, found := strings.Cut(content, "\n")
	if !found || strings.TrimRight(first, " \t") != frontMatterFence {
		return "", false
	}
	var yamlLines []string
	for line := range strings.SplitSeq(rest, "\n") {
		if strings.TrimRight(line, " \t") == frontMatterFence {
			return strings.Join(yamlLines, "\n"), true
		}
		yamlLines = append(yamlLines, line)
	}
	return "", false
}

// plain turns what YAML decoded into what JSON encodes: a mapping keyed by anything becomes one keyed by text.
func plain(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, inner := range x {
			x[k] = plain(inner)
		}
		return x
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, inner := range x {
			out[fmt.Sprint(k)] = plain(inner)
		}
		return out
	case []any:
		for i, inner := range x {
			x[i] = plain(inner)
		}
		return x
	}
	return v
}

// Hash is sha256 over the files sorted by path, each as its path, a NUL, its content and a NUL: neither holds a NUL,
// so no two sets of files read the same.
func Hash(files []File) string {
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	var b bytes.Buffer
	for _, f := range sorted {
		b.WriteString(f.Path)
		b.WriteByte(0)
		b.WriteString(f.Content)
		b.WriteByte(0)
	}
	sum := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(sum[:])
}

// fileHash is one file's sha256, as skill_file holds it.
func fileHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
