// Package sandbox starts and ends the containers runs execute in, behind one interface: docker in development,
// kube (a hardened pod under gVisor) everywhere else.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
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
	// Env is what the runner needs to dial out, such as the control plane's address and its CA; SecretEnv is the
	// part a backend keeps as a secret where it can, such as the bootstrap token.
	Env       map[string]string
	SecretEnv map[string]string
	Limits    Limits
	Labels    map[string]string
	// Deadline is how long the sandbox may live at most, as a backstop to the worker; zero leaves it to the worker.
	Deadline time.Duration
	// Entrypoint and Command replace the image's own; tests use them, runs never do.
	Entrypoint []string
	Command    []string
	// Workspace puts toolchain containers beside the runner, for a run with workspace steps (#136); nil for others.
	Workspace *Workspace
}

// EnvWorkspaceSecret is the secret the runner shows the exec agents, in SecretEnv, which the toolchains get too.
const EnvWorkspaceSecret = "ROTOR_WORKSPACE_SECRET"

// LabelWorkspace marks a sandbox with toolchain containers, whose egress adds the package mirror.
const LabelWorkspace = "rotor.dev/workspace"

// Workspace is one toolchain container per image beside the runner, each started by the exec agent an init
// container or an image mount hands it, all sharing the work tree, which only the runner pairs with a git directory.
type Workspace struct {
	AgentImage string
	Toolchains []Toolchain
	// TreeBytes bounds the work tree, a memory volume.
	TreeBytes int64
	// Env is what every toolchain container gets, such as the package mirror.
	Env    map[string]string
	Limits Limits
}

// Toolchain is one toolchain container: the image's name as workflows give it, its reference, and the loopback
// port its agent listens on.
type Toolchain struct {
	Name  string
	Image string
	Port  int
}

// ToolchainEnv is what a toolchain container runs with beyond ws.Env: where its agent listens, and caches that a
// reset empties instead of the image's own directories, which are read-only.
func ToolchainEnv(ws *Workspace, tc Toolchain) map[string]string {
	cache := config.WorkspaceCachePath
	env := map[string]string{
		"ROTOR_AGENT_LISTEN": fmt.Sprintf("127.0.0.1:%d", tc.Port),
		"ROTOR_AGENT_CACHE":  cache,
		"HOME":               cache,
		"TMPDIR":             cache,
		"XDG_CACHE_HOME":     cache + "/xdg",
		"GOCACHE":            cache + "/go-build",
		"GOMODCACHE":         cache + "/go-mod",
		"GOTOOLCHAIN":        "local",
		"npm_config_cache":   cache + "/npm",
	}
	for k, v := range ws.Env {
		env[k] = v
	}
	return env
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
