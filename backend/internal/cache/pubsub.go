package cache

import (
	"context"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Publish tells every subscriber of channel, on any replica, to look again; a nil *Cache or a failure tells nobody,
// so whoever listens must also look on its own from time to time.
func (c *Cache) Publish(ctx context.Context, channel string, message []byte) {
	if c == nil {
		return
	}
	if err := c.client.Publish(ctx, keyPrefix+channel, message).Err(); err != nil {
		c.log.Warn("cache publish failed", "channel", channel, "error", err)
	}
}

// Subscribe answers the messages published to channel until ctx ends; a nil *Cache answers a channel that stays quiet.
func (c *Cache) Subscribe(ctx context.Context, channel string) <-chan []byte {
	out := make(chan []byte, config.SignalBuffer)
	if c == nil {
		return out
	}
	sub := c.client.Subscribe(ctx, keyPrefix+channel)
	go func() {
		defer sub.Close()
		messages := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-messages:
				if !ok {
					return
				}
				select {
				case out <- []byte(m.Payload):
				default:
				}
			}
		}
	}()
	return out
}
