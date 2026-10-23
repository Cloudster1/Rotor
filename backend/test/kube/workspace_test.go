//go:build kube

package kube

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
	"github.com/cloudster1/rotor/backend/internal/sandbox/kube"
)

// What a workspace pod runs beside the runner (#136): the agent make kind-up loads, a toolchain pinned by digest that
// the node pulls, and gVisor, which make kind-up installs into the node.
const (
	envAgentImage   = "ROTOR_TEST_AGENT_IMAGE"
	toolchainImage  = "busybox:1.37.0@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e"
	gvisorClass     = "gvisor"
	toolchainMemory = 512 << 20
	logBytes        = 16 << 10
)

// A workspace pod under gVisor holds what a step needs, as the runner's --workspace-check proves from inside: the
// copied agent answers it over loopback, the work tree is shared, a reset ends what a step left, and git works.
func TestAWorkspacePodUnderGVisorHoldsWhatItsStepsNeed(t *testing.T) {
	admin, worker := clients(t)
	agent := os.Getenv(envAgentImage)
	if agent == "" {
		t.Skipf("%s is not set; run make kind-up, then make test-kube", envAgentImage)
	}
	b := kube.NewForClient(worker, kube.Config{Namespace: sandboxNamespace, ServiceAccount: sandboxAccount, RuntimeClass: gvisorClass})
	ctx := context.Background()
	id, err := b.Spawn(ctx, sandbox.Spec{RunID: uuid.New(), Image: os.Getenv(envImage), Command: []string{"--workspace-check"},
		Env:       map[string]string{"ROTOR_WORKSPACE": `{"agents":{"busybox-1.37":7700},"stepSeconds":300}`},
		SecretEnv: map[string]string{sandbox.EnvWorkspaceSecret: "workspace-secret-of-the-kube-test-0123456789"},
		Labels:    map[string]string{sandbox.LabelStack: stack + "-workspace"},
		Limits:    sandbox.Limits{MemoryBytes: config.DefaultSandboxMemory, MilliCPUs: config.DefaultSandboxMilliCPUs},
		Deadline:  time.Hour,
		Workspace: &sandbox.Workspace{AgentImage: agent, TreeBytes: config.DefaultWorkspaceVolume, Env: map[string]string{"GOPROXY": "off"},
			Limits:     sandbox.Limits{MemoryBytes: toolchainMemory, MilliCPUs: config.DefaultSandboxMilliCPUs},
			Toolchains: []sandbox.Toolchain{{Name: "busybox-1.37", Image: toolchainImage, Port: config.WorkspaceAgentPortBase}}}})
	if err != nil {
		t.Fatalf("the worker could not start a workspace sandbox (the restricted Pod Security standard refuses one not hardened): %v", err)
	}
	t.Cleanup(func() { _ = b.Destroy(context.Background(), id) })

	waitFor(t, "the workspace check finishing", func() (bool, error) {
		status, err := b.Status(ctx, id)
		if err != nil || status.State != sandbox.StateExited {
			return false, err
		}
		if status.ExitCode != 0 {
			limit := int64(logBytes)
			logs, _ := admin.CoreV1().Pods(sandboxNamespace).GetLogs(string(id), &corev1.PodLogOptions{Container: "runner", LimitBytes: &limit}).DoRaw(ctx)
			return false, errors.New("the workspace check failed in its pod: " + string(logs))
		}
		return true, nil
	})
	pod, err := admin.CoreV1().Pods(sandboxNamespace).Get(ctx, string(id), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != gvisorClass || len(pod.Spec.InitContainers) != 1 || len(pod.Spec.Containers) != 2 {
		t.Errorf("the workspace pod runs %v with %d init containers and %d containers", pod.Spec.RuntimeClassName,
			len(pod.Spec.InitContainers), len(pod.Spec.Containers))
	}
}
