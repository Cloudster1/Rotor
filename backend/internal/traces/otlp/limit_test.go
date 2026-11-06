package otlp

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// clock is a time a test turns by hand.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func TestASenderIsHeldToItsRequestsInAWindowAndNoOtherSenderIs(t *testing.T) {
	c := &clock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	l := NewLimiter(c.now)
	sender, other := uuid.New(), uuid.New()
	for i := range config.OTLPRequestsPerMinute {
		if _, ok := l.Request(sender); !ok {
			t.Fatalf("request %d of the window was refused", i+1)
		}
	}
	c.at = c.at.Add(20 * time.Second)
	wait, ok := l.Request(sender)
	if ok || wait != 40*time.Second {
		t.Errorf("a request past the window's = %v, %v; want a wait of 40s to the window's end", wait, ok)
	}
	if _, ok := l.Request(other); !ok {
		t.Error("another sender was held to the first one's window")
	}
	c.at = c.at.Add(40 * time.Second)
	if _, ok := l.Request(sender); !ok {
		t.Error("a request in the next window was refused")
	}
}

func TestASendersSpansAreCountedUntilTheyWouldPassTheWindows(t *testing.T) {
	c := &clock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	l := NewLimiter(c.now)
	sender := uuid.New()
	if _, ok := l.Spans(sender, config.OTLPSpansPerMinute-10); !ok {
		t.Fatal("spans within the window were refused")
	}
	c.at = c.at.Add(59*time.Second + 700*time.Millisecond)
	wait, ok := l.Spans(sender, 11)
	if ok || wait != time.Second {
		t.Errorf("spans past the window's = %v, %v; want a wait of at least a second", wait, ok)
	}
	if _, ok := l.Spans(sender, 10); !ok {
		t.Error("a refused request's spans were counted")
	}
	c.at = c.at.Add(time.Second)
	if _, ok := l.Spans(sender, config.OTLPSpansPerMinute); !ok {
		t.Error("spans in the next window were refused")
	}
}

// Senders of past windows are forgotten, so the map holds only those of the last minute.
func TestSendersOfPastWindowsAreForgotten(t *testing.T) {
	c := &clock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	l := NewLimiter(c.now)
	for range 5 {
		l.Request(uuid.New())
	}
	c.at = c.at.Add(config.OTLPRateWindow)
	l.Request(uuid.New())
	if len(l.windows) != 1 {
		t.Errorf("the limiter holds %d senders, want the one of this window", len(l.windows))
	}
}
