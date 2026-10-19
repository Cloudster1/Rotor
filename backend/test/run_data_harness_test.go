//go:build integration

package test

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"testing"

	"github.com/google/uuid"
	openbao "github.com/openbao/openbao/api/v2"

	"github.com/cloudster1/rotor/backend/internal/config"
	sealing "github.com/cloudster1/rotor/backend/internal/envelope"
)

// rootTransit makes and opens runs' data keys with OpenBao's root token, as an operator could; the tests read and
// write runs behind the control plane's back with it.
type rootTransit struct{ client *openbao.Client }

func (r rootTransit) NewDataKey(ctx context.Context) ([]byte, string, error) {
	answer, err := r.client.Logical().WriteWithContext(ctx, "transit/datakey/plaintext/"+config.RunDataTransitKey,
		map[string]any{"bits": strconv.Itoa(config.DataKeyBits)})
	if err != nil {
		return nil, "", err
	}
	key, err := base64.StdEncoding.DecodeString(answer.Data["plaintext"].(string))
	return key, answer.Data["ciphertext"].(string), err
}

func (r rootTransit) UnwrapDataKeys(ctx context.Context, wrapped []string) ([][]byte, error) {
	out := make([][]byte, len(wrapped))
	for i, w := range wrapped {
		answer, err := r.client.Logical().WriteWithContext(ctx, "transit/decrypt/"+config.RunDataTransitKey, map[string]any{"ciphertext": w})
		if err != nil {
			return nil, err
		}
		if out[i], err = base64.StdEncoding.DecodeString(answer.Data["plaintext"].(string)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// runKeys are the keys of runs, as the control plane holds them.
func runKeys(t *testing.T) *sealing.Keys {
	t.Helper()
	return sealing.NewKeys(rootTransit{client: rootBao(t)})
}

// runBox is the box that opens what is stored of a run.
func (h *harness) runBox(t *testing.T, runID uuid.UUID) *sealing.Box {
	t.Helper()
	var wrapped string
	if err := h.super.QueryRow(context.Background(), `SELECT data_key FROM run WHERE id = $1`, runID).Scan(&wrapped); err != nil {
		t.Fatalf("read run %s's data key: %v", runID, err)
	}
	box, err := runKeys(t).For(context.Background(), runID, wrapped)
	if err != nil {
		t.Fatalf("open run %s's data key: %v", runID, err)
	}
	return box
}

// sealedRun is a new run's id, wrapped data key and sealed inputs, for a test that stores a run by hand.
func sealedRun(t *testing.T, inputs []byte) (uuid.UUID, string, []byte) {
	t.Helper()
	id := uuid.New()
	wrapped, box, err := runKeys(t).New(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return id, wrapped, box.Seal(sealing.ColumnInputs, "", inputs)
}

// placeholderKey and placeholderSealed stand in for a run's key and inputs in a row nothing ever opens.
const placeholderKey = "vault:v1:placeholder"

var placeholderSealed = []byte{1}

// openEvent opens one stored event of the run.
func openEvent(t *testing.T, box *sealing.Box, seq int64, sealed []byte) []byte {
	t.Helper()
	plain, err := box.Open(sealing.ColumnEvent, strconv.FormatInt(seq, 10), sealed)
	if err != nil {
		t.Fatalf("open event %d: %v", seq, err)
	}
	return plain
}

func describe(sealed []byte) string { return fmt.Sprintf("%x", sealed[:min(len(sealed), 16)]) }
