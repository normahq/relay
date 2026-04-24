package throttle

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestThrottlerDoExecutesFirstCallImmediately(t *testing.T) {
	t.Parallel()

	throttler := New(time.Second, WithClock(func() time.Time {
		return time.Date(2026, 4, 24, 20, 0, 0, 0, time.UTC)
	}))

	calls := 0
	throttler.Do(func() {
		calls++
	})

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestThrottlerDoSuppressesCallsInsideInterval(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 4, 24, 20, 0, 0, 0, time.UTC)
	times := []time.Time{
		base,
		base.Add(500 * time.Millisecond),
		base.Add(999 * time.Millisecond),
	}
	idx := 0
	throttler := New(time.Second, WithClock(func() time.Time {
		now := times[idx]
		idx++
		return now
	}))

	calls := 0
	for range times {
		throttler.Do(func() {
			calls++
		})
	}

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestThrottlerDoExecutesAtOrAfterInterval(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 4, 24, 20, 0, 0, 0, time.UTC)
	times := []time.Time{
		base,
		base.Add(time.Second - time.Nanosecond),
		base.Add(time.Second),
		base.Add(2 * time.Second),
	}
	idx := 0
	throttler := New(time.Second, WithClock(func() time.Time {
		now := times[idx]
		idx++
		return now
	}))

	calls := 0
	for range times {
		throttler.Do(func() {
			calls++
		})
	}

	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestThrottlerDoAllowsEveryCallWhenIntervalDisabled(t *testing.T) {
	t.Parallel()

	throttler := New(0)

	calls := 0
	for range 3 {
		throttler.Do(func() {
			calls++
		})
	}

	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestThrottlerDoConcurrentCallsExecuteOncePerInterval(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 24, 20, 0, 0, 0, time.UTC)
	throttler := New(time.Second, WithClock(func() time.Time {
		return now
	}))

	var calls atomic.Int64
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			throttler.Do(func() {
				calls.Add(1)
			})
		}()
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}

	now = now.Add(time.Second)
	throttler.Do(func() {
		calls.Add(1)
	})

	if got := calls.Load(); got != 2 {
		t.Fatalf("calls after interval = %d, want 2", got)
	}
}
