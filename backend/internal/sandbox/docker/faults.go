package docker

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// The faults the runner protocol's tests inject into a live sandbox: a network that drops, a runner that freezes.

type networkMember struct {
	Container string `json:"Container"`
	Force     bool   `json:"Force,omitempty"`
}

// Disconnect takes the sandbox off its network, as a broken link would.
func (b *Backend) Disconnect(ctx context.Context, id sandbox.ID) error {
	return b.call(ctx, http.MethodPost, "/networks/"+url.PathEscape(b.network)+"/disconnect",
		networkMember{Container: string(id), Force: true}, nil, http.StatusOK)
}

// Reconnect puts the sandbox back on its network.
func (b *Backend) Reconnect(ctx context.Context, id sandbox.ID) error {
	return b.call(ctx, http.MethodPost, "/networks/"+url.PathEscape(b.network)+"/connect",
		networkMember{Container: string(id)}, nil, http.StatusOK)
}

// Pause freezes every process of the sandbox, as a stalled host would.
func (b *Backend) Pause(ctx context.Context, id sandbox.ID) error {
	return b.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(string(id))+"/pause", nil, nil, http.StatusNoContent)
}

// Unpause lets a paused sandbox go on.
func (b *Backend) Unpause(ctx context.Context, id sandbox.ID) error {
	return b.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(string(id))+"/unpause", nil, nil, http.StatusNoContent)
}
