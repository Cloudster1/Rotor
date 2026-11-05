package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/skills"
	"github.com/cloudster1/rotor/backend/internal/textdiff"
)

// Skills is the skill library (#219); *skills.Service is the real one.
type Skills interface {
	List(ctx context.Context, f skills.Filter) (skills.Page, error)
	Skill(ctx context.Context, id uuid.UUID) (skills.Skill, error)
	Get(ctx context.Context, id uuid.UUID) (skills.Skill, skills.Version, error)
	Versions(ctx context.Context, id uuid.UUID) ([]skills.Version, error)
	Version(ctx context.Context, id uuid.UUID, number int) (skills.Version, error)
	File(ctx context.Context, id uuid.UUID, number int, path string) (skills.FileContent, error)
	Diff(ctx context.Context, id uuid.UUID, from, to int) (skills.Diff, error)
	Validate(files []skills.File) []skills.Problem
	Create(ctx context.Context, files []skills.File, comment string) (uuid.UUID, error)
	Save(ctx context.Context, id uuid.UUID, base int, files []skills.File, comment string) error
	Import(ctx context.Context, zipped []byte, comment string) (id uuid.UUID, created bool, err error)
	Export(ctx context.Context, id uuid.UUID, number int) (name string, zipped []byte, err error)
	Archive(ctx context.Context, id uuid.UUID) error
	Restore(ctx context.Context, id uuid.UUID) error
}

var (
	errSkillsUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "skills_unavailable",
		Message: "Skills are not set up on this installation. An operator connects Rotor to its database."}
	errNoSuchSkill   = ErrNotFound("There is no such skill, or it is not shared with you.")
	errSkillNotYours = ErrForbidden("Your grants on this skill do not let you do this. Ask one of its administrators for the role, " +
		"or make a skill of your own from its files.")
	errCannotCreateSkill = ErrForbidden("Your grants do not let you create skills. Ask an administrator of Rotor to grant you Author.")
	errSkillStale        = &APIError{Status: http.StatusConflict, Code: "skill_changed",
		Message: "Someone saved this skill since you opened it. Reload it, then make your change again."}
	errSkillArchived = &APIError{Status: http.StatusConflict, Code: "skill_archived",
		Message: "This skill is archived. Restore it first, or make a new skill from its files."}
	errSkillBuiltin = ErrForbidden("This skill ships with Rotor, so nobody changes or shares it here; everyone reads it already. " +
		"Make a skill of your own from its files to change it.")
	errNotSkillAdministrator = ErrForbidden("Only an administrator of this skill or of Rotor may change who holds its roles. " +
		"Ask one of them to do it.")
)

// skillError says what went wrong with a skill in the words its caller needs.
func skillError(err error) error {
	var invalid *skills.Invalid
	switch {
	case errors.As(err, &invalid):
		fields := map[string]string{}
		for _, p := range invalid.Problems {
			if _, taken := fields[p.Path]; !taken {
				fields[p.Path] = p.Message
			}
		}
		first := "the skill has problems"
		if len(invalid.Problems) > 0 {
			first = invalid.Problems[0].Message
		}
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "skill_invalid", Fields: fields, Problems: toSkillProblems(invalid.Problems),
			Message: fmt.Sprintf("The skill has %d problem(s) to fix before it is saved. The first: %s", len(invalid.Problems), first)}
	case errors.Is(err, skills.ErrNotFound):
		return errNoSuchSkill
	case errors.Is(err, skills.ErrForbidden):
		return errSkillNotYours
	case errors.Is(err, skills.ErrCannotCreate):
		return errCannotCreateSkill
	case errors.Is(err, skills.ErrStale):
		return errSkillStale
	case errors.Is(err, skills.ErrArchived):
		return errSkillArchived
	case errors.Is(err, skills.ErrBuiltin):
		return errSkillBuiltin
	}
	return err
}

// toSkillProblems is the checks' problems as the API shows them, never null.
func toSkillProblems(in []skills.Problem) []problem {
	out := make([]problem, 0, len(in))
	for _, p := range in {
		out = append(out, problem{Path: p.Path, Code: p.Code, Message: p.Message})
	}
	return out
}

func toSkillSummary(k skills.Skill) skillSummary {
	out := skillSummary{ID: k.ID.String(), Name: k.Name, Description: k.Description, CurrentVersion: k.CurrentVersion,
		UpdatedAt: k.UpdatedAt, Archived: k.ArchivedAt != nil, Builtin: k.Builtin, CanEdit: k.CanEdit, CanAdminister: k.CanAdminister}
	if k.OwnerID != nil {
		out.Owner = &personRef{ID: k.OwnerID.String(), Name: k.OwnerName}
	}
	return out
}

func toSkillVersionSummary(v skills.Version) skillVersionSummary {
	out := skillVersionSummary{Number: v.Number, Description: v.Description, Comment: v.Comment, Hash: v.Hash, Bytes: v.Bytes,
		CreatedAt: v.CreatedAt}
	if v.CreatedByID != nil {
		out.CreatedBy = &personRef{ID: v.CreatedByID.String(), Name: v.CreatedByName}
	}
	return out
}

func toSkillVersionResponse(v skills.Version) skillVersionResponse {
	out := skillVersionResponse{skillVersionSummary: toSkillVersionSummary(v), FrontMatter: v.FrontMatter,
		Files: make([]skillFile, 0, len(v.Files))}
	if out.FrontMatter == nil {
		out.FrontMatter = map[string]any{}
	}
	for _, f := range v.Files {
		out.Files = append(out.Files, skillFile{Path: f.Path, Bytes: f.Bytes, SHA256: f.SHA256})
	}
	return out
}

func fromSkillFiles(in []skillFileBody) []skills.File {
	out := make([]skills.File, 0, len(in))
	for _, f := range in {
		out = append(out, skills.File{Path: f.Path, Content: f.Content})
	}
	return out
}

// toDiffHunks is a diff's hunks as the API shows them, for workflow versions and skill files alike.
func toDiffHunks(in []textdiff.Hunk) []diffHunk {
	out := make([]diffHunk, 0, len(in))
	for _, h := range in {
		hunk := diffHunk{FromLine: h.FromLine, ToLine: h.ToLine, Lines: make([]diffLine, 0, len(h.Lines))}
		for _, l := range h.Lines {
			hunk.Lines = append(hunk.Lines, diffLine{Op: diffOp(l.Op), Text: l.Text})
		}
		out = append(out, hunk)
	}
	return out
}

func (s *Server) skillsReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Skills == nil {
		respondError(w, r, errSkillsUnavailable)
		return false
	}
	return true
}

// skillInPath reads the skill id in the path, answering for the caller when it is malformed or skills are not set up.
func (s *Server) skillInPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, perr := pathUUID(r, "skillID", "skill")
	if perr != nil {
		respondError(w, r, perr)
		return uuid.Nil, false
	}
	return id, s.skillsReady(w, r)
}

// versionInPath reads the version number in the path.
func versionInPath(w http.ResponseWriter, r *http.Request) (int, bool) {
	number, err := strconv.Atoi(chi.URLParam(r, "versionNumber"))
	if err != nil || number < 1 {
		respondError(w, r, ErrBadRequest("That is not a version number. Versions count from 1."))
		return 0, false
	}
	return number, true
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	if !s.skillsReady(w, r) {
		return
	}
	q := r.URL.Query()
	page, err := s.Skills.List(r.Context(), skills.Filter{Archived: q.Get("archived") == "true", Text: q.Get("q"),
		Limit:  queryInt(r, "limit", config.SkillPageSize, 1, config.SkillMaxPage),
		Offset: queryInt(r, "offset", 0, 0, config.AccessMaxOffset)})
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := skillList{Skills: make([]skillSummary, 0, len(page.Skills)), Total: page.Total, Limit: page.Limit, Offset: page.Offset}
	for _, k := range page.Skills {
		out.Skills = append(out.Skills, toSkillSummary(k))
	}
	respondJSON(w, r, http.StatusOK, out)
}

// respondSkill answers a skill with its current version.
func (s *Server) respondSkill(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	k, v, err := s.Skills.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, skillError(err))
		return
	}
	respondJSON(w, r, status, skillResponse{Skill: toSkillSummary(k), Version: toSkillVersionResponse(v)})
}

func (s *Server) handleCreateSkill(w http.ResponseWriter, r *http.Request) {
	var req createSkillRequest
	if err := decodeJSONWithin(w, r, &req, config.MaxSkillBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.skillsReady(w, r) {
		return
	}
	id, err := s.Skills.Create(r.Context(), fromSkillFiles(req.Files), req.Comment)
	if err != nil {
		respondError(w, r, skillError(err))
		return
	}
	s.respondSkill(w, r, id, http.StatusCreated)
}

func (s *Server) handleValidateSkill(w http.ResponseWriter, r *http.Request) {
	var req validateSkillRequest
	if err := decodeJSONWithin(w, r, &req, config.MaxSkillBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.skillsReady(w, r) {
		return
	}
	problems := s.Skills.Validate(fromSkillFiles(req.Files))
	respondJSON(w, r, http.StatusOK, validateSkillResponse{Valid: len(problems) == 0, Problems: toSkillProblems(problems)})
}

func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.skillInPath(w, r); ok {
		s.respondSkill(w, r, id, http.StatusOK)
	}
}

func (s *Server) handleSaveSkill(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "skillID", "skill")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req saveSkillRequest
	if err := decodeJSONWithin(w, r, &req, config.MaxSkillBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.skillsReady(w, r) {
		return
	}
	if err := s.Skills.Save(r.Context(), id, req.BaseVersion, fromSkillFiles(req.Files), req.Comment); err != nil {
		respondError(w, r, skillError(err))
		return
	}
	s.respondSkill(w, r, id, http.StatusOK)
}

func (s *Server) handleArchiveSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := s.skillInPath(w, r)
	if !ok {
		return
	}
	if err := s.Skills.Archive(r.Context(), id); err != nil {
		respondError(w, r, skillError(err))
		return
	}
	respondNoContent(w)
}

func (s *Server) handleRestoreSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := s.skillInPath(w, r)
	if !ok {
		return
	}
	if err := s.Skills.Restore(r.Context(), id); err != nil {
		respondError(w, r, skillError(err))
		return
	}
	s.respondSkill(w, r, id, http.StatusOK)
}

func (s *Server) handleListSkillVersions(w http.ResponseWriter, r *http.Request) {
	id, ok := s.skillInPath(w, r)
	if !ok {
		return
	}
	versions, err := s.Skills.Versions(r.Context(), id)
	if err != nil {
		respondError(w, r, skillError(err))
		return
	}
	out := make([]skillVersionSummary, 0, len(versions))
	for _, v := range versions {
		out = append(out, toSkillVersionSummary(v))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"versions": out})
}

func (s *Server) handleGetSkillVersion(w http.ResponseWriter, r *http.Request) {
	number, ok := versionInPath(w, r)
	if !ok {
		return
	}
	id, ok := s.skillInPath(w, r)
	if !ok {
		return
	}
	v, err := s.Skills.Version(r.Context(), id, number)
	if err != nil {
		respondError(w, r, skillError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, toSkillVersionResponse(v))
}

func (s *Server) handleGetSkillFile(w http.ResponseWriter, r *http.Request) {
	number, ok := versionInPath(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, r, ErrValidation(map[string]string{"path": "Name the file, such as SKILL.md or references/guide.md."}))
		return
	}
	id, ok := s.skillInPath(w, r)
	if !ok {
		return
	}
	f, err := s.Skills.File(r.Context(), id, number, path)
	if errors.Is(err, skills.ErrNotFound) {
		err = ErrNotFound("This version of the skill has no such file, or the skill is not shared with you. Check the path.")
	}
	if err != nil {
		respondError(w, r, skillError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, skillFileResponse{Path: f.Path, Content: f.Content, Bytes: f.Bytes, SHA256: f.SHA256})
}

func (s *Server) handleDiffSkillVersions(w http.ResponseWriter, r *http.Request) {
	from, errFrom := strconv.Atoi(r.URL.Query().Get("from"))
	to, errTo := strconv.Atoi(r.URL.Query().Get("to"))
	if errFrom != nil || errTo != nil || from < 1 || to < 1 {
		respondError(w, r, ErrValidation(map[string]string{"from": "Give the older version's number.", "to": "Give the newer version's number."}))
		return
	}
	id, ok := s.skillInPath(w, r)
	if !ok {
		return
	}
	d, err := s.Skills.Diff(r.Context(), id, from, to)
	if err != nil {
		respondError(w, r, skillError(err))
		return
	}
	out := skillDiffResponse{From: d.From, To: d.To, Files: make([]skillFileDiff, 0, len(d.Files))}
	for _, f := range d.Files {
		out.Files = append(out.Files, skillFileDiff{Path: f.Path, Status: skillFileStatus(f.Status), Hunks: toDiffHunks(f.Hunks)})
	}
	respondJSON(w, r, http.StatusOK, out)
}
