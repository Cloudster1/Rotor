package httpapi

import (
	"net/http"
	"reflect"
	"time"

	"github.com/cloudster1/rotor/backend/internal/openapi"
	"github.com/cloudster1/rotor/backend/internal/skills"
)

// The rows of M17: the skills library (#219, #220); docs/api-contract-m17.md says what they mean. Import and export
// answer 501 until #220 builds them.
func init() {
	operations = append(operations, m17Operations...)
}

// skillFileStatus is what happened to a file between two versions.
type skillFileStatus string

func registerM17Schemas(b *openapi.Builder) {
	b.Enums[reflect.TypeOf(skillFileStatus(""))] = []string{skills.FileAdded, skills.FileRemoved, skills.FileChanged}
}

var m17Operations = []operation{
	{method: "GET", path: "/skills", handler: "handleListSkills", tag: "skills",
		summary:     "The skills you may read, built-in ones included.",
		description: "archived lists archived skills instead; q matches a name or a description.",
		query: append([]param{{name: "archived", description: "true lists archived skills instead."},
			{name: "q", description: "Part of a name or a description."}}, pageQuery...),
		responses: ok(skillList{})},
	{method: "POST", path: "/skills", handler: "handleCreateSkill", tag: "skills",
		summary:     "Make a skill from its files; its SKILL.md names it, and the first save is version 1.",
		description: "For whoever holds skill.create. Files the checks refuse answer 422 with the problems, each with its code.",
		request:     createSkillRequest{},
		responses: map[int]any{http.StatusCreated: skillResponse{}, http.StatusForbidden: errorEnvelope{},
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "POST", path: "/skills/validate", handler: "handleValidateSkill", tag: "skills", reads: true,
		summary:     "Check a skill's files as a save would, without saving, and answer the problems.",
		description: "What the files alone tell: whether another skill has the name is answered by the save.",
		request:     validateSkillRequest{}, responses: ok(validateSkillResponse{})},
	{method: "POST", path: "/skills/import", handler: "handleImportSkill", tag: "skills", pending: true,
		summary: "Make a skill from a zip of one skill, or save the next version of the skill with that name.",
		description: "The body is the zip itself (application/zip, at most 2 MiB); the comment goes in the query. For whoever holds " +
			"skill.create, or skill.edit on the skill named. The answer says which happened.",
		query:     []param{{name: "comment", description: "The note the version carries, at most 500 characters."}},
		responses: map[int]any{http.StatusOK: importedSkill{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/skills/{skillID}", handler: "handleGetSkill", tag: "skills",
		summary:   "A skill with its current version's front matter and files, and what you may do with it.",
		responses: map[int]any{http.StatusOK: skillResponse{}, http.StatusNotFound: notFound}},
	{method: "PUT", path: "/skills/{skillID}", handler: "handleSaveSkill", tag: "skills",
		summary:     "Save the next version of a skill.",
		description: "baseVersion must be the current version, or the save is refused with 409 so nobody overwrites a colleague unseen.",
		request:     saveSkillRequest{},
		responses: map[int]any{http.StatusOK: skillResponse{}, http.StatusNotFound: notFound, http.StatusForbidden: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "DELETE", path: "/skills/{skillID}", handler: "handleArchiveSkill", tag: "skills",
		summary:     "Archive a skill; its versions stay for the workflows that pin them.",
		description: "For whoever holds skill.administer on it. Skills are archived, never deleted; a built-in one is refused with 403.",
		responses:   map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound, http.StatusForbidden: errorEnvelope{}}},
	{method: "POST", path: "/skills/{skillID}/restore", handler: "handleRestoreSkill", tag: "skills",
		summary:     "Take a skill out of the archive, which undoes archiving it.",
		description: "For whoever holds skill.administer on it.",
		responses:   map[int]any{http.StatusOK: skillResponse{}, http.StatusNotFound: notFound, http.StatusForbidden: errorEnvelope{}}},
	{method: "GET", path: "/skills/{skillID}/versions", handler: "handleListSkillVersions", tag: "skills",
		summary:   "Every version of a skill, newest first.",
		responses: map[int]any{http.StatusOK: env{"versions": []skillVersionSummary{}}, http.StatusNotFound: notFound}},
	{method: "GET", path: "/skills/{skillID}/versions/{versionNumber}", handler: "handleGetSkillVersion", tag: "skills",
		summary:   "One version of a skill: its front matter, comment, author and the path, size and hash of each file.",
		responses: map[int]any{http.StatusOK: skillVersionResponse{}, http.StatusNotFound: notFound}},
	{method: "GET", path: "/skills/{skillID}/versions/{versionNumber}/file", handler: "handleGetSkillFile", tag: "skills",
		summary: "One file of a version, as text.",
		query:   []param{{name: "path", description: "The file's path below the skill's root, such as references/templates.md."}},
		responses: map[int]any{http.StatusOK: skillFileResponse{}, http.StatusNotFound: notFound,
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/skills/{skillID}/versions/{versionNumber}/export", handler: "handleExportSkillVersion", tag: "skills",
		pending: true, summary: "One version as a zip that imports unchanged.",
		description: "The body is application/zip.",
		responses:   map[int]any{http.StatusOK: nil, http.StatusNotFound: notFound}},
	{method: "GET", path: "/skills/{skillID}/diff", handler: "handleDiffSkillVersions", tag: "skills",
		summary: "The files that changed between two versions, line by line.",
		query:   []param{{name: "from", description: "The older version's number."}, {name: "to", description: "The newer version's number."}},
		responses: map[int]any{http.StatusOK: skillDiffResponse{}, http.StatusNotFound: notFound,
			http.StatusUnprocessableEntity: errorEnvelope{}}},
	{method: "GET", path: "/skills/{skillID}/grants", handler: "handleListSkillGrants", tag: "skills",
		summary:   "The roles granted on this skill, for anybody who may read it.",
		responses: map[int]any{http.StatusOK: env{"grants": []assignmentView{}}, http.StatusNotFound: notFound}},
	{method: "POST", path: "/skills/{skillID}/grants", handler: "handleGrantOnSkill", tag: "skills",
		summary:     "Grant a person or a group a role on this skill, such as Skill reader or Skill editor.",
		description: "For whoever holds skill.administer on it, from a browser session and never an assistant.",
		request:     workflowGrantRequest{},
		responses: map[int]any{http.StatusCreated: env{"grant": assignmentView{}}, http.StatusNotFound: notFound,
			http.StatusForbidden: refused, http.StatusUnprocessableEntity: refused}},
	{method: "DELETE", path: "/skills/{skillID}/grants/{grantID}", handler: "handleRevokeOnSkill", tag: "skills",
		summary:     "Take a grant on this skill away.",
		description: "For whoever holds skill.administer on it, from a browser session and never an assistant.",
		responses:   map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound, http.StatusForbidden: refused}},
}

// skillFile is a file as a version lists it.
type skillFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// skillVersionSummary is a version in a list; createdBy is null for a version the api installed.
type skillVersionSummary struct {
	Number      int        `json:"number"`
	Description string     `json:"description"`
	Comment     string     `json:"comment"`
	Hash        string     `json:"hash"`
	Bytes       int        `json:"bytes"`
	CreatedBy   *personRef `json:"createdBy"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// skillVersionResponse is a version with its front matter and file list.
type skillVersionResponse struct {
	skillVersionSummary
	FrontMatter map[string]any `json:"frontMatter"`
	Files       []skillFile    `json:"files"`
}

// skillSummary is a skill in a list.
type skillSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Owner is null for a built-in skill.
	Owner          *personRef `json:"owner"`
	CurrentVersion int        `json:"currentVersion"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	Archived       bool       `json:"archived"`
	// Builtin skills ship with Rotor, have no owner and are changed by nobody through the API.
	Builtin       bool `json:"builtin"`
	CanEdit       bool `json:"canEdit"`
	CanAdminister bool `json:"canAdminister"`
}

// skillList is a page of skills; total counts all that match.
type skillList struct {
	Skills []skillSummary `json:"skills"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// skillResponse is a skill with its current version.
type skillResponse struct {
	Skill   skillSummary         `json:"skill"`
	Version skillVersionResponse `json:"version"`
}

// skillFileBody is a file to save: a path below the skill's root and its UTF-8 text.
type skillFileBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// createSkillRequest makes a skill from its files, SKILL.md among them.
type createSkillRequest struct {
	Files   []skillFileBody `json:"files"`
	Comment string          `json:"comment,omitempty"`
}

// saveSkillRequest saves the next version of a skill; baseVersion is the version the change was made from.
type saveSkillRequest struct {
	BaseVersion int             `json:"baseVersion"`
	Files       []skillFileBody `json:"files"`
	Comment     string          `json:"comment,omitempty"`
}

// validateSkillRequest is the files a save would take.
type validateSkillRequest struct {
	Files []skillFileBody `json:"files"`
}

// validateSkillResponse lists what is wrong; valid is true when nothing is.
type validateSkillResponse struct {
	Valid    bool      `json:"valid"`
	Problems []problem `json:"problems"`
}

// importedSkill says whether an import made the skill or saved its next version.
type importedSkill struct {
	skillResponse
	Created bool `json:"created"`
}

// skillFileResponse is one file's text.
type skillFileResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256"`
}

// skillDiffResponse is the files that changed between two versions, each with its hunks.
type skillDiffResponse struct {
	From  int             `json:"from"`
	To    int             `json:"to"`
	Files []skillFileDiff `json:"files"`
}

// skillFileDiff is one file's change; added and removed files have all their lines in hunks.
type skillFileDiff struct {
	Path   string          `json:"path"`
	Status skillFileStatus `json:"status"`
	Hunks  []diffHunk      `json:"hunks"`
}
