//go:build integration

package test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
	"github.com/cloudster1/rotor/backend/internal/sandbox/docker"
)

// What the suite needs to start sandboxes as the api does, set by make test-integration.
const (
	envSandboxNetwork = "ROTOR_TEST_SANDBOX_NETWORK"
	envSandboxImage   = "ROTOR_TEST_SANDBOX_IMAGE"
	sandboxWait       = 90 * time.Second
	python            = "/opt/venv/bin/python"
)

// probe exits 0 when the sandbox reaches the api and the tool double, cannot reach the internet, cannot write its
// root and can write /tmp; otherwise 10 plus a bit for each thing that is wrong.
const probe = `
import socket, sys, urllib.request
def reaches(url):
    try:
        return urllib.request.urlopen(url, timeout=5).status < 500
    except Exception:
        return False
def internet():
    try:
        socket.create_connection(("1.1.1.1", 443), timeout=3).close()
        return True
    except OSError:
        return False
def writes(path):
    try:
        with open(path, "w") as f:
            f.write("x")
        return True
    except OSError:
        return False
wrong = (not reaches("http://api:8080/healthz")) * 1 + internet() * 2 + writes("/rotor-root") * 4 \
    + (not writes("/tmp/rotor-tmp")) * 8 + (not reaches("http://tool-stub:8080/t/probe/api/v1/meta")) * 16
sys.exit(0 if wrong == 0 else 10 + wrong)
`

func dockerBackend(t *testing.T) (*docker.Backend, string) {
	t.Helper()
	network, image := os.Getenv(envSandboxNetwork), os.Getenv(envSandboxImage)
	if network == "" || image == "" {
		t.Skipf("%s and %s are not set; run the suite with make test-integration", envSandboxNetwork, envSandboxImage)
	}
	if _, err := os.Stat(config.DefaultDockerSocket); err != nil {
		t.Skipf("no Docker socket at %s", config.DefaultDockerSocket)
	}
	return docker.New(config.DefaultDockerSocket, network), image
}

func waitExited(t *testing.T, b sandbox.Backend, id sandbox.ID) sandbox.Status {
	t.Helper()
	deadline := time.Now().Add(sandboxWait)
	for time.Now().Before(deadline) {
		status, err := b.Status(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == sandbox.StateExited {
			return status
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("sandbox %s did not finish within %s", id, sandboxWait)
	return sandbox.Status{}
}

func TestASandboxReachesTheAPIAndTheToolsAndNothingElse(t *testing.T) {
	b, image := dockerBackend(t)
	ctx := context.Background()
	stack, run := "test-"+uuid.NewString()[:8], uuid.New()
	id, err := b.Spawn(ctx, sandbox.Spec{RunID: run, Image: image, Labels: map[string]string{sandbox.LabelStack: stack},
		Limits:     sandbox.Limits{MemoryBytes: config.DefaultSandboxMemory, MilliCPUs: config.DefaultSandboxMilliCPUs, PIDs: config.DefaultSandboxPIDs},
		Entrypoint: []string{python, "-c", probe}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Destroy(context.Background(), id) })

	listed, err := b.List(ctx, map[string]string{sandbox.LabelRun: run.String()})
	if err != nil || len(listed) != 1 || listed[0].ID != id || listed[0].Labels[sandbox.LabelStack] != stack {
		t.Fatalf("listing by the run's label = %+v, %v", listed, err)
	}
	if status := waitExited(t, b, id); status.ExitCode != 0 {
		t.Fatalf("the probe exited %d: 1 api unreachable, 2 internet reachable, 4 root writable, 8 /tmp not writable, 16 tools unreachable (minus 10)", status.ExitCode)
	}
	if err := b.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Status(ctx, id); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("a destroyed sandbox's status = %v, want ErrNotFound", err)
	}
	if err := b.Destroy(ctx, id); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("destroying it again = %v, want ErrNotFound", err)
	}
}

func TestASweepDestroysTheStacksSandboxesThatNobodyKeeps(t *testing.T) {
	b, image := dockerBackend(t)
	ctx := context.Background()
	stack := "test-" + uuid.NewString()[:8]
	labels := map[string]string{sandbox.LabelStack: stack}
	sleeper := func() (sandbox.ID, uuid.UUID) {
		run := uuid.New()
		id, err := b.Spawn(ctx, sandbox.Spec{RunID: run, Image: image, Labels: labels,
			Limits:     sandbox.Limits{MemoryBytes: config.DefaultSandboxMemory, MilliCPUs: config.DefaultSandboxMilliCPUs, PIDs: config.DefaultSandboxPIDs},
			Entrypoint: []string{python, "-c", "import time; time.sleep(300)"}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = b.Destroy(context.Background(), id) })
		return id, run
	}
	kept, keptRun := sleeper()
	sleeper()

	destroyed, err := sandbox.Sweep(ctx, b, labels, func(s sandbox.Sandbox) bool { return s.Labels[sandbox.LabelRun] == keptRun.String() })
	if err != nil || destroyed != 1 {
		t.Fatalf("the sweep destroyed %d, %v; want the one nobody keeps", destroyed, err)
	}
	left, err := b.List(ctx, labels)
	if err != nil || len(left) != 1 || left[0].ID != kept {
		t.Fatalf("after the sweep the stack has %+v, %v", left, err)
	}
	if destroyed, err := sandbox.Sweep(ctx, b, labels, nil); err != nil || destroyed != 1 {
		t.Errorf("sweeping everything destroyed %d, %v", destroyed, err)
	}
}
