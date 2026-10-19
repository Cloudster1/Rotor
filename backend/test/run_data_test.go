//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// leaky is a run that prints and answers credentials of known shapes, with a marker from its inputs.
func leaky() string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("leaky") + `}
inputs:
  type: object
  required: [note]
  properties:
    note: {type: string}
limits: {maxSteps: 10, timeout: 2m}
entry: leak
nodes:
  - id: leak
    type: code
    output: {schema: {type: object}}
    source: |
      def run(state, ctx):
          print("pushing with ghp_" + "0123456789abcdefABCDEF0123456789")
          return {"note": state["inputs"]["note"], "key": "AKIAIOSFODNN7EXAMPLE"}
`
}

func TestARunsDataIsCiphertextAtRestAndCarriesNoCredential(t *testing.T) {
	h := newHarness(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	marker := "plaintext-" + uuid.NewString()
	run := startRun(t, alice, createWorkflow(t, alice, leaky()), map[string]any{"note": marker}, http.StatusCreated)
	runID := run["id"].(string)
	done := waitRun(t, alice, runID, "succeeded", "failed")
	if done["status"] != "succeeded" {
		t.Fatalf("the run ended %v: %v", done["status"], done["reason"])
	}

	// Through the api the run reads as it was given and answered, with the credential cut.
	inputs, _ := done["inputs"].(map[string]any)
	output, _ := done["output"].(map[string]any)
	leak, _ := output["leak"].(map[string]any)
	if inputs["note"] != marker || leak["note"] != marker || leak["key"] != "[redacted]" {
		t.Errorf("the run reads inputs %v and output %v", inputs, output)
	}
	events, _ := json.Marshal(h.events(t, uuid.MustParse(runID)))
	if strings.Contains(string(events), "ghp_0123") || strings.Contains(string(events), "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(string(events), marker) {
		t.Errorf("the run's events hold a credential, or not the run's data: %s", events)
	}

	// At rest every column of the run's data is sealed, with the version first, and the marker is nowhere.
	var dataKey string
	var raw [][]byte
	rows, err := h.super.Query(context.Background(), `
		SELECT data_key, sealed_inputs FROM run WHERE id = $1
		UNION ALL SELECT data_key, sealed_output FROM run WHERE id = $1
		UNION ALL SELECT '', sealed_payload FROM run_event WHERE run_id = $1
		UNION ALL SELECT '', sealed_message FROM run_control WHERE run_id = $1
		UNION ALL SELECT '', sealed_checkpoint FROM run_checkpoint WHERE run_id = $1
		UNION ALL SELECT '', sealed_value FROM run_checkpoint_write WHERE run_id = $1`, runID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatal(err)
		}
		if key != "" {
			dataKey = key
		}
		raw = append(raw, value)
	}
	rows.Close()
	if !strings.HasPrefix(dataKey, "vault:v") || len(raw) < 4 {
		t.Fatalf("the run keeps data key %q and %d sealed values", dataKey, len(raw))
	}
	for i, value := range raw {
		if len(value) == 0 || value[0] != 1 || bytes.Contains(value, []byte(marker)) || bytes.Contains(value, []byte("ghp_0123")) {
			t.Errorf("value %d is not sealed: %s", i, describe(value))
		}
	}
}
