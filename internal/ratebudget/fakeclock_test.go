package ratebudget

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// waitUntil yields until cond holds. Goroutines under test block on
// channels, so the test has to let them reach that point before it moves
// the fake clock. It fails after a generous real-time limit instead of
// hanging forever.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		runtime.Gosched()
	}
}

func waitForTimers(t *testing.T, c *fakeClock, n int) {
	t.Helper()
	waitUntil(t, fmt.Sprintf("%d timers are pending", n), func() bool { return c.active() == n })
}

// fakeClock is a Clock that only moves when a test calls Advance, so no
// test ever sleeps.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(at time.Time) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, at: at, c: make(chan time.Time, 1)}
	if !at.After(c.now) {
		t.c <- c.now
		return t
	}
	c.timers = append(c.timers, t)
	return t
}

// Advance moves the clock forward and fires every timer that is now due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	pending := c.timers[:0]
	for _, t := range c.timers {
		if t.at.After(c.now) {
			pending = append(pending, t)
			continue
		}
		t.c <- c.now
	}
	c.timers = pending
}

// active counts the timers that have not fired nor been stopped.
func (c *fakeClock) active() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

type fakeTimer struct {
	clock *fakeClock
	at    time.Time
	c     chan time.Time
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }

func (t *fakeTimer) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	for i, other := range t.clock.timers {
		if other == t {
			t.clock.timers = append(t.clock.timers[:i], t.clock.timers[i+1:]...)
			return
		}
	}
}
