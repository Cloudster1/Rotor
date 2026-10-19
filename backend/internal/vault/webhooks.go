package vault

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// WebhookSecret is the secret a tool signs its webhook deliveries to Rotor with; the tool made it (#35).
type WebhookSecret struct {
	Secret string    `json:"secret"`
	SetAt  time.Time `json:"set_at"`
}

func webhookPath(toolID uuid.UUID) (string, error) {
	if toolID == uuid.Nil {
		return "", errors.New("vault: a webhook secret needs a tool")
	}
	return "tools/" + toolID.String() + "/webhook", nil
}

// WebhookSecret reads a tool's webhook secret and its version.
func (c *Client) WebhookSecret(ctx context.Context, toolID uuid.UUID) (WebhookSecret, int, error) {
	var s WebhookSecret
	path, err := webhookPath(toolID)
	if err != nil {
		return s, 0, err
	}
	version, err := c.get(ctx, path, &s)
	return s, version, err
}

// PutWebhookSecret writes a tool's webhook secret over the version read; 0 writes the first one.
func (c *Client) PutWebhookSecret(ctx context.Context, toolID uuid.UUID, s WebhookSecret, version int) (int, error) {
	path, err := webhookPath(toolID)
	if err != nil {
		return 0, err
	}
	return c.put(ctx, path, s, version)
}

// DeleteWebhookSecret forgets a tool's webhook secret; a missing one is no error.
func (c *Client) DeleteWebhookSecret(ctx context.Context, toolID uuid.UUID) error {
	path, err := webhookPath(toolID)
	if err != nil {
		return err
	}
	return c.delete(ctx, path)
}
