package ratebudget

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

// ErrInvalidLimit reports a Limit that could never hand out a call.
var ErrInvalidLimit = errors.New("ratebudget: PerMinute and Burst must be at least 1")

// Limiter is a Budget with one token bucket per Slack API method. Methods
// missing from its limits share the default Limit, each in its own bucket.
type Limiter struct {
	clock    Clock
	limits   map[string]Limit
	fallback Limit

	mu      sync.Mutex
	buckets map[string]*bucket
}

var _ Budget = (*Limiter)(nil)

// New builds a Limiter. limits maps a method to its budget; fallback is the
// budget of every other method.
func New(limits map[string]Limit, fallback Limit, clock Clock) (*Limiter, error) {
	for name, lim := range limits {
		if !lim.valid() {
			return nil, fmt.Errorf("%w (method %s)", ErrInvalidLimit, name)
		}
	}
	if !fallback.valid() {
		return nil, fmt.Errorf("%w (default)", ErrInvalidLimit)
	}
	return &Limiter{
		clock:    clock,
		limits:   maps.Clone(limits),
		fallback: fallback,
		buckets:  make(map[string]*bucket),
	}, nil
}

func (l Limit) valid() bool { return l.PerMinute >= 1 && l.Burst >= 1 }

// Wait queues the caller behind every waiter of the same method with the
// same or higher priority, and returns once it is at the head of the queue
// and the method has a token and no penalty. It returns ctx's error if ctx
// ends first; the caller then leaves the queue and spends nothing.
func (l *Limiter) Wait(ctx context.Context, method string, p Priority) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w := &waiter{priority: p}
	b := l.join(method, w)
	for {
		changed, timer, granted := l.try(b, w)
		if granted {
			return nil
		}
		if err := sleep(ctx, changed, timer); err != nil {
			l.leave(b, w)
			return err
		}
	}
}

// Penalize stops method until retryAfter from now. A shorter penalty never
// cuts a longer one short.
func (l *Limiter) Penalize(method string, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucket(method)
	until := l.clock.Now().Add(retryAfter)
	if until.After(b.blockedUntil) {
		b.blockedUntil = until
		b.notify()
	}
}

func (l *Limiter) join(method string, w *waiter) *bucket {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucket(method)
	b.enqueue(w)
	return b
}

func (l *Limiter) leave(b *bucket, w *waiter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b.remove(w)
}

// try grants w its call if it can go now. Otherwise it returns what w must
// wait for: a change in the queue or penalty, and, at the head of the
// queue, a timer for the instant the next call is due.
func (l *Limiter) try(b *bucket, w *waiter) (<-chan struct{}, Timer, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b.queue[0] != w {
		return b.changed, nil, false
	}
	now := l.clock.Now()
	at := b.readyAt()
	if at.After(now) {
		return b.changed, l.clock.NewTimer(at), false
	}
	b.take(now)
	b.remove(w)
	return nil, nil, true
}

func sleep(ctx context.Context, changed <-chan struct{}, timer Timer) error {
	var fired <-chan time.Time
	if timer != nil {
		defer timer.Stop()
		fired = timer.C()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-changed:
	case <-fired:
	}
	return nil
}

// bucket must be called with l.mu held.
func (l *Limiter) bucket(method string) *bucket {
	b, ok := l.buckets[method]
	if !ok {
		lim, known := l.limits[method]
		if !known {
			lim = l.fallback
		}
		b = newBucket(lim)
		l.buckets[method] = b
	}
	return b
}

type waiter struct{ priority Priority }

// bucket is one method's budget, kept as a GCRA (a token bucket expressed
// as the theoretical arrival time of the next call), plus its penalty and
// its queue of waiters. Every field is guarded by Limiter.mu.
type bucket struct {
	interval  time.Duration // time to earn one token
	tolerance time.Duration // how far ahead of schedule a burst may run
	tat       time.Time     // theoretical arrival time of the next call

	blockedUntil time.Time
	queue        []*waiter     // Interactive first, FIFO within a priority
	changed      chan struct{} // closed and replaced on every change
}

func newBucket(lim Limit) *bucket {
	interval := time.Minute / time.Duration(lim.PerMinute)
	return &bucket{
		interval:  interval,
		tolerance: interval * time.Duration(lim.Burst-1),
		changed:   make(chan struct{}),
	}
}

// readyAt is the earliest instant the next call may go out.
func (b *bucket) readyAt() time.Time {
	at := b.tat.Add(-b.tolerance)
	if b.blockedUntil.After(at) {
		return b.blockedUntil
	}
	return at
}

func (b *bucket) take(now time.Time) {
	b.tat = later(b.tat, now).Add(b.interval)
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (b *bucket) enqueue(w *waiter) {
	i := len(b.queue)
	for i > 0 && b.queue[i-1].priority < w.priority {
		i--
	}
	b.queue = slices.Insert(b.queue, i, w)
	b.notify()
}

func (b *bucket) remove(w *waiter) {
	b.queue = slices.DeleteFunc(b.queue, func(o *waiter) bool { return o == w })
	b.notify()
}

func (b *bucket) notify() {
	close(b.changed)
	b.changed = make(chan struct{})
}
