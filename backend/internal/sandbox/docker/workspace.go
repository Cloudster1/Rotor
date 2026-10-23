package docker

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// labelWorkspaceOf names the sandbox a toolchain container or a work tree belongs to, so Destroy finds them.
const labelWorkspaceOf = "rotor.dev/workspace-of"

// mount is a mount of the Engine API: a volume, or an image's files (Docker 28 and later).
type mount struct {
	Type          string         `json:"Type"`
	Source        string         `json:"Source"`
	Target        string         `json:"Target"`
	ReadOnly      bool           `json:"ReadOnly,omitempty"`
	VolumeOptions *volumeOptions `json:"VolumeOptions,omitempty"`
}

// volumeOptions keeps Docker from copying a toolchain image's directory onto the work tree, owner and mode with it,
// which takes the tree from the sandbox's user.
type volumeOptions struct {
	NoCopy bool `json:"NoCopy"`
}

// treeMount is the work tree, shared by the runner and every toolchain.
func treeMount(tree string) mount {
	return mount{Type: "volume", Source: tree, Target: config.WorkspaceTreePath, VolumeOptions: &volumeOptions{NoCopy: true}}
}

type volumeRequest struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	DriverOpts map[string]string `json:"DriverOpts"`
	Labels     map[string]string `json:"Labels"`
}

// spawnWorkspace starts the runner with the work tree and a git directory of its own, then each toolchain in the
// runner's network namespace, so loopback reaches the agents and the egress is the runner's (#136).
func (b *Backend) spawnWorkspace(ctx context.Context, spec sandbox.Spec, req createRequest) (sandbox.ID, error) {
	ws := spec.Workspace
	name, err := sandboxName(spec)
	if err != nil {
		return "", err
	}
	images := []string{ws.AgentImage}
	for _, tc := range ws.Toolchains {
		images = append(images, tc.Image)
	}
	for _, image := range images {
		if err := b.ensureImage(ctx, image); err != nil {
			return "", fmt.Errorf("make the image %s ready for run %s: %w", image, spec.RunID, err)
		}
	}
	tree := name + "-tree"
	owner := fmt.Sprintf("uid=%d,gid=%d", config.SandboxUID, config.SandboxUID)
	if err := b.call(ctx, http.MethodPost, "/volumes/create", volumeRequest{Name: tree, Driver: "local",
		DriverOpts: map[string]string{"type": "tmpfs", "device": "tmpfs", "o": fmt.Sprintf("size=%d,%s,mode=0700", ws.TreeBytes, owner)},
		Labels:     map[string]string{labelWorkspaceOf: name, sandbox.LabelStack: req.Labels[sandbox.LabelStack]}}, nil, http.StatusCreated); err != nil {
		return "", fmt.Errorf("make the work tree of run %s: %w", spec.RunID, err)
	}
	req.Labels[sandbox.LabelWorkspace] = "1"
	req.HostConfig.Tmpfs = maps.Clone(req.HostConfig.Tmpfs)
	req.HostConfig.Tmpfs[config.WorkspaceGitPath] = fmt.Sprintf("rw,noexec,nosuid,size=%d,%s,mode=0700", config.WorkspaceGitBytes, owner)
	req.HostConfig.Mounts = []mount{treeMount(tree)}
	id, err := b.start(ctx, name, req)
	if err != nil {
		_ = b.call(context.WithoutCancel(ctx), http.MethodDelete, "/volumes/"+url.PathEscape(tree), nil, nil, http.StatusNoContent)
		return "", fmt.Errorf("start a sandbox for run %s: %w", spec.RunID, err)
	}
	for i, tc := range ws.Toolchains {
		if _, err := b.start(ctx, fmt.Sprintf("%s-toolchain-%d", name, i), toolchainRequest(spec, id, name, tree, tc)); err != nil {
			_ = b.destroyWorkspace(context.WithoutCancel(ctx), id, name)
			return "", fmt.Errorf("start the toolchain %s of run %s: %w", tc.Name, spec.RunID, err)
		}
	}
	return id, nil
}

// toolchainRequest is a toolchain container as hardened as the runner, started by the agent its image mount brings.
func toolchainRequest(spec sandbox.Spec, runner sandbox.ID, name, tree string, tc sandbox.Toolchain) createRequest {
	ws := spec.Workspace
	env := sandbox.ToolchainEnv(ws, tc)
	env[sandbox.EnvWorkspaceSecret] = spec.SecretEnv[sandbox.EnvWorkspaceSecret]
	vars := make([]string, 0, len(env))
	for k, v := range env {
		vars = append(vars, k+"="+v)
	}
	uid := fmt.Sprintf("%d", config.SandboxUID)
	return createRequest{Image: tc.Image, Env: vars, User: uid + ":" + uid, WorkingDir: config.WorkspaceTreePath,
		Entrypoint: []string{config.WorkspaceAgentBinary, "serve"},
		Labels:     map[string]string{labelWorkspaceOf: name, sandbox.LabelStack: spec.Labels[sandbox.LabelStack]},
		HostConfig: hostConfig{
			ReadonlyRootfs: true,
			Tmpfs: map[string]string{config.WorkspaceCachePath: fmt.Sprintf("rw,exec,nosuid,size=%d,uid=%s,gid=%s,mode=0700",
				config.WorkspaceCacheBytes, uid, uid)},
			CapDrop:     []string{"ALL"},
			SecurityOpt: []string{"no-new-privileges"},
			Memory:      ws.Limits.MemoryBytes,
			MemorySwap:  ws.Limits.MemoryBytes,
			NanoCPUs:    ws.Limits.MilliCPUs * config.NanoCPUsPerMilli,
			PidsLimit:   config.WorkspacePIDs,
			NetworkMode: "container:" + string(runner),
			Mounts: []mount{
				{Type: "image", Source: ws.AgentImage, Target: config.WorkspaceAgentPath, ReadOnly: true},
				treeMount(tree),
			},
		}}
}

// destroyWorkspace removes the toolchains first, which live in the runner's network namespace, then the runner and
// the work tree.
func (b *Backend) destroyWorkspace(ctx context.Context, id sandbox.ID, name string) error {
	filters, _ := json.Marshal(map[string][]string{"label": {labelWorkspaceOf + "=" + name}})
	var listed []struct {
		ID string `json:"Id"`
	}
	errs := []error{b.call(ctx, http.MethodGet, "/containers/json?all=true&filters="+url.QueryEscape(string(filters)), nil, &listed, http.StatusOK)}
	for _, c := range listed {
		if err := b.remove(ctx, c.ID); !errors.Is(err, sandbox.ErrNotFound) {
			errs = append(errs, err)
		}
	}
	runner := b.remove(ctx, string(id))
	if err := b.call(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(name+"-tree"), nil, nil, http.StatusNoContent); !errors.Is(err, sandbox.ErrNotFound) {
		errs = append(errs, err)
	}
	return errors.Join(append(errs, runner)...)
}

// ensureImage pulls an image the daemon does not hold; the pull answers a stream whose last word may be an error.
func (b *Backend) ensureImage(ctx context.Context, image string) error {
	// References are checked when an administrator allows them, and hold nothing a path must escape.
	err := b.call(ctx, http.MethodGet, "/images/"+image+"/json", nil, nil, http.StatusOK)
	if !errors.Is(err, sandbox.ErrNotFound) {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/images/create?fromImage="+url.QueryEscape(image), nil)
	if err != nil {
		return err
	}
	resp, err := b.pull.Do(req)
	if err != nil {
		return fmt.Errorf("the Docker daemon does not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the Docker daemon could not pull %s: it answered %d", image, resp.StatusCode)
	}
	lines := bufio.NewScanner(resp.Body)
	for lines.Scan() {
		var progress struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(lines.Bytes(), &progress) == nil && progress.Error != "" {
			return fmt.Errorf("the Docker daemon could not pull %s: %s", image, strings.TrimSpace(progress.Error))
		}
	}
	return lines.Err()
}

// sandboxName names a workspace sandbox as the kube backend names its pods, so its parts can be found by name.
func sandboxName(spec sandbox.Spec) (string, error) {
	suffix := make([]byte, config.SandboxNameSuffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	return "rotor-run-" + spec.RunID.String()[:config.SandboxNameRunChars] + "-" + hex.EncodeToString(suffix), nil
}
