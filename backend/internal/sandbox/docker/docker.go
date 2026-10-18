// Package docker runs sandboxes as containers through the Docker Engine API on its unix socket. It is for
// development only: the socket is the host's, so the api holding it is trusted with the machine.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// apiBase is the Engine API as the socket serves it; the host part is ignored.
const apiBase = "http://docker"

// Backend is a sandbox.Backend on one Docker daemon, attaching every sandbox to one network alone.
type Backend struct {
	http    *http.Client
	network string
}

// New talks to the daemon at socket; network is the internal network sandboxes join, and nothing else.
func New(socket, network string) *Backend {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &Backend{http: &http.Client{Transport: transport, Timeout: config.SandboxCallTimeout}, network: network}
}

type hostConfig struct {
	ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
	Tmpfs          map[string]string `json:"Tmpfs"`
	CapDrop        []string          `json:"CapDrop"`
	SecurityOpt    []string          `json:"SecurityOpt"`
	Memory         int64             `json:"Memory,omitempty"`
	MemorySwap     int64             `json:"MemorySwap,omitempty"`
	NanoCPUs       int64             `json:"NanoCpus,omitempty"`
	PidsLimit      int64             `json:"PidsLimit,omitempty"`
	NetworkMode    string            `json:"NetworkMode"`
}

type createRequest struct {
	Image      string            `json:"Image"`
	Env        []string          `json:"Env"`
	Labels     map[string]string `json:"Labels"`
	Entrypoint []string          `json:"Entrypoint,omitempty"`
	Cmd        []string          `json:"Cmd,omitempty"`
	HostConfig hostConfig        `json:"HostConfig"`
}

// Spawn creates and starts a container that holds no capability, writes only to a small /tmp, and reaches only
// the sandbox network.
func (b *Backend) Spawn(ctx context.Context, spec sandbox.Spec) (sandbox.ID, error) {
	env := make([]string, 0, len(spec.Env)+len(spec.SecretEnv))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	// The daemon has no secrets of its own; a development sandbox gets them as the rest of its environment.
	for k, v := range spec.SecretEnv {
		env = append(env, k+"="+v)
	}
	labels := map[string]string{sandbox.LabelSandbox: "1", sandbox.LabelRun: spec.RunID.String()}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	req := createRequest{Image: spec.Image, Env: env, Labels: labels, Entrypoint: spec.Entrypoint, Cmd: spec.Command,
		HostConfig: hostConfig{
			ReadonlyRootfs: true,
			Tmpfs:          map[string]string{"/tmp": config.SandboxTmpfs},
			CapDrop:        []string{"ALL"},
			SecurityOpt:    []string{"no-new-privileges"},
			Memory:         spec.Limits.MemoryBytes,
			MemorySwap:     spec.Limits.MemoryBytes,
			NanoCPUs:       spec.Limits.MilliCPUs * config.NanoCPUsPerMilli,
			PidsLimit:      spec.Limits.PIDs,
			NetworkMode:    b.network,
		}}
	var created struct {
		ID string `json:"Id"`
	}
	if err := b.call(ctx, http.MethodPost, "/containers/create", req, &created, http.StatusCreated); err != nil {
		return "", fmt.Errorf("create a sandbox for run %s: %w", spec.RunID, err)
	}
	id := sandbox.ID(created.ID)
	if err := b.call(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil, http.StatusNoContent); err != nil {
		_ = b.Destroy(context.WithoutCancel(ctx), id)
		return "", fmt.Errorf("start the sandbox of run %s: %w", spec.RunID, err)
	}
	return id, nil
}

// Destroy stops and removes a container at once; one already gone is ErrNotFound.
func (b *Backend) Destroy(ctx context.Context, id sandbox.ID) error {
	return b.call(ctx, http.MethodDelete, "/containers/"+url.PathEscape(string(id))+"?force=true&v=true", nil, nil, http.StatusNoContent)
}

type inspected struct {
	ID     string `json:"Id"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status   string `json:"Status"`
		ExitCode int    `json:"ExitCode"`
	} `json:"State"`
}

// Status reads a container's state.
func (b *Backend) Status(ctx context.Context, id sandbox.ID) (sandbox.Status, error) {
	var got inspected
	if err := b.call(ctx, http.MethodGet, "/containers/"+url.PathEscape(string(id))+"/json", nil, &got, http.StatusOK); err != nil {
		return sandbox.Status{}, err
	}
	return status(got.State.Status, got.State.ExitCode), nil
}

func status(state string, exitCode int) sandbox.Status {
	switch state {
	case "created", "restarting":
		return sandbox.Status{State: sandbox.StateStarting}
	case "running", "paused":
		return sandbox.Status{State: sandbox.StateRunning}
	case "exited", "dead", "removing":
		return sandbox.Status{State: sandbox.StateExited, ExitCode: exitCode}
	}
	return sandbox.Status{State: sandbox.StateUnknown}
}

// List finds the containers carrying every label given, running or not.
func (b *Backend) List(ctx context.Context, labels map[string]string) ([]sandbox.Sandbox, error) {
	match := []string{sandbox.LabelSandbox + "=1"}
	for k, v := range labels {
		match = append(match, k+"="+v)
	}
	filters, _ := json.Marshal(map[string][]string{"label": match})
	var listed []struct {
		ID     string            `json:"Id"`
		Labels map[string]string `json:"Labels"`
		State  string            `json:"State"`
	}
	if err := b.call(ctx, http.MethodGet, "/containers/json?all=true&filters="+url.QueryEscape(string(filters)), nil, &listed, http.StatusOK); err != nil {
		return nil, err
	}
	out := make([]sandbox.Sandbox, 0, len(listed))
	for _, c := range listed {
		out = append(out, sandbox.Sandbox{ID: sandbox.ID(c.ID), Labels: c.Labels, Status: status(c.State, 0)})
	}
	return out, nil
}

// call sends one request and decodes the answer into out; 404 is ErrNotFound, another unexpected status an error
// carrying the daemon's message.
func (b *Backend) call(ctx context.Context, method, path string, in, out any, want int) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("the Docker daemon does not answer: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, config.MaxDaemonAnswerBytes))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return sandbox.ErrNotFound
	case resp.StatusCode != want && !(want == http.StatusNoContent && resp.StatusCode == http.StatusNotModified):
		var refusal struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &refusal)
		return fmt.Errorf("the Docker daemon answered %d: %s", resp.StatusCode, strings.TrimSpace(refusal.Message))
	case out != nil:
		return json.Unmarshal(raw, out)
	}
	return nil
}
