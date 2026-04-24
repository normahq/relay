// Package throttle provides small, in-process function throttling.
package throttle

import (
	"sync"
	"time"
)

// Option configures a Throttler.
type Option func(*Throttler)

// Throttler executes at most one function per interval.
type Throttler struct {
	mu       sync.Mutex
	interval time.Duration
	now      func() time.Time
	last     time.Time
}

// New creates a Throttler. Non-positive intervals disable throttling.
func New(interval time.Duration, opts ...Option) *Throttler {
	t := &Throttler{
		interval: interval,
		now:      time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}
	if t.now == nil {
		t.now = time.Now
	}
	return t
}

// WithClock configures the clock used by the throttler.
func WithClock(clock func() time.Time) Option {
	return func(t *Throttler) {
		if clock != nil {
			t.now = clock
		}
	}
}

// Do executes fn when the throttle interval permits it.
func (t *Throttler) Do(fn func()) {
	if fn == nil {
		return
	}
	if t == nil {
		fn()
		return
	}

	now := t.now()
	t.mu.Lock()
	if t.interval > 0 && !t.last.IsZero() && now.Sub(t.last) < t.interval {
		t.mu.Unlock()
		return
	}
	t.last = now
	t.mu.Unlock()

	fn()
}
