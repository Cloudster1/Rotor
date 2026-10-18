//go:build kube

// Package kube tests the Kubernetes sandbox backend against a real cluster: make kind-up, then make test-kube.
package kube

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
	"github.com/cloudster1/rotor/backend/internal/sandbox/kube"
)

const (
	envKubeconfig = "ROTOR_TEST_KUBECONFIG"
	envImage      = "ROTOR_TEST_KUBE_IMAGE"
	// The namespace, accounts and RBAC deploy/kube/sandboxes.yaml applies, as the chart renders them.
	sandboxNamespace = "rotor-sandboxes"
	sandboxAccount   = "rotor-sandbox"
	workerAccount    = "system:serviceaccount:rotor:rotor-worker"
	stack            = "kube-test"
	// podWait bounds how long a pod may take to pull nothing (the image is loaded) and run the self-check.
	podWait = 2 * time.Minute
	poll    = time.Second
)

// clients answers the cluster as its administrator and as the worker's ServiceAccount, by impersonation.
func clients(t *testing.T) (admin, worker kubernetes.Interface) {
	t.Helper()
	path := os.Getenv(envKubeconfig)
	if path == "" {
		t.Skipf("%s is not set; run make kind-up, then make test-kube", envKubeconfig)
	}
	rc, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		t.Fatal(err)
	}
	admin = kubernetes.NewForConfigOrDie(rc)
	as := rest.CopyConfig(rc)
	as.Impersonate = rest.ImpersonationConfig{UserName: workerAccount}
	return admin, kubernetes.NewForConfigOrDie(as)
}

func waitFor(t *testing.T, what string, done func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(podWait)
	for time.Now().Before(deadline) {
		ok, err := done()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if ok {
			return
		}
		time.Sleep(poll)
	}
	t.Fatalf("%s did not happen within %s", what, podWait)
}

func TestASandboxRunsAsAHardenedPodWithNoTokenAndLeavesNothingBehind(t *testing.T) {
	admin, worker := clients(t)
	// Acting as the worker proves its RBAC is enough; runc, since kind has no gVisor.
	b := kube.NewForClient(worker, kube.Config{Namespace: sandboxNamespace, ServiceAccount: sandboxAccount})
	ctx := context.Background()
	id, err := b.Spawn(ctx, sandbox.Spec{RunID: uuid.New(), Image: os.Getenv(envImage), Command: []string{"--self-check"},
		Env: map[string]string{"ROTOR_RUNNER_CA": "pem"}, SecretEnv: map[string]string{"ROTOR_RUNNER_BOOTSTRAP": "not-a-token"},
		Labels:   map[string]string{sandbox.LabelStack: stack},
		Limits:   sandbox.Limits{MemoryBytes: config.DefaultSandboxMemory, MilliCPUs: config.DefaultSandboxMilliCPUs},
		Deadline: time.Hour})
	if err != nil {
		t.Fatalf("the worker could not start a sandbox (the restricted Pod Security standard refuses a pod that is not hardened): %v", err)
	}
	t.Cleanup(func() { _ = b.Destroy(context.Background(), id) })

	waitFor(t, "the self-check finishing", func() (bool, error) {
		status, err := b.Status(ctx, id)
		if err != nil || status.State != sandbox.StateExited {
			return false, err
		}
		if status.ExitCode != 0 {
			return false, errors.New("the runner's self-check failed in its pod")
		}
		return true, nil
	})
	pod, err := admin.CoreV1().Pods(sandboxNamespace).Get(ctx, string(id), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range pod.Spec.Volumes {
		if v.Projected != nil || v.Secret != nil {
			t.Errorf("the pod mounts %s, a token or a secret as a volume", v.Name)
		}
	}
	for _, m := range pod.Spec.Containers[0].VolumeMounts {
		if m.MountPath != "/tmp" {
			t.Errorf("the runner mounts %s", m.MountPath)
		}
	}
	if pod.Spec.ActiveDeadlineSeconds == nil || *pod.Spec.ActiveDeadlineSeconds != int64(time.Hour.Seconds()) {
		t.Errorf("the pod's deadline = %v", pod.Spec.ActiveDeadlineSeconds)
	}
	listed, err := b.List(ctx, map[string]string{sandbox.LabelStack: stack})
	if err != nil || len(listed) != 1 || listed[0].ID != id {
		t.Errorf("the stack's sandboxes = %+v (%v)", listed, err)
	}

	if err := b.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pod and its Secret going", func() (bool, error) {
		_, podErr := admin.CoreV1().Pods(sandboxNamespace).Get(ctx, string(id), metav1.GetOptions{})
		_, secretErr := admin.CoreV1().Secrets(sandboxNamespace).Get(ctx, string(id), metav1.GetOptions{})
		return apierrors.IsNotFound(podErr) && apierrors.IsNotFound(secretErr), nil
	})
}

func TestTheWorkersRightsEndAtTheSandboxNamespace(t *testing.T) {
	_, worker := clients(t)
	ctx := context.Background()
	elsewhere := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "escape"}, Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "c", Image: os.Getenv(envImage)}}}}
	for what, err := range map[string]error{
		"create a pod in default": func() error {
			_, err := worker.CoreV1().Pods("default").Create(ctx, elsewhere, metav1.CreateOptions{})
			return err
		}(),
		"list pods in kube-system": func() error {
			_, err := worker.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{})
			return err
		}(),
		"read secrets in kube-system": func() error {
			_, err := worker.CoreV1().Secrets("kube-system").List(ctx, metav1.ListOptions{})
			return err
		}(),
		"list namespaces": func() error {
			_, err := worker.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
			return err
		}(),
		"make a ServiceAccount in its own namespace": func() error {
			_, err := worker.CoreV1().ServiceAccounts(sandboxNamespace).Create(ctx,
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "more-rights"}}, metav1.CreateOptions{})
			return err
		}(),
	} {
		if !apierrors.IsForbidden(err) {
			t.Errorf("the worker may %s: %v", what, err)
		}
	}
}

func TestTheSandboxNamespaceRefusesAPodThatIsNotHardened(t *testing.T) {
	admin, _ := clients(t)
	root := int64(0)
	_, err := admin.CoreV1().Pods(sandboxNamespace).Create(context.Background(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "as-root"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: os.Getenv(envImage),
			SecurityContext: &corev1.SecurityContext{RunAsUser: &root}}}}}, metav1.CreateOptions{})
	if !apierrors.IsForbidden(err) {
		t.Errorf("the sandbox namespace accepted a pod running as root: %v", err)
		_ = admin.CoreV1().Pods(sandboxNamespace).Delete(context.Background(), "as-root", metav1.DeleteOptions{})
	}
}
