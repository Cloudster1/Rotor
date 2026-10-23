//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// What a workspace sandbox runs beside the runner: the exec agent make builds, and a small toolchain pinned by digest.
const (
	envAgentImage    = "ROTOR_TEST_AGENT_IMAGE"
	toolchainImage   = "busybox:1.37.0@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e"
	toolchainName    = "busybox-1.37"
	workspaceSecret  = "workspace-secret-of-the-test-0123456789abcdef"
	workspaceAnswer  = `{"agents":{"busybox-1.37":7700},"stepSeconds":300}`
	labelWorkspaceOf = "rotor.dev/workspace-of"
)

// workspaceSpec is a sandbox with one toolchain whose runner proves, with --workspace-check, what its steps need.
func workspaceSpec(t *testing.T, image string) sandbox.Spec {
	t.Helper()
	agent := os.Getenv(envAgentImage)
	if agent == "" {
		t.Skipf("%s is not set; run the suite with make test-integration", envAgentImage)
	}
	return sandbox.Spec{RunID: uuid.New(), Image: image, Labels: map[string]string{sandbox.LabelStack: "test-" + uuid.NewString()[:8]},
		Limits:    sandbox.Limits{MemoryBytes: config.DefaultSandboxMemory, MilliCPUs: config.DefaultSandboxMilliCPUs, PIDs: config.DefaultSandboxPIDs},
		Env:       map[string]string{"ROTOR_WORKSPACE": workspaceAnswer},
		SecretEnv: map[string]string{sandbox.EnvWorkspaceSecret: workspaceSecret},
		Command:   []string{"--workspace-check"},
		Workspace: &sandbox.Workspace{AgentImage: agent, TreeBytes: config.DefaultWorkspaceVolume,
			Env:        map[string]string{"GOPROXY": "off"},
			Limits:     sandbox.Limits{MemoryBytes: config.DefaultSandboxMemory, MilliCPUs: config.DefaultSandboxMilliCPUs},
			Toolchains: []sandbox.Toolchain{{Name: toolchainName, Image: toolchainImage, Port: config.WorkspaceAgentPortBase}}}}
}

// engine asks the host's Docker daemon what is left of a sandbox, as the backend's own listing would not show it.
func engine(t *testing.T, path string) []byte {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", config.DefaultDockerSocket)
	}}}
	resp, err := client.Get("http://docker" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body
}

// A workspace sandbox in Docker has the shape of the gVisor pod: the toolchain in the runner's network namespace,
// the agent as an image mount, the work tree shared and the git directory the runner's; and it leaves nothing.
func TestAWorkspaceSandboxHoldsWhatItsStepsNeedAndLeavesNothing(t *testing.T) {
	b, image := dockerBackend(t)
	ctx := context.Background()
	spec := workspaceSpec(t, image)
	id, err := b.Spawn(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Destroy(context.Background(), id) })
	if status := waitExited(t, b, id); status.ExitCode != 0 {
		logs := engine(t, "/containers/"+string(id)+"/logs?stdout=true&stderr=true")
		t.Fatalf("the workspace check exited %d: %s", status.ExitCode, logs)
	}
	var inspected struct {
		Name string `json:"Name"`
	}
	_ = json.Unmarshal(engine(t, "/containers/"+string(id)+"/json"), &inspected)
	if err := b.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Status(ctx, id); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("a destroyed workspace sandbox's status = %v", err)
	}
	filters, _ := json.Marshal(map[string][]string{"label": {labelWorkspaceOf + "=" + inspected.Name[1:]}})
	var left []any
	_ = json.Unmarshal(engine(t, "/containers/json?all=true&filters="+url.QueryEscape(string(filters))), &left)
	var volumes struct {
		Volumes []any `json:"Volumes"`
	}
	_ = json.Unmarshal(engine(t, "/volumes?filters="+url.QueryEscape(string(filters))), &volumes)
	if len(left) != 0 || len(volumes.Volumes) != 0 {
		t.Errorf("destroying the sandbox left %d toolchains and %d work trees", len(left), len(volumes.Volumes))
	}
}
