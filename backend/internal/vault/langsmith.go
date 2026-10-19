package vault

import (
	"context"
	"time"
)

// LangSmithKey is the key runs' spans are sent to LangSmith with, which an administrator gave (#37).
type LangSmithKey struct {
	Key   string    `json:"key"`
	SetAt time.Time `json:"set_at"`
}

const langSmithPath = "settings/langsmith/key"

// LangSmithKey reads the LangSmith key and its version.
func (c *Client) LangSmithKey(ctx context.Context) (LangSmithKey, int, error) {
	var k LangSmithKey
	version, err := c.get(ctx, langSmithPath, &k)
	return k, version, err
}

// PutLangSmithKey writes the LangSmith key over the version read; 0 writes the first one.
func (c *Client) PutLangSmithKey(ctx context.Context, k LangSmithKey, version int) (int, error) {
	return c.put(ctx, langSmithPath, k, version)
}

// DeleteLangSmithKey forgets the LangSmith key; a missing one is no error.
func (c *Client) DeleteLangSmithKey(ctx context.Context) error {
	return c.delete(ctx, langSmithPath)
}
