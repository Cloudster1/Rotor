package envelope

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeTransit wraps a key as its hex name and counts what OpenBao would be asked.
type fakeTransit struct {
	keys    map[string][]byte
	made    int
	unwraps int
	asked   int
}

func (f *fakeTransit) NewDataKey(context.Context) ([]byte, string, error) {
	key := make([]byte, keyBytes)
	_, _ = rand.Read(key)
	f.made++
	wrapped := fmt.Sprintf("vault:v1:%d", f.made)
	f.keys[wrapped] = key
	return key, wrapped, nil
}

func (f *fakeTransit) UnwrapDataKeys(_ context.Context, wrapped []string) ([][]byte, error) {
	f.unwraps++
	f.asked += len(wrapped)
	out := make([][]byte, len(wrapped))
	for i, w := range wrapped {
		key, ok := f.keys[w]
		if !ok {
			return nil, fmt.Errorf("unknown key %s", w)
		}
		out[i] = key
	}
	return out, nil
}

func newKeys() (*Keys, *fakeTransit, *time.Time) {
	transit := &fakeTransit{keys: map[string][]byte{}}
	k := NewKeys(transit)
	now := time.Now()
	k.now = func() time.Time { return now }
	return k, transit, &now
}

func TestAKeyInMemoryIsNotAskedForAgainUntilItIsIdleTooLong(t *testing.T) {
	ctx := context.Background()
	k, transit, now := newKeys()
	run := uuid.New()
	wrapped, box, err := k.New(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	sealed := box.Seal(ColumnInputs, "", []byte("hi"))
	again, err := k.For(ctx, run, wrapped)
	if err != nil || transit.unwraps != 0 {
		t.Fatalf("a fresh key was unwrapped again: %v, %d", err, transit.unwraps)
	}
	if plain, err := again.Open(ColumnInputs, "", sealed); err != nil || string(plain) != "hi" {
		t.Fatalf("the cached box opens %q, %v", plain, err)
	}
	*now = now.Add(k.ttl - time.Second)
	_, _ = k.For(ctx, run, wrapped)
	*now = now.Add(k.ttl - time.Second)
	if _, _ = k.For(ctx, run, wrapped); transit.unwraps != 0 {
		t.Error("a key in use expired; its time counts from its last use")
	}
	*now = now.Add(k.ttl + time.Second)
	if _, _ = k.For(ctx, run, wrapped); transit.unwraps != 1 {
		t.Errorf("an idle key stayed in memory: %d unwraps", transit.unwraps)
	}
}

func TestAnotherWrappedKeyForTheRunIsUnwrapped(t *testing.T) {
	ctx := context.Background()
	k, transit, _ := newKeys()
	run := uuid.New()
	_, _, _ = k.New(ctx, run)
	_, other, _ := transit.NewDataKey(ctx)
	if _, err := k.For(ctx, run, other); err != nil || transit.unwraps != 1 {
		t.Errorf("a different wrapped key for the run used the cached one: %v, %d", err, transit.unwraps)
	}
}

func TestManyMissingKeysAreUnwrappedInOneCall(t *testing.T) {
	ctx := context.Background()
	k, transit, _ := newKeys()
	wanted := map[uuid.UUID]string{}
	for range 5 {
		_, wrapped, _ := transit.NewDataKey(ctx)
		wanted[uuid.New()] = wrapped
	}
	boxes, err := k.ForMany(ctx, wanted)
	if err != nil || len(boxes) != 5 || transit.unwraps != 1 || transit.asked != 5 {
		t.Fatalf("ForMany = %d boxes, %v, %d calls for %d keys", len(boxes), err, transit.unwraps, transit.asked)
	}
	if _, err := k.ForMany(ctx, wanted); err != nil || transit.unwraps != 1 {
		t.Errorf("the second page asked OpenBao again: %d", transit.unwraps)
	}
}

func TestTheLeastRecentlyUsedKeyGoesFirst(t *testing.T) {
	ctx := context.Background()
	k, transit, _ := newKeys()
	k.max = 2
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	wa, _, _ := k.New(ctx, a)
	_, _, _ = k.New(ctx, b)
	_, _ = k.For(ctx, a, wa)
	_, _, _ = k.New(ctx, c)
	if k.Len() != 2 {
		t.Fatalf("%d keys in memory, want 2", k.Len())
	}
	if _, _ = k.For(ctx, a, wa); transit.unwraps != 0 {
		t.Error("the recently used key was dropped instead of the oldest")
	}
}

func TestARunWithoutAKeyIsRefused(t *testing.T) {
	k, _, _ := newKeys()
	if _, err := k.For(context.Background(), uuid.New(), ""); err == nil {
		t.Error("a run without a data key got a box")
	}
}
