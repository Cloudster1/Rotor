package vault

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// NewDataKey has OpenBao make a run's data key: the key itself, used at once and only kept in memory, and the same
// key wrapped by the installation's transit key, which is stored with the run.
func (c *Client) NewDataKey(ctx context.Context) ([]byte, string, error) {
	if err := c.ensure(ctx); err != nil {
		return nil, "", err
	}
	path := "transit/datakey/plaintext/" + config.RunDataTransitKey
	secret, err := c.api.Logical().WriteWithContext(ctx, path, map[string]any{"bits": strconv.Itoa(config.DataKeyBits)})
	if err != nil {
		return nil, "", fmt.Errorf("vault: %s: %w", path, err)
	}
	if secret == nil {
		return nil, "", fmt.Errorf("vault: %s answered nothing", path)
	}
	plain, _ := secret.Data["plaintext"].(string)
	wrapped, _ := secret.Data["ciphertext"].(string)
	key, err := base64.StdEncoding.DecodeString(plain)
	if err != nil || len(key)*8 != config.DataKeyBits || wrapped == "" {
		return nil, "", fmt.Errorf("vault: %s answered no usable key", path)
	}
	return key, wrapped, nil
}

// UnwrapDataKeys opens wrapped data keys in one call, in order.
func (c *Client) UnwrapDataKeys(ctx context.Context, wrapped []string) ([][]byte, error) {
	batch := make([]map[string]any, len(wrapped))
	for i, text := range wrapped {
		batch[i] = map[string]any{"ciphertext": text}
	}
	results, err := c.transit(ctx, "transit/decrypt/"+config.RunDataTransitKey, batch, "plaintext")
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
