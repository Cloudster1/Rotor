package envelope

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, keyBytes)
	_, _ = rand.Read(key)
	return key
}

func TestASealedValueOpensOnlyWhereItWasSealed(t *testing.T) {
	run, other := uuid.New(), uuid.New()
	key := newKey(t)
	box, _ := NewBox(run, key)
	sealed := box.Seal(ColumnEvent, "7", []byte(`{"secret":"plans"}`))
	if bytes.Contains(sealed, []byte("plans")) || sealed[0] != version {
		t.Fatalf("the sealed value shows its plaintext or no version: %q", sealed)
	}
	if plain, err := box.Open(ColumnEvent, "7", sealed); err != nil || string(plain) != `{"secret":"plans"}` {
		t.Fatalf("open = %q, %v", plain, err)
	}
	elsewhere, _ := NewBox(other, key)
	wrongKey, _ := NewBox(run, newKey(t))
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	cases := map[string]func() ([]byte, error){
		"another row":    func() ([]byte, error) { return box.Open(ColumnEvent, "8", sealed) },
		"another column": func() ([]byte, error) { return box.Open(ColumnOutput, "7", sealed) },
		"another run":    func() ([]byte, error) { return elsewhere.Open(ColumnEvent, "7", sealed) },
		"another key":    func() ([]byte, error) { return wrongKey.Open(ColumnEvent, "7", sealed) },
		"a changed byte": func() ([]byte, error) { return box.Open(ColumnEvent, "7", tampered) },
		"too short":      func() ([]byte, error) { return box.Open(ColumnEvent, "7", sealed[:5]) },
		"another version": func() ([]byte, error) {
			v := append([]byte{version + 1}, sealed[1:]...)
			return box.Open(ColumnEvent, "7", v)
		},
	}
	for name, open := range cases {
		if _, err := open(); !errors.Is(err, ErrDamaged) {
			t.Errorf("%s opened: %v", name, err)
		}
	}
}

func TestTwoSealsOfOneValueDiffer(t *testing.T) {
	box, _ := NewBox(uuid.New(), newKey(t))
	if bytes.Equal(box.Seal(ColumnInputs, "", []byte("x")), box.Seal(ColumnInputs, "", []byte("x"))) {
		t.Error("two seals of one value are equal, so the nonce repeats")
	}
}

func TestAnAbsentValueStaysAbsent(t *testing.T) {
	box, _ := NewBox(uuid.New(), newKey(t))
	if plain, err := box.OpenOr(ColumnOutput, "", nil); plain != nil || err != nil {
		t.Errorf("OpenOr(nil) = %q, %v", plain, err)
	}
}

func TestAKeyOfTheWrongLengthIsRefused(t *testing.T) {
	if _, err := NewBox(uuid.New(), []byte("short")); err == nil {
		t.Error("a 5 byte key made a box")
	}
}
