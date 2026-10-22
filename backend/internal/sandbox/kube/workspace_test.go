package kube

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

func workspaceSpec() sandbox.Spec {
	spec := testSpec()
	spec.SecretEnv[sandbox.EnvWorkspaceSecret] = "agent-secret"
	spec.Workspace = &sandbox.Workspace{AgentImage: "rotor-agent:test", TreeBytes: config.DefaultWorkspaceVolume,
		Env:    map[string]string{"GOPROXY": "https://mirror.example/go"},
		Limits: sandbox.Limits{MemoryBytes: config.DefaultWorkspaceMemory, MilliCPUs: config.DefaultWorkspaceMilliCPUs},
		Toolchains: []sandbox.Toolchain{
			{Name: "golang-1.26", Image: "golang@sha256:aaaa", Port: config.WorkspaceAgentPortBase},
			{Name: "node-24", Image: "node@sha256:bbbb", Port: config.WorkspaceAgentPortBase + 1},
		}}
	return spec
}

func mounts(c corev1.Container) map[string]string {
	out := map[string]string{}
	for _, m := range c.VolumeMounts {
		out[m.MountPath] = m.Name
	}
	return out
}

// A run with workspace steps gets one toolchain container per image beside the runner, started by the agent an init
// container copies in, all as hardened as the runner, and sharing the work tree but never the runner's git directory.
func TestAWorkspacePodHasAHardenedToolchainPerImageBesideTheRunner(t *testing.T) {
	pod := podFor("rotor-run-x", Config{Namespace: namespace, RuntimeClass: "gvisor"}, workspaceSpec())
	if pod.Labels[sandbox.LabelWorkspace] != "1" {
		t.Errorf("the pod's labels %v do not mark it a workspace, so its egress lacks the mirror", pod.Labels)
	}
	if len(pod.Spec.InitContainers) != 1 || pod.Spec.InitContainers[0].Image != "rotor-agent:test" ||
		strings.Join(pod.Spec.InitContainers[0].Args, " ") != "install "+config.WorkspaceAgentBinary {
		t.Fatalf("the init containers = %+v", pod.Spec.InitContainers)
	}
	if len(pod.Spec.Containers) != 3 {
		t.Fatalf("the pod has %d containers, want the runner and two toolchains", len(pod.Spec.Containers))
	}
	runner := mounts(pod.Spec.Containers[0])
	if runner[config.WorkspaceTreePath] != treeVolume || runner[config.WorkspaceGitPath] != gitVolume {
		t.Errorf("the runner mounts %v", runner)
	}
	all := append([]corev1.Container{pod.Spec.InitContainers[0]}, pod.Spec.Containers...)
	for _, c := range all {
		sc := c.SecurityContext
		if sc == nil || !*sc.RunAsNonRoot || !*sc.ReadOnlyRootFilesystem || *sc.AllowPrivilegeEscalation || *sc.Privileged ||
			len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
			t.Errorf("%s is not hardened: %+v", c.Name, sc)
		}
	}
	for i, c := range pod.Spec.Containers[1:] {
		m := mounts(c)
		if m[config.WorkspaceTreePath] != treeVolume || m[config.WorkspaceGitPath] != "" || m[config.WorkspaceCachePath] == "" ||
			m[config.WorkspaceAgentPath] != agentVolume {
			t.Errorf("toolchain %d mounts %v", i, m)
		}
		if strings.Join(c.Command, " ") != config.WorkspaceAgentBinary+" serve" || c.WorkingDir != config.WorkspaceTreePath {
			t.Errorf("toolchain %d starts %v in %s", i, c.Command, c.WorkingDir)
		}
		if c.Resources.Limits.Memory().Value() != config.DefaultWorkspaceMemory {
			t.Errorf("toolchain %d may use %s of memory", i, c.Resources.Limits.Memory())
		}
		env := map[string]corev1.EnvVar{}
		for _, e := range c.Env {
			env[e.Name] = e
			if strings.Contains(e.Value, "agent-secret") || strings.Contains(e.Value, "bootstrap-token") {
				t.Errorf("toolchain %d holds a secret in its spec as %s", i, e.Name)
			}
		}
		if s := env[sandbox.EnvWorkspaceSecret]; s.ValueFrom == nil || s.ValueFrom.SecretKeyRef.Name != "rotor-run-x" {
			t.Errorf("toolchain %d does not take the agent's secret from the sandbox's Secret: %+v", i, s)
		}
		if _, ok := env["ROTOR_RUNNER_BOOTSTRAP"]; ok {
			t.Errorf("toolchain %d is handed the runner's bootstrap token", i)
		}
		if env["GOPROXY"].Value != "https://mirror.example/go" || env["HOME"].Value != config.WorkspaceCachePath ||
			!strings.HasPrefix(env["ROTOR_AGENT_LISTEN"].Value, "127.0.0.1:") {
			t.Errorf("toolchain %d runs with %v", i, env)
		}
	}
	volumes := map[string]corev1.Volume{}
	for _, v := range pod.Spec.Volumes {
		volumes[v.Name] = v
	}
	if tree := volumes[treeVolume].EmptyDir; tree == nil || tree.Medium != corev1.StorageMediumMemory || tree.SizeLimit.Value() != config.DefaultWorkspaceVolume {
		t.Errorf("the work tree is %+v", volumes[treeVolume])
	}
	if cache := volumes["cache-0"].EmptyDir; cache == nil || cache.Medium != "" || cache.SizeLimit.Value() != config.WorkspaceCacheBytes {
		t.Errorf("the caches are %+v", volumes["cache-0"])
	}
}

// The toolchains serve until the pod goes, so a runner that ended ends the sandbox while the pod still runs.
func TestAWorkspaceSandboxEndsWithItsRunner(t *testing.T) {
	pod := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{
		{Name: "toolchain-0", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		{Name: containerName, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
	}}}
	if status := statusOf(pod); status.State != sandbox.StateExited || status.ExitCode != 0 {
		t.Errorf("a running pod whose runner ended = %+v", status)
	}
}

// A run without workspace steps gets the pod it always did.
func TestARunWithoutWorkspacesGetsTodaysPod(t *testing.T) {
	pod := podFor("rotor-run-x", Config{Namespace: namespace}, testSpec())
	if len(pod.Spec.InitContainers) != 0 || len(pod.Spec.Containers) != 1 || pod.Labels[sandbox.LabelWorkspace] != "" {
		t.Errorf("a plain run's pod = %d init containers, %d containers, labels %v", len(pod.Spec.InitContainers), len(pod.Spec.Containers), pod.Labels)
	}
}
