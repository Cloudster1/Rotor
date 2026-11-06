package otlp

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Limiter counts each sender's requests and spans in this replica's memory, in windows of config.OTLPRateWindow from
// the sender's first request: no round trip per export, and n replicas take at most n times the rate.
type Limiter struct {
	now func() time.Time

	mu      sync.Mutex
	windows map[uuid.UUID]*window
	swept   time.Time
}

type window struct {
	start           time.Time
	requests, spans int
}

// NewLimiter counts against now, the clock a test turns.
func NewLimiter(now func() time.Time) *Limiter {
	return &Limiter{now: now, windows: map[uuid.UUID]*window{}}
}

// current is the sender's window now, a fresh one once the last has ended; windows that ended are forgotten once a
// window, so the map holds only senders of the last minute.
func (l *Limiter) current(sender uuid.UUID) (*window, time.Time) {
	now := l.now()
	if now.Sub(l.swept) >= config.OTLPRateWindow {
		for id, w := range l.windows {
			if now.Sub(w.start) >= config.OTLPRateWindow {
				delete(l.windows, id)
			}
		}
		l.swept = now
	}
	w := l.windows[sender]
	if w == nil || now.Sub(w.start) >= config.OTLPRateWindow {
		w = &window{start: now}
		l.windows[sender] = w
	}
	return w, now
}

// wait is how long until a window ends, in whole seconds as Retry-After writes it, at least one.
func wait(w *window, now time.Time) time.Duration {
	left := w.start.Add(config.OTLPRateWindow).Sub(now)
	return max(left.Round(time.Second), time.Second)
}

// Request counts a request of sender's, or answers how long to wait when the sender has made its window's requests.
func (l *Limiter) Request(sender uuid.UUID) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, now := l.current(sender)
	if w.requests >= config.OTLPRequestsPerMinute {
		return wait(w, now), false
	}
	w.requests++
	return 0, true
}

// Spans counts n spans of sender's, or answers how long to wait when they would take the sender past its window's
// spans; a refused request counts none.
func (l *Limiter) Spans(sender uuid.UUID, n int) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, now := l.current(sender)
	if w.spans+n > config.OTLPSpansPerMinute {
		return wait(w, now), false
	}
	w.spans += n
	return 0, true
}
