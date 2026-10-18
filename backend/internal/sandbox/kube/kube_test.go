package kube

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

const namespace = "rotor-sandboxes"

func testSpec() sandbox.Spec {
	return sandbox.Spec{RunID: uuid.MustParse("0190a5c4-0000-7000-8000-00000000dd01"), Image: "rotor-runner:test",
		Env:       map[string]string{"ROTOR_CONTROL_PLANE_ADDR": "rotor-api.rotor.svc:9443", "ROTOR_RUNNER_CA": "pem"},
		SecretEnv: map[string]string{"ROTOR_RUNNER_BOOTSTRAP": "bootstrap-token"},
		Labels:    map[string]string{sandbox.LabelStack: "rotor"},
		Limits:    sandbox.Limits{MemoryBytes: config.DefaultSandboxMemory, MilliCPUs: config.DefaultSandboxMilliCPUs},
		Deadline:  15 * time.Minute}
}

func TestASandboxPodIsHardenedAndHoldsNoTokenInItsSpec(t *testing.T) {
	pod := podFor("rotor-run-x", Config{Namespace: namespace, RuntimeClass: "gvisor", ServiceAccount: "rotor-sandbox"}, testSpec())
	spec := pod.Spec
	switch {
	case spec.RuntimeClassName == nil || *spec.RuntimeClassName != "gvisor":
		t.Errorf("runtime class = %v", spec.RuntimeClassName)
	case spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken:
		t.Error("the pod mounts a ServiceAccount token")
	case spec.EnableServiceLinks == nil || *spec.EnableServiceLinks:
		t.Error("the pod gets the namespace's services as environment variables")
	case spec.RestartPolicy != corev1.RestartPolicyNever:
		t.Errorf("restart policy = %s", spec.RestartPolicy)
	case spec.ActiveDeadlineSeconds == nil || *spec.ActiveDeadlineSeconds != int64((15*time.Minute).Seconds()):
		t.Errorf("deadline = %v", spec.ActiveDeadlineSeconds)
	case spec.ServiceAccountName != "rotor-sandbox":
		t.Errorf("service account = %s", spec.ServiceAccountName)
	}
	c := spec.Containers[0].SecurityContext
	if c == nil || !*c.RunAsNonRoot || *c.RunAsUser != config.SandboxUID || !*c.ReadOnlyRootFilesystem || *c.AllowPrivilegeEscalation ||
		*c.Privileged || len(c.Capabilities.Drop) != 1 || c.Capabilities.Drop[0] != "ALL" || c.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("the runner's security context = %+v", c)
	}
	for _, e := range spec.Containers[0].Env {
		if strings.Contains(e.Value, "bootstrap-token") {
			t.Errorf("the bootstrap token is in the pod spec as %s", e.Name)
		}
		if e.Name == "ROTOR_RUNNER_BOOTSTRAP" && (e.ValueFrom == nil || e.ValueFrom.SecretKeyRef.Name != "rotor-run-x") {
			t.Errorf("the bootstrap token does not come from the sandbox's Secret: %+v", e)
		}
	}
	if pod.Labels[sandbox.LabelSandbox] != "1" || pod.Labels[sandbox.LabelStack] != "rotor" || pod.Labels[sandbox.LabelRun] == "" {
		t.Errorf("labels = %v", pod.Labels)
	}
	if runc := podFor("x", Config{Namespace: namespace}, sandbox.Spec{}); runc.Spec.RuntimeClassName != nil || runc.Spec.ActiveDeadlineSeconds != nil {
		t.Error("an empty runtime class or deadline still set one")
	}
}

func TestTheBackendStartsListsAndEndsPodsWithTheirSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	b := NewForClient(client, Config{Namespace: namespace, RuntimeClass: "gvisor", ServiceAccount: "rotor-sandbox"})
	ctx := context.Background()
	id, err := b.Spawn(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, string(id), metav1.GetOptions{})
	if err != nil || secret.StringData["ROTOR_RUNNER_BOOTSTRAP"] != "bootstrap-token" || len(secret.OwnerReferences) != 1 || secret.OwnerReferences[0].Kind != "Pod" {
		t.Errorf("the sandbox's Secret = %+v (%v)", secret, err)
	}
	listed, err := b.List(ctx, map[string]string{sandbox.LabelStack: "rotor"})
	if err != nil || len(listed) != 1 || listed[0].ID != id || listed[0].Status.State != sandbox.StateUnknown {
		t.Errorf("list = %+v (%v)", listed, err)
	}
	if other, _ := b.List(ctx, map[string]string{sandbox.LabelStack: "another"}); len(other) != 0 {
		t.Errorf("another stack's list = %+v", other)
	}
	pod, _ := client.CoreV1().Pods(namespace).Get(ctx, string(id), metav1.GetOptions{})
	pod.Status = corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{Name: containerName,
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 2}}}}}
	if _, err := client.CoreV1().Pods(namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if status, err := b.Status(ctx, id); err != nil || status.State != sandbox.StateExited || status.ExitCode != 2 {
		t.Errorf("status = %+v (%v)", status, err)
	}
	if err := b.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := b.Destroy(ctx, id); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("destroying again = %v", err)
	}
	if _, err := b.Status(ctx, id); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("status of a destroyed sandbox = %v", err)
	}
}
