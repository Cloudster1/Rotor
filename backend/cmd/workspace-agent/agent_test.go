package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

const secret = "sandbox-secret-for-the-test"

func call(t *testing.T, a *agent, path, bearer string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	return rec
}

// execute runs a command as the route does once it took the request, in a directory the test may run in.
func execute(req request) answer {
	req.TimeoutSeconds, req.MaxOutputBytes = 10, config.MaxCommandOutput
	return run(context.Background(), req)
}

// Only the runner, which holds the sandbox's secret, runs anything; a code node on the same loopback does not.
func TestTheAgentRunsNothingWithoutTheSandboxesSecret(t *testing.T) {
	a := newAgent(secret, "")
	for _, bearer := range []string{"", "a-guess"} {
		if rec := call(t, a, "/exec", bearer, request{Argv: []string{"true"}, Dir: config.WorkspaceTreePath}); rec.Code != http.StatusUnauthorized {
			t.Errorf("exec with %q = %d", bearer, rec.Code)
		}
		if rec := call(t, a, "/reset", bearer, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("reset with %q = %d", bearer, rec.Code)
		}
	}
}

// argv runs as given: a shell's metacharacters are an argument like any other, never a pipeline.
func TestACommandIsAnArgvAndNeverAShellLine(t *testing.T) {
	got := execute(request{Argv: []string{"echo", "a; rm -rf / && echo $HOME | cat"}, Dir: "/"})
	if got.ExitCode != 0 || got.Output != "a; rm -rf / && echo $HOME | cat\n" {
		t.Errorf("echo with metacharacters = %+v", got)
	}
	got = execute(request{Argv: []string{"no-such-program-anywhere"}, Dir: "/"})
	if got.ExitCode != -1 || !strings.Contains(got.Error, "could not be started") {
		t.Errorf("a program that does not exist = %+v", got)
	}
}

func TestACommandOutsideTheWorkTreeOrWithoutAProgramIsRefused(t *testing.T) {
	a := newAgent(secret, "")
	for _, req := range []request{
		{Argv: nil, Dir: config.WorkspaceTreePath},
		{Argv: []string{"true"}, Dir: "/etc"},
		{Argv: []string{"true"}, Dir: config.WorkspaceTreePath + "/../etc"},
		{Argv: []string{"true"}, Dir: config.WorkspaceTreePath, Env: []string{"ROTOR_WORKSPACE_SECRET=x"}},
	} {
		if rec := call(t, a, "/exec", secret, req); rec.Code != http.StatusBadRequest {
			t.Errorf("exec %+v = %d", req, rec.Code)
		}
	}
}

// A timeout ends the command's whole process group, what it started in the background included.
func TestATimeoutEndsTheCommandAndWhatItStarted(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	start := time.Now()
	got := run(context.Background(), request{Argv: []string{"sh", "-c", "(sleep 3; touch " + marker + ") & sleep 30"},
		Dir: "/", TimeoutSeconds: 1, MaxOutputBytes: config.MaxCommandOutput})
	if !got.TimedOut || time.Since(start) > 10*time.Second {
		t.Fatalf("a command past its timeout = %+v after %s", got, time.Since(start))
	}
	time.Sleep(4 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a process the command started outlived its timeout")
	}
}

// Output past the cap keeps its first and last half, so both the start of a log and its verdict are read.
func TestOutputKeepsItsFirstAndLastHalfPastTheCap(t *testing.T) {
	got := run(context.Background(), request{Argv: []string{"seq", "1", "200000"}, Dir: "/", TimeoutSeconds: 10, MaxOutputBytes: 1024})
	if !got.Truncated || !strings.HasPrefix(got.Output, "1\n2\n3\n") || !strings.HasSuffix(got.Output, "199999\n200000\n") ||
		!strings.Contains(got.Output, "bytes left out") || len(got.Output) > 1024+64 {
		t.Errorf("200000 lines cut to %d bytes, truncated %v: %.80q ... %.80q", len(got.Output), got.Truncated, got.Output, got.Output[len(got.Output)-80:])
	}
	small := &capped{max: 8}
	_, _ = small.Write([]byte("abc"))
	_, _ = small.Write([]byte("de"))
	if out, cut := small.String(); out != "abcde" || cut {
		t.Errorf("output under the cap = %q %v", out, cut)
	}
}

// The agent's own variables, the secret among them, never reach a command.
func TestACommandDoesNotSeeTheAgentsSecret(t *testing.T) {
	t.Setenv(envSecret, "should-not-leak")
	t.Setenv(envListen, "127.0.0.1:0")
	for _, k := range []string{envListen, envSecret, envCache} {
		_ = os.Unsetenv(k)
	}
	got := run(context.Background(), request{Argv: []string{"env"}, Dir: "/", Env: []string{"GOFLAGS=-mod=mod"}, TimeoutSeconds: 5, MaxOutputBytes: config.MaxCommandOutput})
	if strings.Contains(got.Output, "should-not-leak") || !strings.Contains(got.Output, "GOFLAGS=-mod=mod") {
		t.Errorf("a command's environment = %s", got.Output)
	}
}

// A reset ends every process of its container, so it refuses to run anywhere but as a container's first process.
func TestAResetRefusesOutsideAContainerOfItsOwn(t *testing.T) {
	if os.Getpid() == 1 {
		t.Skip("the test runs as a container's first process")
	}
	rec := call(t, newAgent(secret, ""), "/reset", secret, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "first process") {
		t.Errorf("reset outside a container = %d %s", rec.Code, rec.Body)
	}
}

// The caches go with a reset, the module cache's read-only directories included.
func TestEmptyingTheCachesRemovesReadOnlyDirectories(t *testing.T) {
	cache := t.TempDir()
	locked := filepath.Join(cache, "mod", "example.com", "m@v1.0.0")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "go.mod"), []byte("module m\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	if removed, err := emptyDir(cache); err != nil || removed != 1 {
		t.Fatalf("emptying the caches = %d, %v", removed, err)
	}
	if left, _ := os.ReadDir(cache); len(left) != 0 {
		t.Errorf("the caches still hold %v", left)
	}
}

func TestTheAgentInstallsItselfWhereTheToolchainFindsIt(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "agent")
	if err := install(dst); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dst); err != nil || info.Mode().Perm() != binaryMode || info.Size() == 0 {
		t.Errorf("the installed agent = %v, %v", info, err)
	}
}
