// Package openshell starts runs in NVIDIA OpenShell sandboxes. It is the trial backend of #118, reverted once the
// trial's findings are recorded in docs/decisions.md; #119 builds the real one.
package openshell

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"strconv"
	"strings"

	v1 "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// runnerCommand is the runner image's entrypoint, which OpenShell starts as the agent's child process.
var runnerCommand = []string{"/opt/venv/bin/python", "-m", "rotor_runner"}

// runnerBinary is the interpreter's real path, which OpenShell's per-binary network rules match.
const runnerBinary = "/opt/python/cpython-3.13.12-linux-x86_64-gnu/bin/python3.13"

// clusterServices is kind's Service range; OpenShell refuses a private address no endpoint allows.
var clusterServices = []string{"10.96.0.0/12"}

// Config is where the gateway is and how sandboxes are placed.
type Config struct {
	Addr         string
	Workspace    string
	RuntimeClass string
}

// Backend drives an OpenShell gateway.
type Backend struct {
	client *v1.Client
	cfg    Config
}

// New connects to the gateway in plaintext with no user credential, as only the trial does.
func New(cfg Config) (*Backend, error) {
	c, err := v1.NewClient(v1.Config{Address: cfg.Addr, Auth: v1.NoAuth()})
	if err != nil {
		return nil, err
	}
	if cfg.Workspace == "" {
		cfg.Workspace = "default"
	}
	return &Backend{client: c, cfg: cfg}, nil
}

// policy allows the runner's interpreter to reach the control plane's runner port and OpenBao, and nothing else.
func policy(env map[string]string) (*v1.SandboxPolicy, error) {
	host, port, err := net.SplitHostPort(env["ROTOR_CONTROL_PLANE_ADDR"])
	if err != nil {
		return nil, fmt.Errorf("the control plane's address: %w", err)
	}
	runnerPort, _ := strconv.Atoi(port)
	bao, err := url.Parse(env["ROTOR_OPENBAO_ADDR"])
	if err != nil {
		return nil, fmt.Errorf("OpenBao's address: %w", err)
	}
	baoPort, _ := strconv.Atoi(bao.Port())
	binaries := []v1.PolicyNetworkBinary{{Path: runnerBinary}}
	return &v1.SandboxPolicy{
		Version: 1,
		// The runner image keeps its interpreter and venv under /opt, outside OpenShell's baseline paths.
		Filesystem: &v1.FilesystemPolicy{IncludeWorkdir: true, ReadOnly: []string{"/opt"}, ReadWrite: []string{"/tmp"}},
		NetworkPolicies: map[string]v1.NetworkPolicyRule{
			"rotor-runner": {Name: "rotor-runner", Binaries: binaries, Endpoints: []v1.PolicyNetworkEndpoint{
				{Host: host, Port: uint32(runnerPort), TLS: v1.NetworkTLSModeSkip, AllowedIPs: clusterServices},
				// Hostless, matched by port and address alone, for gRPC's IPv4-mapped connect.
				{Port: uint32(runnerPort), TLS: v1.NetworkTLSModeSkip, AllowedIPs: clusterServices},
			}},
			"openbao": {Name: "openbao", Binaries: binaries, Endpoints: []v1.PolicyNetworkEndpoint{
				{Host: bao.Hostname(), Port: uint32(baoPort), Protocol: "rest", Enforcement: v1.NetworkEnforcementModeEnforce,
					Access: v1.NetworkAccessPresetFull, AllowedIPs: clusterServices},
			}},
		},
	}, nil
}

// Spawn creates a sandbox running the runner with the run's environment and policy, and waits until it is ready.
func (b *Backend) Spawn(ctx context.Context, spec sandbox.Spec) (sandbox.ID, error) {
	env := maps.Clone(spec.Env)
	maps.Copy(env, spec.SecretEnv)
	env["ROTOR_TRIAL_DUMPABLE"] = "1"
	// gRPC's dual-stack socket dials the placeholder address as ::ffff:198.18.x, which OpenShell 0.1.5 does not map
	// back to the host its policy names; through the supervisor's CONNECT proxy the host name travels instead.
	env["grpc_proxy"] = "http://127.0.0.1:3128"
	// OpenShell refuses an environment value with a line break, such as the runner's CA in PEM.
	for k, v := range env {
		if strings.ContainsAny(v, "\r\n") {
			env[k] = base64.StdEncoding.EncodeToString([]byte(v))
		}
	}
	pol, err := policy(spec.Env)
	if err != nil {
		return "", err
	}
	command := runnerCommand
	if len(spec.Entrypoint) > 0 {
		command = append(append([]string{}, spec.Entrypoint...), spec.Command...)
	}
	suffix := make([]byte, config.SandboxNameSuffixBytes)
	_, _ = rand.Read(suffix)
	name := "r" + spec.RunID.String()[:config.SandboxNameRunChars] + "-" + hex.EncodeToString(suffix)
	labels := maps.Clone(spec.Labels)
	labels[sandbox.LabelSandbox] = "true"
	labels[sandbox.LabelRun] = spec.RunID.String()
	created, err := b.client.Sandboxes().Create(ctx, b.cfg.Workspace, name, &v1.SandboxSpec{
		Environment:   env,
		Command:       command,
		Policy:        pol,
		RestartPolicy: v1.SandboxRestartNever,
		Template:      &v1.SandboxTemplate{Image: spec.Image, RuntimeClassName: b.cfg.RuntimeClass},
	}, v1.WithLabels(labels))
	if err != nil {
		return "", fmt.Errorf("creating the OpenShell sandbox: %w", err)
	}
	return sandbox.ID(created.Name), nil
}

// Destroy deletes the sandbox; one already gone is fine.
func (b *Backend) Destroy(ctx context.Context, id sandbox.ID) error {
	if _, err := b.client.Sandboxes().Delete(ctx, b.cfg.Workspace, string(id)); err != nil && !v1.IsNotFound(err) {
		return err
	}
	return nil
}

// Status maps OpenShell's phase onto Rotor's states.
func (b *Backend) Status(ctx context.Context, id sandbox.ID) (sandbox.Status, error) {
	s, err := b.client.Sandboxes().Get(ctx, b.cfg.Workspace, string(id))
	if v1.IsNotFound(err) {
		return sandbox.Status{}, sandbox.ErrNotFound
	}
	if err != nil {
		return sandbox.Status{}, err
	}
	return status(s), nil
}

func status(s *v1.Sandbox) sandbox.Status {
	switch s.Status.Phase {
	case v1.SandboxProvisioning, v1.SandboxStarting:
		return sandbox.Status{State: sandbox.StateStarting}
	case v1.SandboxReady:
		return sandbox.Status{State: sandbox.StateRunning}
	case v1.SandboxCompleted, v1.SandboxStopped, v1.SandboxError, v1.SandboxStopping, v1.SandboxDeleting:
		code := -1
		if s.Status.ExitCode != nil {
			code = int(*s.Status.ExitCode)
		}
		return sandbox.Status{State: sandbox.StateExited, ExitCode: code}
	}
	return sandbox.Status{State: sandbox.StateUnknown}
}

// List answers the sandboxes carrying every label asked for.
func (b *Backend) List(ctx context.Context, labels map[string]string) ([]sandbox.Sandbox, error) {
	all, err := b.client.Sandboxes().ListAll(ctx, b.cfg.Workspace)
	if err != nil {
		return nil, err
	}
	var out []sandbox.Sandbox
	for _, s := range all {
		slog.Info("trial: openshell sandbox", "name", s.Name, "labels", s.Labels, "phase", s.Status.Phase, "want", labels)
		match := true
		for k, v := range labels {
			if s.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			out = append(out, sandbox.Sandbox{ID: sandbox.ID(s.Name), Labels: s.Labels, Status: status(s)})
		}
	}
	return out, nil
}
