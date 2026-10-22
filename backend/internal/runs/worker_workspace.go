package runs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
	"github.com/cloudster1/rotor/backend/internal/workspaces"
)

// ReasonImageNotAllowed ends a run whose workspace image an administrator stopped allowing after it started.
const ReasonImageNotAllowed = "image_not_allowed"

// EnvWorkspace tells the runner which agent serves which image, and how long a step may take.
const EnvWorkspace = "ROTOR_WORKSPACE"

// WorkspaceSettings is what workspaces may use, as Rotor reads it; *workspaces.Service is the real one.
type WorkspaceSettings interface {
	Current(ctx context.Context) (workspaces.Setting, error)
}

// WorkspaceLaunch is how the worker starts a run's toolchains (#136): the agent's image and each toolchain's size.
type WorkspaceLaunch struct {
	Settings   WorkspaceSettings
	AgentImage string
	Limits     sandbox.Limits
}

// imageGone is a workspace image no administrator allows any more, found as the run's sandbox starts.
type imageGone struct{ name string }

func (e *imageGone) Error() string {
	return fmt.Sprintf("No administrator allows the toolchain image %s any more, so the run cannot start its workspace. An administrator allows it again, or the workflow's owner saves it with an allowed image.", e.name)
}

// runnerWorkspace is what the runner reads from EnvWorkspace.
type runnerWorkspace struct {
	Agents      map[string]int `json:"agents"`
	StepSeconds int            `json:"stepSeconds"`
}

// workspaceOf is the workspace a run's sandbox gets, with what its runner is told, or nil for a run without
// workspace steps: the images its pinned version names, as administrators allow them now.
func (w *Worker) workspaceOf(ctx context.Context, runID uuid.UUID) (*sandbox.Workspace, map[string]string, error) {
	var document []byte
	err := w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT v.spec FROM run r
			JOIN workflow_version v ON v.workflow_id = r.workflow_id AND v.number = r.version WHERE r.id = $1`, runID).Scan(&document)
	})
	if err != nil {
		return nil, nil, err
	}
	var pinned spec.Spec
	if err := json.Unmarshal(document, &pinned); err != nil {
		return nil, nil, fmt.Errorf("read run %s's pinned workflow: %w", runID, err)
	}
	names := pinned.WorkspaceImages()
	if len(names) == 0 {
		return nil, nil, nil
	}
	if w.launch.Workspace.Settings == nil {
		return nil, nil, &imageGone{name: names[0]}
	}
	setting, err := w.launch.Workspace.Settings.Current(ctx)
	if err != nil {
		return nil, nil, err
	}
	ws := &sandbox.Workspace{AgentImage: w.launch.Workspace.AgentImage, TreeBytes: setting.VolumeBytes(),
		Env: setting.MirrorEnv(), Limits: w.launch.Workspace.Limits}
	told := runnerWorkspace{Agents: map[string]int{}, StepSeconds: int(setting.StepTime() / time.Second)}
	for i, name := range names {
		reference, ok := setting.Reference(name)
		if !ok {
			return nil, nil, &imageGone{name: name}
		}
		port := config.WorkspaceAgentPortBase + i
		ws.Toolchains = append(ws.Toolchains, sandbox.Toolchain{Name: name, Image: reference, Port: port})
		told.Agents[name] = port
	}
	encoded, err := json.Marshal(told)
	if err != nil {
		return nil, nil, err
	}
	secret := make([]byte, config.WorkspaceSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, nil, err
	}
	return ws, map[string]string{EnvWorkspace: string(encoded), sandbox.EnvWorkspaceSecret: hex.EncodeToString(secret)}, nil
}
