package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// zipType is the media type of a skill's import and export (#220).
const zipType = "application/zip"

var errSkillZipTooLarge = &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
	Message: fmt.Sprintf("The zip is larger than %d MiB, which is all an import takes. Leave out files the skill does not need.",
		config.MaxSkillImportBytes>>20)}

func (s *Server) handleImportSkill(w http.ResponseWriter, r *http.Request) {
	if !s.skillsReady(w, r) {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.MaxSkillImportBytes))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		respondError(w, r, errSkillZipTooLarge)
		return
	case err != nil:
		respondError(w, r, ErrBadRequest("The zip could not be read from the request. Send it again."))
		return
	}
	id, created, err := s.Skills.Import(r.Context(), data, r.URL.Query().Get("comment"))
	if err != nil {
		respondError(w, r, skillError(err))
		return
	}
	k, v, err := s.Skills.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, skillError(err))
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondJSON(w, r, status, importedSkill{skillResponse: skillResponse{Skill: toSkillSummary(k), Version: toSkillVersionResponse(v)}, Created: created})
}

func (s *Server) handleExportSkillVersion(w http.ResponseWriter, r *http.Request) {
	number, ok := versionInPath(w, r)
	if !ok {
		return
	}
	id, ok := s.skillInPath(w, r)
	if !ok {
		return
	}
	name, data, err := s.Skills.Export(r.Context(), id, number)
	if err != nil {
		respondError(w, r, skillError(err))
		return
	}
	w.Header().Set("Content-Type", zipType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-v%d.zip"`, name, number))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
