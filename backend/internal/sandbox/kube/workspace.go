package kube

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// The volumes of a workspace pod (#136): the agent's binary, the work tree both sides see, the runner's own git
// directory, and each toolchain's caches.
const (
	agentVolume     = "agent"
	treeVolume      = "work-tree"
	gitVolume       = "git"
	cacheVolume     = "cache-%d"
	agentContainer  = "agent"
	toolchainPrefix = "toolchain-%d"
	agentMilliCPUs  = 100
)

// addWorkspace puts the workspace's toolchain containers beside the runner: an init container copies the agent in,
// the runner mounts the work tree and its git directory, each toolchain the work tree and caches of its own.
func addWorkspace(pod *corev1.Pod, name string, ws *sandbox.Workspace, hardened func() *corev1.SecurityContext) {
	pod.Labels[sandbox.LabelWorkspace] = "1"
	memory := func(bytes int64) corev1.VolumeSource {
		return corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory,
			SizeLimit: resource.NewQuantity(bytes, resource.BinarySI)}}
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes,
		corev1.Volume{Name: agentVolume, VolumeSource: memory(config.WorkspaceAgentBytes)},
		corev1.Volume{Name: treeVolume, VolumeSource: memory(ws.TreeBytes)},
		corev1.Volume{Name: gitVolume, VolumeSource: memory(config.WorkspaceGitBytes)})
	agentQuantities := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(agentMilliCPUs, resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(config.WorkspaceAgentBytes, resource.BinarySI),
	}
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name: agentContainer, Image: ws.AgentImage, Args: []string{"install", config.WorkspaceAgentBinary},
		ImagePullPolicy: corev1.PullIfNotPresent, SecurityContext: hardened(),
		Resources:    corev1.ResourceRequirements{Limits: agentQuantities, Requests: agentQuantities},
		VolumeMounts: []corev1.VolumeMount{{Name: agentVolume, MountPath: config.WorkspaceAgentPath}},
	})
	runner := &pod.Spec.Containers[0]
	runner.VolumeMounts = append(runner.VolumeMounts,
		corev1.VolumeMount{Name: treeVolume, MountPath: config.WorkspaceTreePath},
		corev1.VolumeMount{Name: gitVolume, MountPath: config.WorkspaceGitPath})
	quantities := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(ws.Limits.MilliCPUs, resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(ws.Limits.MemoryBytes, resource.BinarySI),
	}
	for i, tc := range ws.Toolchains {
		cache := fmt.Sprintf(cacheVolume, i)
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{Name: cache, VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: resource.NewQuantity(config.WorkspaceCacheBytes, resource.BinarySI)}}})
		env := sandbox.ToolchainEnv(ws, tc)
		var vars []corev1.EnvVar
		for _, k := range sortedKeys(env) {
			vars = append(vars, corev1.EnvVar{Name: k, Value: env[k]})
		}
		// The agent reads the secret from the pod's Secret and drops it, so no command it runs sees it.
		vars = append(vars, corev1.EnvVar{Name: sandbox.EnvWorkspaceSecret, ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: sandbox.EnvWorkspaceSecret}}})
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
			Name: fmt.Sprintf(toolchainPrefix, i), Image: tc.Image, Command: []string{config.WorkspaceAgentBinary, "serve"},
			WorkingDir: config.WorkspaceTreePath, Env: vars, ImagePullPolicy: corev1.PullIfNotPresent,
			Resources:       corev1.ResourceRequirements{Limits: quantities, Requests: quantities},
			SecurityContext: hardened(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: agentVolume, MountPath: config.WorkspaceAgentPath, ReadOnly: true},
				{Name: treeVolume, MountPath: config.WorkspaceTreePath},
				{Name: cache, MountPath: config.WorkspaceCachePath},
			},
		})
	}
}
