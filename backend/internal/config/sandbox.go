package config

import (
	"fmt"
	"time"
)

// Sandbox settings and limits (#21); docs/api-contract-m2.md describes the interface.
const (
	// SandboxDocker is the development backend, which drives the host's Docker daemon.
	SandboxDocker        = "docker"
	DefaultSandboxImage  = "rotor-runner:dev"
	DefaultDockerSocket  = "/var/run/docker.sock"
	DefaultSandboxMemory = 1536 << 20
	// DefaultSandboxMilliCPUs is one CPU; DefaultSandboxPIDs bounds the runner, its threads and its code nodes.
	DefaultSandboxMilliCPUs = 1000
	DefaultSandboxPIDs      = 256
	// SandboxTmpfs is the one place a sandbox may write, small and never executable.
	SandboxTmpfs     = "rw,noexec,nosuid,size=64m"
	NanoCPUsPerMilli = 1_000_000
	// SandboxCallTimeout bounds one call to the Docker daemon; MaxDaemonAnswerBytes what Rotor reads of its answer.
	SandboxCallTimeout   = 30 * time.Second
	MaxDaemonAnswerBytes = 1 << 20
)

// Sandbox is where runs execute.
type Sandbox struct {
	// Backend is empty when this api starts no sandboxes, or docker.
	Backend string
	Image   string
	Socket  string
	// Network is the internal network sandboxes join, reaching the api and the tools alone.
	Network string
	// Stack labels this installation's sandboxes, so a sweep never touches another's.
	Stack string
}

func loadSandbox(l *loader) Sandbox {
	return Sandbox{
		Backend: l.str("ROTOR_SANDBOX_BACKEND", ""),
		Image:   l.str("ROTOR_SANDBOX_IMAGE", DefaultSandboxImage),
		Socket:  l.str("ROTOR_SANDBOX_DOCKER_SOCKET", DefaultDockerSocket),
		Network: l.str("ROTOR_SANDBOX_NETWORK", ""),
		Stack:   l.str("ROTOR_SANDBOX_STACK", ""),
	}
}

func checkSandbox(l *loader, c Config) {
	switch c.Sandbox.Backend {
	case "":
	case SandboxDocker:
		if c.Env != EnvDevelopment {
			l.problem("ROTOR_SANDBOX_BACKEND is docker, which hands the api the host's Docker daemon; use it in development only.")
		}
		if c.Sandbox.Network == "" || c.Sandbox.Stack == "" {
			l.problem("ROTOR_SANDBOX_BACKEND is docker, so set ROTOR_SANDBOX_NETWORK to the internal network sandboxes join and ROTOR_SANDBOX_STACK to the label of this installation's sandboxes.")
		}
	default:
		l.problem(fmt.Sprintf("ROTOR_SANDBOX_BACKEND is %q; set it to docker, or leave it empty for an api that starts no sandboxes.", c.Sandbox.Backend))
	}
}
