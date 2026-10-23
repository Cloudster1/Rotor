package config

import (
	"fmt"
	"time"
)

// Sandbox settings and limits (#21, #26); docs/api-contract-m2.md describes the interface.
const (
	// SandboxDocker is the development backend, which drives the host's Docker daemon.
	SandboxDocker = "docker"
	// SandboxKubernetes runs each sandbox as a hardened pod, under gVisor unless explicitly overridden (#26).
	SandboxKubernetes    = "kubernetes"
	DefaultSandboxImage  = "rotor-runner:dev"
	DefaultDockerSocket  = "/var/run/docker.sock"
	DefaultSandboxMemory = 1536 << 20
	// DefaultSandboxMilliCPUs is one CPU; DefaultSandboxPIDs bounds the runner, its threads and its code nodes.
	DefaultSandboxMilliCPUs = 1000
	DefaultSandboxPIDs      = 256
	// SandboxTmpfs is the one place a sandbox may write, small and never executable.
	SandboxTmpfs     = "rw,noexec,nosuid,size=64m"
	SandboxTmpBytes  = 64 << 20
	NanoCPUsPerMilli = 1_000_000
	// SandboxCallTimeout bounds one call to the Docker daemon; MaxDaemonAnswerBytes what Rotor reads of its answer.
	SandboxCallTimeout   = 30 * time.Second
	MaxDaemonAnswerBytes = 1 << 20

	// DefaultRuntimeClass is the gVisor RuntimeClass a sandbox pod runs under unless an operator names another.
	DefaultRuntimeClass = "gvisor"
	// DefaultSandboxNamespace and DefaultSandboxServiceAccount are where sandbox pods run and the token-less
	// account they run as.
	DefaultSandboxNamespace      = "rotor-sandboxes"
	DefaultSandboxServiceAccount = "rotor-sandbox"
	// SandboxUID is the runner image's nonroot user, which a pod runs as.
	SandboxUID = 65532
	// SandboxTerminationGrace is short: a sandbox keeps nothing worth a graceful stop, its checkpoints being stored.
	SandboxTerminationGrace = 5 * time.Second
	// SandboxNameRunChars of the run's id and SandboxNameSuffixBytes of randomness name a sandbox pod.
	SandboxNameRunChars    = 8
	SandboxNameSuffixBytes = 3
	// SandboxDeadlineGrace is how long past its run's own deadline a pod may live before Kubernetes ends it.
	SandboxDeadlineGrace = 10 * time.Minute
)

// Sandbox is where runs execute.
type Sandbox struct {
	// Backend is empty when this process starts no sandboxes, docker or kubernetes.
	Backend string
	Image   string
	Socket  string
	// Network is the internal network Docker sandboxes join, reaching the api and the tools alone.
	Network string
	// Stack labels this installation's sandboxes, so a sweep never touches another's.
	Stack string
	// Namespace, RuntimeClass and ServiceAccount place a Kubernetes sandbox; RuntimeClass is gVisor unless set,
	// and set empty runs without one. Kubeconfig is for tests outside a cluster.
	Namespace      string
	RuntimeClass   string
	ServiceAccount string
	Kubeconfig     string
	// AllowRunc lets a Kubernetes sandbox run without gVisor, for a test cluster that has none; never in production.
	AllowRunc bool
	// AgentImage is the exec agent's image, which a workspace's toolchains start (#136); WorkspaceMemoryMiB and
	// WorkspaceMilliCPUs size each toolchain container.
	AgentImage         string
	WorkspaceMemoryMiB int
	WorkspaceMilliCPUs int
}

func loadSandbox(l *loader) Sandbox {
	return Sandbox{
		Backend:            l.str("ROTOR_SANDBOX_BACKEND", ""),
		Image:              l.str("ROTOR_SANDBOX_IMAGE", DefaultSandboxImage),
		Socket:             l.str("ROTOR_SANDBOX_DOCKER_SOCKET", DefaultDockerSocket),
		Network:            l.str("ROTOR_SANDBOX_NETWORK", ""),
		Stack:              l.str("ROTOR_SANDBOX_STACK", ""),
		Namespace:          l.str("ROTOR_SANDBOX_NAMESPACE", DefaultSandboxNamespace),
		RuntimeClass:       l.orEmpty("ROTOR_SANDBOX_RUNTIME_CLASS", DefaultRuntimeClass),
		ServiceAccount:     l.str("ROTOR_SANDBOX_SERVICE_ACCOUNT", DefaultSandboxServiceAccount),
		Kubeconfig:         l.str("ROTOR_SANDBOX_KUBECONFIG", ""),
		AllowRunc:          l.boolean("ROTOR_SANDBOX_ALLOW_RUNC", false),
		AgentImage:         l.str("ROTOR_WORKSPACE_AGENT_IMAGE", DefaultWorkspaceAgentImage),
		WorkspaceMemoryMiB: l.integer("ROTOR_WORKSPACE_MEMORY_MIB", DefaultWorkspaceMemory>>20),
		WorkspaceMilliCPUs: l.integer("ROTOR_WORKSPACE_MILLICPUS", DefaultWorkspaceMilliCPUs),
	}
}

func checkSandbox(l *loader, c Config) {
	switch c.Sandbox.Backend {
	case "":
	case SandboxDocker:
		if c.Env != EnvDevelopment {
			l.problem("ROTOR_SANDBOX_BACKEND is docker, which hands the worker the host's Docker daemon; use it in development only, and kubernetes elsewhere.")
		}
		if c.Sandbox.Network == "" || c.Sandbox.Stack == "" {
			l.problem("ROTOR_SANDBOX_BACKEND is docker, so set ROTOR_SANDBOX_NETWORK to the internal network sandboxes join and ROTOR_SANDBOX_STACK to the label of this installation's sandboxes.")
		}
	case SandboxKubernetes:
		if c.Sandbox.Stack == "" {
			l.problem("ROTOR_SANDBOX_BACKEND is kubernetes, so set ROTOR_SANDBOX_STACK to the label of this installation's sandboxes, such as its release name.")
		}
		if c.Sandbox.RuntimeClass == "" && !c.Sandbox.AllowRunc {
			l.problem("ROTOR_SANDBOX_RUNTIME_CLASS is empty, so sandboxes would run without gVisor. Name the cluster's gVisor RuntimeClass, or set ROTOR_SANDBOX_ALLOW_RUNC=true for a test cluster that has none.")
		}
		if c.Sandbox.AllowRunc && c.Env == EnvProduction {
			l.problem("ROTOR_SANDBOX_ALLOW_RUNC is true in production, which would run hostile code without gVisor. Unset it and name the cluster's gVisor RuntimeClass.")
		}
	default:
		l.problem(fmt.Sprintf("ROTOR_SANDBOX_BACKEND is %q; set it to kubernetes, docker in development, or leave it empty for a process that starts no sandboxes.", c.Sandbox.Backend))
	}
}
