package config

import "time"

// Workspaces (#136), as docs/api-contract-m13.md agrees: a toolchain container beside the runner per image, an exec
// agent in it, and the run's branch checked out into a work tree they share.
const (
	// MaxWorkspaceSteps bounds the workspace steps of one run, as the runner counts them.
	MaxWorkspaceSteps = 50
	// MaxCommandOutput is what an agent reads of a command: its first and last half when there is more.
	MaxCommandOutput = 64 << 10
	// DefaultCommandTimeout is how long a command runs unless the call says, at most the step's time left.
	DefaultCommandTimeout = 15 * time.Minute
	// DefaultWorkspaceStepTime and DefaultWorkspaceVolume are the settings' defaults: a step's time and the work tree.
	DefaultWorkspaceStepTime = time.Hour
	DefaultWorkspaceVolume   = 1 << 30
	// MaxWorkspaceStepTime and MaxWorkspaceVolume bound what an administrator may set.
	MaxWorkspaceStepTime = 6 * time.Hour
	MaxWorkspaceVolume   = 8 << 30
	// MinWorkspaceStepTime and MinWorkspaceVolume keep a setting from making every step fail.
	MinWorkspaceStepTime = time.Minute
	MinWorkspaceVolume   = 64 << 20
	// MaxWorkspaceImages bounds the toolchain images an administrator allows; MaxImageReference one's reference.
	MaxWorkspaceImages = 20
	MaxImageReference  = 300
	// MaxPackageMirror bounds the package mirror's address.
	MaxPackageMirror = 500
	// DefaultWorkspaceAgentImage is the exec agent's image in development; the chart names Rotor's published one.
	DefaultWorkspaceAgentImage = "rotor-agent:dev"
	// DefaultWorkspaceMemory and DefaultWorkspaceMilliCPUs size a toolchain container: a cold Go build of Rotor's
	// backend peaked at 1.2 GB under gVisor (#132).
	DefaultWorkspaceMemory    = 3 << 30
	DefaultWorkspaceMilliCPUs = 2000
	// WorkspacePIDs bounds a toolchain container's processes, a build's compilers included.
	WorkspacePIDs = 1024
	// WorkspaceCacheBytes is a toolchain container's caches on disk: HOME, TMPDIR and the build and module caches.
	WorkspaceCacheBytes = 4 << 30
	// WorkspaceGitBytes bounds the runner's git directory, a depth 1 fetch of the run's branch.
	WorkspaceGitBytes = 1 << 30
	// WorkspaceAgentBytes is the memory volume the init container copies the exec agent into.
	WorkspaceAgentBytes = 32 << 20
	// WorkspaceAgentPortBase is the first loopback port an exec agent listens on; each image takes the next.
	WorkspaceAgentPortBase = 7700
	// WorkspaceSecretBytes is the randomness of the secret the runner shows an exec agent.
	WorkspaceSecretBytes = 32
	// WorkspacePullTimeout bounds pulling a toolchain image in the docker backend.
	WorkspacePullTimeout = 10 * time.Minute
)

// Where a workspace's volumes are mounted, in the runner and the toolchain containers alike.
const (
	WorkspaceTreePath  = "/workspace"
	WorkspaceGitPath   = "/private"
	WorkspaceAgentPath = "/rotor/bin"
	WorkspaceCachePath = "/cache"
	// WorkspaceAgentBinary is the exec agent inside WorkspaceAgentPath, which a toolchain container starts.
	WorkspaceAgentBinary = WorkspaceAgentPath + "/agent"
)
