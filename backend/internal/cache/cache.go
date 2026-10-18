// Package cache keeps what Rotor can fetch again in Valkey, such as the tools' catalogs. Losing it costs a fetch,
// never data, so every failure reads as a miss.
package cache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyPrefix keeps Rotor's keys apart from anything else sharing the Valkey.
const keyPrefix = "rotor:"

// Cache stores bytes under a key for a while; a nil *Cache stores nothing.
type Cache struct {
	client *redis.Client
	log    *slog.Logger
}

// Open connects to Valkey at url (redis://[:password@]host:port/db); an empty url answers a cache that keeps nothing.
func Open(url string, log *slog.Logger) (*Cache, error) {
	if url == "" {
		return nil, nil
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, errors.New("cache: ROTOR_VALKEY_URL is not a redis:// address")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Cache{client: redis.NewClient(opts), log: log}, nil
}

// Get answers what is stored under key; found is false on a miss or when Valkey does not answer.
func (c *Cache) Get(ctx context.Context, key string) (value []byte, found bool) {
	if c == nil {
		return nil, false
	}
	value, err := c.client.Get(ctx, keyPrefix+key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			c.log.Warn("cache read failed, fetching instead", "error", err)
		}
		return nil, false
	}
	return value, true
}

// Set stores value under key for ttl; a failure is logged, since the next read just fetches again.
func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) {
	if c == nil {
		return
	}
	if err := c.client.Set(ctx, keyPrefix+key, value, ttl).Err(); err != nil {
		c.log.Warn("cache write failed", "error", err)
	}
}

// Close lets go of the connections.
func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	return c.client.Close()
}
