package envelope

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Transit makes and unwraps data keys with the installation's transit key; *vault.Client is the real one.
type Transit interface {
	NewDataKey(ctx context.Context) ([]byte, string, error)
	UnwrapDataKeys(ctx context.Context, wrapped []string) ([][]byte, error)
}

// ErrNoRun is a run whose data key is not there to read, such as one deleted or not visible to the caller.
var ErrNoRun = errors.New("envelope: no such run")

// Keys hands out the boxes of runs, keeping their unwrapped keys in memory briefly so a run's stream, its followers
// and its pages do not each ask OpenBao. Keys never leave the process: no cache shared across replicas holds one.
type Keys struct {
	transit Transit
	ttl     time.Duration
	max     int
	now     func() time.Time

	mu      sync.Mutex
	entries map[uuid.UUID]*list.Element
	order   *list.List
}

type entry struct {
	run     uuid.UUID
	wrapped string
	box     *Box
	used    time.Time
}

// NewKeys returns the keys of runs, unwrapped through transit.
func NewKeys(transit Transit) *Keys {
	return &Keys{transit: transit, ttl: config.DataKeyCacheTTL, max: config.DataKeyCacheEntries, now: time.Now,
		entries: map[uuid.UUID]*list.Element{}, order: list.New()}
}

// New makes the data key of a run about to be stored: the box to seal its first values with, and the wrapped key
// to store with it.
func (k *Keys) New(ctx context.Context, run uuid.UUID) (string, *Box, error) {
	key, wrapped, err := k.transit.NewDataKey(ctx)
	if err != nil {
		return "", nil, err
	}
	box, err := NewBox(run, key)
	if err != nil {
		return "", nil, err
	}
	k.put(run, wrapped, box)
	return wrapped, box, nil
}

// For is the box of a run whose wrapped key the caller read with it.
func (k *Keys) For(ctx context.Context, run uuid.UUID, wrapped string) (*Box, error) {
	boxes, err := k.ForMany(ctx, map[uuid.UUID]string{run: wrapped})
	if err != nil {
		return nil, err
	}
	return boxes[run], nil
}

// ForMany is the boxes of several runs, such as a page of them, unwrapping those not in memory in one call.
func (k *Keys) ForMany(ctx context.Context, wanted map[uuid.UUID]string) (map[uuid.UUID]*Box, error) {
	out := make(map[uuid.UUID]*Box, len(wanted))
	var missing []uuid.UUID
	var wrapped []string
	for run, w := range wanted {
		if w == "" {
			return nil, fmt.Errorf("envelope: run %s has no data key", run)
		}
		if box := k.cached(run, w); box != nil {
			out[run] = box
			continue
		}
		missing, wrapped = append(missing, run), append(wrapped, w)
	}
	if len(missing) == 0 {
		return out, nil
	}
	keys, err := k.transit.UnwrapDataKeys(ctx, wrapped)
	if err != nil {
		return nil, err
	}
	for i, run := range missing {
		box, err := NewBox(run, keys[i])
		if err != nil {
			return nil, err
		}
		k.put(run, wrapped[i], box)
		out[run] = box
	}
	return out, nil
}

// Run reads a run's wrapped key in tx and answers its box; the caller's role must see the run.
func (k *Keys) Run(ctx context.Context, tx db.DBTX, run uuid.UUID) (*Box, error) {
	var wrapped string
	err := tx.QueryRow(ctx, `SELECT data_key FROM run WHERE id = $1`, run).Scan(&wrapped)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoRun
	}
	if err != nil {
		return nil, err
	}
	return k.For(ctx, run, wrapped)
}

// cached is a run's box if it is in memory for the same wrapped key and still fresh, marking it used.
func (k *Keys) cached(run uuid.UUID, wrapped string) *Box {
	k.mu.Lock()
	defer k.mu.Unlock()
	el, ok := k.entries[run]
	if !ok {
		return nil
	}
	e := el.Value.(*entry)
	now := k.now()
	if e.wrapped != wrapped || now.Sub(e.used) > k.ttl {
		k.order.Remove(el)
		delete(k.entries, run)
		return nil
	}
	e.used = now
	k.order.MoveToFront(el)
	return e.box
}

// put keeps a run's box, dropping the least recently used beyond the bound.
func (k *Keys) put(run uuid.UUID, wrapped string, box *Box) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if el, ok := k.entries[run]; ok {
		k.order.Remove(el)
	}
	k.entries[run] = k.order.PushFront(&entry{run: run, wrapped: wrapped, box: box, used: k.now()})
	for k.order.Len() > k.max {
		last := k.order.Back()
		k.order.Remove(last)
		delete(k.entries, last.Value.(*entry).run)
	}
}

// Len is how many keys are in memory, for tests.
func (k *Keys) Len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.order.Len()
}
