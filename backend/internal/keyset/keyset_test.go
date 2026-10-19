package keyset

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestACursorReadsBackAsItWasWritten(t *testing.T) {
	c := Cursor{At: time.Date(2026, 10, 19, 8, 0, 0, 123456789, time.UTC), ID: uuid.New()}
	back, err := Decode(c.Encode())
	if err != nil || !back.At.Equal(c.At) || back.ID != c.ID {
		t.Fatalf("decoded %v (%v), want %v", back, err, c)
	}
	if start, err := Decode(""); start != nil || err != nil {
		t.Errorf("an empty cursor = %v, %v", start, err)
	}
	for _, bad := range []string{"!!", "bm8tYmFy", "MjAyNnx4"} {
		if _, err := Decode(bad); err != ErrBadCursor {
			t.Errorf("%q = %v, want ErrBadCursor", bad, err)
		}
	}
	if Next(2, 2, c) != nil || Next(2, 3, c) == nil {
		t.Error("Next must answer a cursor only when a row more than the limit was read")
	}
}
