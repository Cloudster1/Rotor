//go:build integration

package test

import (
	"net/http"
	"testing"
)

// probedFiles are what a hostile code node would read of the runner it runs under: its environment, memory, open
// files and memory map.
var probedFiles = []string{"environ", "mem", "fd", "maps"}

// prober is a code node that tries to read the runner it runs under, as a hostile one would (#89).
func prober() string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("prober") + `}
limits: {maxSteps: 5, timeout: 2m}
entry: probe
nodes:
  - id: probe
    type: code
    output: {schema: {type: object}}
    source: |
      import os
      def run(state, ctx):
          parent, seen = os.getppid(), {}
          for name in ("environ", "mem", "fd", "maps"):
              path = "/proc/%d/%s" % (parent, name)
              try:
                  if name == "fd":
                      os.listdir(path)
                  else:
                      open(path, "rb").close()
                  seen[name] = "read"
              except PermissionError:
                  seen[name] = "denied"
              except OSError as err:
                  seen[name] = type(err).__name__
          return seen
`
}

func TestACodeNodeCannotReadTheRunnerItRunsUnder(t *testing.T) {
	newHarness(t)
	alice := signedIn(t, "alice")
	run := startRun(t, alice, createWorkflow(t, alice, prober()), map[string]any{}, http.StatusCreated)
	done := waitRun(t, alice, run["id"].(string), "succeeded", "failed")
	output, _ := done["output"].(map[string]any)
	seen, _ := output["probe"].(map[string]any)
	if done["status"] != "succeeded" {
		t.Fatalf("the probe ended %v: %v", done["status"], done["reason"])
	}
	for _, name := range probedFiles {
		if seen[name] != "denied" {
			t.Errorf("a code node opened the runner's /proc/<pid>/%s: %v", name, seen[name])
		}
	}
}
