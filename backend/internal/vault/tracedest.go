package vault

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// DestinationHeaders are the headers a trace destination is sent runs' spans with, which an administrator gave (#246).
type DestinationHeaders struct {
	Headers map[string]string `json:"headers"`
	SetAt   time.Time         `json:"set_at"`
}

func destinationPath(id uuid.UUID) (string, error) {
	if id == uuid.Nil {
		return "", errors.New("vault: a trace destination needs an id")
	}
	return "settings/trace-destinations/" + id.String() + "/headers", nil
}

// DestinationHeaders reads a destination's headers.
func (c *Client) DestinationHeaders(ctx context.Context, id uuid.UUID) (DestinationHeaders, error) {
	path, err := destinationPath(id)
	if err != nil {
		return DestinationHeaders{}, err
	}
	var h DestinationHeaders
	_, err = c.get(ctx, path, &h)
	return h, err
}

// PutDestinationHeaders writes a destination's headers over whatever it held.
func (c *Client) PutDestinationHeaders(ctx context.Context, id uuid.UUID, h DestinationHeaders) error {
	path, err := destinationPath(id)
	if err != nil {
		return err
	}
	var old DestinationHeaders
	version, err := c.get(ctx, path, &old)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = c.put(ctx, path, h, version)
	return err
}

// DeleteDestinationHeaders forgets a destination's headers; missing ones are no error.
func (c *Client) DeleteDestinationHeaders(ctx context.Context, id uuid.UUID) error {
	path, err := destinationPath(id)
	if err != nil {
		return err
	}
	return c.delete(ctx, path)
}
