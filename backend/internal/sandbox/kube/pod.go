package kube

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// containerName is the runner's container in a sandbox pod; tmpVolume the one place it may write.
const (
	containerName = "runner"
	tmpVolume     = "tmp"
	tmpPath       = "/tmp"
)

// labelsOf are every label a sandbox carries, so the worker finds its own and nobody else's.
func labelsOf(spec sandbox.Spec) map[string]string {
	labels := map[string]string{sandbox.LabelSandbox: "1", sandbox.LabelRun: spec.RunID.String()}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	return labels
}

// secretFor is the Secret holding what the sandbox must not show in its pod spec, such as its bootstrap token.
func secretFor(name, namespace string, spec sandbox.Spec) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labelsOf(spec)},
		Type:       corev1.SecretTypeOpaque,
		StringData: spec.SecretEnv,
	}
}

// podFor is a sandbox as a pod: under the configured RuntimeClass, non-root, read-only, with no capabilities, no
// privilege escalation, the runtime's seccomp profile, no ServiceAccount token and a deadline as a backstop.
func podFor(name string, cfg Config, spec sandbox.Spec) *corev1.Pod {
	yes, no := true, false
	uid := int64(config.SandboxUID)
	grace := int64(config.SandboxTerminationGrace.Seconds())
	var env []corev1.EnvVar
	for _, k := range sortedKeys(spec.Env) {
		env = append(env, corev1.EnvVar{Name: k, Value: spec.Env[k]})
	}
	for _, k := range sortedKeys(spec.SecretEnv) {
		env = append(env, corev1.EnvVar{Name: k, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: k}}})
	}
	quantities := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(spec.Limits.MilliCPUs, resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(spec.Limits.MemoryBytes, resource.BinarySI),
	}
	tmpSize := resource.NewQuantity(config.SandboxTmpBytes, resource.BinarySI)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cfg.Namespace, Labels: labelsOf(spec)},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			ServiceAccountName:            cfg.ServiceAccount,
			AutomountServiceAccountToken:  &no,
			EnableServiceLinks:            &no,
			TerminationGracePeriodSeconds: &grace,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &yes, RunAsUser: &uid, RunAsGroup: &uid,
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name: containerName, Image: spec.Image, Command: spec.Entrypoint, Args: spec.Command, Env: env,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Resources:       corev1.ResourceRequirements{Limits: quantities, Requests: quantities},
				SecurityContext: &corev1.SecurityContext{
					RunAsNonRoot: &yes, RunAsUser: &uid, RunAsGroup: &uid,
					ReadOnlyRootFilesystem:   &yes,
					AllowPrivilegeEscalation: &no,
					Privileged:               &no,
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				},
				VolumeMounts: []corev1.VolumeMount{{Name: tmpVolume, MountPath: tmpPath}},
			}},
			Volumes: []corev1.Volume{{Name: tmpVolume, VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: tmpSize}}}},
		},
	}
	if cfg.RuntimeClass != "" {
		class := cfg.RuntimeClass
		pod.Spec.RuntimeClassName = &class
	}
	if spec.Deadline > 0 {
		seconds := int64(spec.Deadline.Seconds())
		pod.Spec.ActiveDeadlineSeconds = &seconds
	}
	return pod
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
