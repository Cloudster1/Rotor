// Package sandbox starts and ends the containers runs execute in, behind one interface; docker is the
// development backend, Kubernetes with gVisor follows in M4.
package sandbox

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Labels every sandbox carries, so a control plane finds its own and nobody else's.
const (
	LabelSandbox = "rotor.dev/sandbox"
	LabelStack   = "rotor.dev/stack"
	LabelRun     = "rotor.dev/run"
)

// ErrNotFound is a sandbox that does not exist, or no longer does.
var ErrNotFound = errors.New("no such sandbox")

// ID names a sandbox within its backend.
type ID string

// Limits bound what a sandbox may use.
type Limits struct {
	MemoryBytes int64
	// MilliCPUs is thousandths of a CPU: 1000 is one.
	MilliCPUs int64
	PIDs      int64
}

// Spec is a sandbox to start for a run.
type Spec struct {
	RunID uuid.UUID
	Image string
	// Env is what the runner needs to dial out: the control plane's address, its CA and a bootstrap token.
	Env    map[string]string
	Limits Limits
	Labels map[string]string
	// Entrypoint and Command replace the image's own; tests use them, runs never do.
	Entrypoint []string
	Command    []string
}

// State is where a sandbox is in its life.
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateExited   State = "exited"
	StateUnknown  State = "unknown"
)

// Status is a sandbox's state, and its exit code once exited.
type Status struct {
	State    State
	ExitCode int
}

// Sandbox is one sandbox as a listing shows it.
type Sandbox struct {
	ID     ID
	Labels map[string]string
	Status Status
}

// Backend starts and ends sandboxes.
type Backend interface {
	Spawn(ctx context.Context, spec Spec) (ID, error)
	Destroy(ctx context.Context, id ID) error
	Status(ctx context.Context, id ID) (Status, error)
	List(ctx context.Context, labels map[string]string) ([]Sandbox, error)
}

// Sweep destroys the sandboxes with the labels given that keep does not claim, such as those of runs that ended
// while the control plane was down; it answers how many it destroyed.
func Sweep(ctx context.Context, b Backend, labels map[string]string, keep func(Sandbox) bool) (int, error) {
	found, err := b.List(ctx, labels)
	if err != nil {
		return 0, err
	}
	destroyed := 0
	for _, s := range found {
		if keep != nil && keep(s) {
			continue
		}
		if err := b.Destroy(ctx, s.ID); err != nil && !errors.Is(err, ErrNotFound) {
			return destroyed, err
		}
		destroyed++
	}
	return destroyed, nil
}
