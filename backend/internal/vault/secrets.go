package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	openbao "github.com/openbao/openbao/api/v2"
)

// ConnectionSecret is what a person's connection to a tool needs to mint tokens: never stored elsewhere.
type ConnectionSecret struct {
	RefreshToken string    `json:"refresh_token"`
	Scope        string    `json:"scope"`
	ClientID     string    `json:"client_id"`
	ObtainedAt   time.Time `json:"obtained_at"`
}

// ToolClient is Rotor's OAuth client at a tool.
type ToolClient struct {
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	AuthMethod   string    `json:"auth_method"`
	RegisteredAt time.Time `json:"registered_at"`
}

func connectionPath(userID, toolID uuid.UUID) (string, error) {
	if userID == uuid.Nil || toolID == uuid.Nil {
		return "", errors.New("vault: a connection needs both a person and a tool")
	}
	return "connections/" + userID.String() + "/" + toolID.String(), nil
}

func toolClientPath(toolID uuid.UUID) (string, error) {
	if toolID == uuid.Nil {
		return "", errors.New("vault: a tool client needs a tool")
	}
	return "tools/" + toolID.String() + "/client", nil
}

// Connection reads a person's connection secret and the version a later write must name.
func (c *Client) Connection(ctx context.Context, userID, toolID uuid.UUID) (ConnectionSecret, int, error) {
	var s ConnectionSecret
	path, err := connectionPath(userID, toolID)
	if err != nil {
		return s, 0, err
	}
	version, err := c.get(ctx, path, &s)
	return s, version, err
}

// PutConnection writes a connection secret over the version read; 0 writes the first one.
func (c *Client) PutConnection(ctx context.Context, userID, toolID uuid.UUID, s ConnectionSecret, version int) (int, error) {
	path, err := connectionPath(userID, toolID)
	if err != nil {
		return 0, err
	}
	return c.put(ctx, path, s, version)
}

// DeleteConnection forgets a connection secret with every version of it; a missing one is no error.
func (c *Client) DeleteConnection(ctx context.Context, userID, toolID uuid.UUID) error {
	path, err := connectionPath(userID, toolID)
	if err != nil {
		return err
	}
	return c.delete(ctx, path)
}

// ToolClient reads Rotor's client at a tool and its version.
func (c *Client) ToolClient(ctx context.Context, toolID uuid.UUID) (ToolClient, int, error) {
	var s ToolClient
	path, err := toolClientPath(toolID)
	if err != nil {
		return s, 0, err
	}
	version, err := c.get(ctx, path, &s)
	return s, version, err
}

// PutToolClient writes Rotor's client at a tool over the version read.
func (c *Client) PutToolClient(ctx context.Context, toolID uuid.UUID, s ToolClient, version int) (int, error) {
	path, err := toolClientPath(toolID)
	if err != nil {
		return 0, err
	}
	return c.put(ctx, path, s, version)
}

// DeleteToolClient forgets Rotor's client at a tool.
func (c *Client) DeleteToolClient(ctx context.Context, toolID uuid.UUID) error {
	path, err := toolClientPath(toolID)
	if err != nil {
		return err
	}
	return c.delete(ctx, path)
}

func (c *Client) get(ctx context.Context, path string, into any) (int, error) {
	if err := c.ensure(ctx); err != nil {
		return 0, err
	}
	secret, err := c.kv.Get(ctx, path)
	if errors.Is(err, openbao.ErrSecretNotFound) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("vault: read %s: %w", path, err)
	}
	encoded, err := json.Marshal(secret.Data)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(encoded, into); err != nil {
		return 0, fmt.Errorf("vault: %s holds something else: %w", path, err)
	}
	return secret.VersionMetadata.Version, nil
}

func (c *Client) put(ctx context.Context, path string, value any, version int) (int, error) {
	if err := c.ensure(ctx); err != nil {
		return 0, err
	}
	var data map[string]any
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(encoded, &data); err != nil {
		return 0, err
	}
	secret, err := c.kv.Put(ctx, path, data, openbao.WithCheckAndSet(version))
	if err != nil {
		if strings.Contains(err.Error(), "check-and-set parameter did not match") {
			return 0, ErrConflict
		}
		return 0, fmt.Errorf("vault: write %s: %w", path, err)
	}
	return secret.VersionMetadata.Version, nil
}

func (c *Client) delete(ctx context.Context, path string) error {
	if err := c.ensure(ctx); err != nil {
		return err
	}
	if err := c.kv.DeleteMetadata(ctx, path); err != nil {
		return fmt.Errorf("vault: delete %s: %w", path, err)
	}
	return nil
}
