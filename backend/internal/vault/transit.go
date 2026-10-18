package vault

import (
	"context"
	"encoding/base64"
	"fmt"
)

// checkpointKey is the transit key run checkpoints are encrypted with.
const checkpointKey = "rotor-checkpoints"

// Encrypt seals each value with the checkpoint key in one call; the answers are OpenBao ciphertexts, in order.
func (c *Client) Encrypt(ctx context.Context, values [][]byte) ([]string, error) {
	batch := make([]map[string]any, len(values))
	for i, v := range values {
		batch[i] = map[string]any{"plaintext": base64.StdEncoding.EncodeToString(v)}
	}
	results, err := c.transit(ctx, "transit/encrypt/"+checkpointKey, batch, "ciphertext")
	if err != nil {
		return nil, err
	}
	return results, nil
}

// Decrypt opens ciphertexts the checkpoint key sealed, in order.
func (c *Client) Decrypt(ctx context.Context, ciphertexts []string) ([][]byte, error) {
	batch := make([]map[string]any, len(ciphertexts))
	for i, text := range ciphertexts {
		batch[i] = map[string]any{"ciphertext": text}
	}
	results, err := c.transit(ctx, "transit/decrypt/"+checkpointKey, batch, "plaintext")
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(results))
	for i, r := range results {
		if out[i], err = base64.StdEncoding.DecodeString(r); err != nil {
			return nil, fmt.Errorf("vault: OpenBao answered plaintext that is not base64: %w", err)
		}
	}
	return out, nil
}

func (c *Client) transit(ctx context.Context, path string, batch []map[string]any, field string) ([]string, error) {
	if len(batch) == 0 {
		return nil, nil
	}
	if err := c.ensure(ctx); err != nil {
		return nil, err
	}
	secret, err := c.api.Logical().WriteWithContext(ctx, path, map[string]any{"batch_input": batch})
	if err != nil {
		return nil, fmt.Errorf("vault: %s: %w", path, err)
	}
	if secret == nil {
		return nil, fmt.Errorf("vault: %s answered nothing", path)
	}
	results, _ := secret.Data["batch_results"].([]any)
	if len(results) != len(batch) {
		return nil, fmt.Errorf("vault: %s answered %d results for %d values", path, len(results), len(batch))
	}
	out := make([]string, len(results))
	for i, r := range results {
		item, _ := r.(map[string]any)
		if msg, _ := item["error"].(string); msg != "" {
			return nil, fmt.Errorf("vault: %s: %s", path, msg)
		}
		out[i], _ = item[field].(string)
	}
	return out, nil
}
