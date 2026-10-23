package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/workspaces"
)

// Workspaces is what administrators let workspaces use (#136); *workspaces.Service is the real one.
type Workspaces interface {
	Get(ctx context.Context) (workspaces.Setting, error)
	Set(ctx context.Context, in workspaces.Setting) (workspaces.Setting, error)
}

var errWorkspacesUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "workspaces_unavailable",
	Message: "Workspaces are not set up on this installation. An operator connects Rotor to its database."}

func (s *Server) workspacesReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Workspaces == nil {
		respondError(w, r, errWorkspacesUnavailable)
		return false
	}
	return true
}

func (s *Server) handleGetWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !s.workspacesReady(w, r) {
		return
	}
	setting, err := s.Workspaces.Get(r.Context())
	answerWorkspaces(w, r, setting, err)
}

func (s *Server) handleSetWorkspaces(w http.ResponseWriter, r *http.Request) {
	var req workspaceSetting
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.workspacesReady(w, r) {
		return
	}
	in := workspaces.Setting{PackageMirror: req.PackageMirror, MaxStepTime: req.MaxStepTime, MaxVolume: req.MaxVolume}
	for _, i := range req.Images {
		in.Images = append(in.Images, workspaces.Image{Name: i.Name, Reference: i.Reference})
	}
	setting, err := s.Workspaces.Set(r.Context(), in)
	var invalid *workspaces.Invalid
	switch {
	case errors.As(err, &invalid):
		err = ErrValidation(invalid.Fields)
	case errors.Is(err, workspaces.ErrNotAdministrator):
		err = ErrForbidden("Only an administrator changes what workspaces may use, in person.")
	}
	answerWorkspaces(w, r, setting, err)
}

func answerWorkspaces(w http.ResponseWriter, r *http.Request, setting workspaces.Setting, err error) {
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := workspaceSetting{Images: []workspaceImage{}, PackageMirror: setting.PackageMirror, MaxStepTime: setting.MaxStepTime,
		MaxVolume: setting.MaxVolume}
	for _, i := range setting.Images {
		out.Images = append(out.Images, workspaceImage{Name: i.Name, Reference: i.Reference})
	}
	respondJSON(w, r, http.StatusOK, out)
}
