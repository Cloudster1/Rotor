package vault

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RunToken is an access token Rotor handed to a run, kept only until it is revoked at its tool.
type RunToken struct {
	Token  string    `json:"token"`
	ToolID uuid.UUID `json:"tool_id"`
}

func runTokenPath(runID, tokenID uuid.UUID) (string, error) {
	if runID == uuid.Nil || tokenID == uuid.Nil {
		return "", errors.New("vault: a run's token needs both the run and the token's id")
	}
	return "runs/" + runID.String() + "/tokens/" + tokenID.String(), nil
}

// PutRunToken keeps a token handed to a run under an id of its own, until DeleteRunToken.
func (c *Client) PutRunToken(ctx context.Context, runID, tokenID uuid.UUID, t RunToken) error {
	path, err := runTokenPath(runID, tokenID)
	if err != nil {
		return err
	}
	_, err = c.put(ctx, path, t, 0)
	return err
}

// RunToken reads a token handed to a run; ErrNotFound once it is deleted.
func (c *Client) RunToken(ctx context.Context, runID, tokenID uuid.UUID) (RunToken, error) {
	var t RunToken
	path, err := runTokenPath(runID, tokenID)
	if err != nil {
		return t, err
	}
	_, err = c.get(ctx, path, &t)
	return t, err
}

// DeleteRunToken forgets a token handed to a run, every version of it.
func (c *Client) DeleteRunToken(ctx context.Context, runID, tokenID uuid.UUID) error {
	path, err := runTokenPath(runID, tokenID)
	if err != nil {
		return err
	}
	return c.delete(ctx, path)
}

// Wrap puts data in a single-use OpenBao wrapping token that lives ttl; whoever holds the token unwraps it once.
func (c *Client) Wrap(ctx context.Context, data map[string]any, ttl time.Duration) (string, error) {
	if err := c.ensure(ctx); err != nil {
		return "", err
	}
	wrapper, err := c.api.Clone()
	if err != nil {
		return "", fmt.Errorf("vault: wrap: %w", err)
	}
	wrapper.SetToken(c.api.Token())
	wrapper.SetWrappingLookupFunc(func(string, string) string { return ttl.String() })
	secret, err := wrapper.Logical().WriteWithContext(ctx, "sys/wrapping/wrap", data)
	if err != nil {
		return "", fmt.Errorf("vault: wrap: %w", err)
	}
	if secret == nil || secret.WrapInfo == nil || secret.WrapInfo.Token == "" {
		return "", errors.New("vault: OpenBao wrapped nothing")
	}
	return secret.WrapInfo.Token, nil
}
