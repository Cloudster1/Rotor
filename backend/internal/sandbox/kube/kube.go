// Package kube runs each sandbox as one hardened pod in the sandbox namespace (#26), under gVisor unless explicitly
// overridden, with its bootstrap token in a Secret the pod owns.
package kube

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// namePrefix starts every sandbox pod's name.
const namePrefix = "rotor-run-"

// Config places sandboxes: the namespace, the RuntimeClass (empty for none) and the token-less ServiceAccount.
type Config struct {
	Namespace      string
	RuntimeClass   string
	ServiceAccount string
	// Kubeconfig reaches a cluster from outside, as tests do; empty uses the pod's own in-cluster config.
	Kubeconfig string
}

// Backend starts sandboxes as pods.
type Backend struct {
	client kubernetes.Interface
	cfg    Config
}

// New connects to the cluster; it calls nothing yet, so the worker starts while the API server is busy.
func New(cfg Config) (*Backend, error) {
	var rc *rest.Config
	var err error
	if cfg.Kubeconfig != "" {
		rc, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	} else {
		rc, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("kube: reach the cluster: %w", err)
	}
	rc.Timeout = config.SandboxCallTimeout
	client, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("kube: %w", err)
	}
	return NewForClient(client, cfg), nil
}

// NewForClient is the backend over a client already made, such as a test's.
func NewForClient(client kubernetes.Interface, cfg Config) *Backend {
	return &Backend{client: client, cfg: cfg}
}

func podName(spec sandbox.Spec) (string, error) {
	suffix := make([]byte, config.SandboxNameSuffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	return namePrefix + spec.RunID.String()[:config.SandboxNameRunChars] + "-" + hex.EncodeToString(suffix), nil
}

// Spawn makes the sandbox's Secret, then its pod, then hands the Secret to the pod, so deleting the pod deletes it.
func (b *Backend) Spawn(ctx context.Context, spec sandbox.Spec) (sandbox.ID, error) {
	name, err := podName(spec)
	if err != nil {
		return "", err
	}
	secrets := b.client.CoreV1().Secrets(b.cfg.Namespace)
	secret, err := secrets.Create(ctx, secretFor(name, b.cfg.Namespace, spec), metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create the secret of run %s's sandbox: %w", spec.RunID, err)
	}
	pod, err := b.client.CoreV1().Pods(b.cfg.Namespace).Create(ctx, podFor(name, b.cfg, spec), metav1.CreateOptions{})
	if err != nil {
		_ = secrets.Delete(context.WithoutCancel(ctx), name, metav1.DeleteOptions{})
		return "", fmt.Errorf("create run %s's sandbox: %w", spec.RunID, err)
	}
	yes := true
	secret.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: pod.UID, BlockOwnerDeletion: &yes}}
	if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		_ = b.Destroy(context.WithoutCancel(ctx), sandbox.ID(name))
		_ = secrets.Delete(context.WithoutCancel(ctx), name, metav1.DeleteOptions{})
		return "", fmt.Errorf("hand run %s's secret to its sandbox: %w", spec.RunID, err)
	}
	return sandbox.ID(name), nil
}

// Destroy deletes the pod at once, and its Secret with it; one already gone is ErrNotFound.
func (b *Backend) Destroy(ctx context.Context, id sandbox.ID) error {
	now, background := int64(0), metav1.DeletePropagationBackground
	err := b.client.CoreV1().Pods(b.cfg.Namespace).Delete(ctx, string(id), metav1.DeleteOptions{GracePeriodSeconds: &now, PropagationPolicy: &background})
	if apierrors.IsNotFound(err) {
		return sandbox.ErrNotFound
	}
	return err
}

// Status reads the pod's phase and, once it ended, its runner's exit code.
func (b *Backend) Status(ctx context.Context, id sandbox.ID) (sandbox.Status, error) {
	pod, err := b.client.CoreV1().Pods(b.cfg.Namespace).Get(ctx, string(id), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return sandbox.Status{}, sandbox.ErrNotFound
	}
	if err != nil {
		return sandbox.Status{}, err
	}
	return statusOf(pod), nil
}

// List is the sandboxes with every label given.
func (b *Backend) List(ctx context.Context, match map[string]string) ([]sandbox.Sandbox, error) {
	selector := labels.Set{sandbox.LabelSandbox: "1"}
	for k, v := range match {
		selector[k] = v
	}
	pods, err := b.client.CoreV1().Pods(b.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	out := make([]sandbox.Sandbox, 0, len(pods.Items))
	for i := range pods.Items {
		p := &pods.Items[i]
		out = append(out, sandbox.Sandbox{ID: sandbox.ID(p.Name), Labels: p.Labels, Status: statusOf(p)})
	}
	return out, nil
}

// statusOf maps a pod's phase to a sandbox's state; a pod being deleted counts as exited.
func statusOf(pod *corev1.Pod) sandbox.Status {
	if pod.DeletionTimestamp != nil {
		return sandbox.Status{State: sandbox.StateExited}
	}
	switch pod.Status.Phase {
	case corev1.PodPending:
		return sandbox.Status{State: sandbox.StateStarting}
	case corev1.PodRunning:
		return sandbox.Status{State: sandbox.StateRunning}
	case corev1.PodSucceeded, corev1.PodFailed:
		status := sandbox.Status{State: sandbox.StateExited}
		for _, c := range pod.Status.ContainerStatuses {
			if c.Name == containerName && c.State.Terminated != nil {
				status.ExitCode = int(c.State.Terminated.ExitCode)
			}
		}
		return status
	}
	return sandbox.Status{State: sandbox.StateUnknown}
}
