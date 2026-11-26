package ratebudget

import (
	"context"
	"errors"
	"testing"
	"time"
)

// queued reports how many callers wait on method.
func (l *Limiter) queued(m string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.bucket(m).queue)
}

// enqueue starts a named waiter and returns once it is in the queue, so the
// order callers join in is the order the test chose.
func enqueue(t *testing.T, l *Limiter, ctx context.Context, name string, p Priority, served chan<- string) {
	t.Helper()
	before := l.queued(method)
	go func() {
		if err := l.Wait(ctx, method, p); err == nil {
			served <- name
		}
	}()
	waitUntil(t, name+" is queued", func() bool { return l.queued(method) == before+1 })
}

// spendBurst uses up method's only token.
func spendBurst(t *testing.T, l *Limiter) {
	t.Helper()
	if err := l.Wait(context.Background(), method, Background); err != nil {
		t.Fatalf("spending the burst: %v", err)
	}
}

func assertBlocked(t *testing.T, done <-chan error, why string) {
	t.Helper()
	select {
	case <-done:
		t.Fatal(why)
	default:
	}
}

var (
	onePerSecond = map[string]Limit{method: {PerMinute: 60, Burst: 1}}
	roomy        = map[string]Limit{method: {PerMinute: 60, Burst: 5}}
	slowDefault  = Limit{PerMinute: 1, Burst: 1}
)

// Spec sync-engine, "User request while backfill used up the budget".
func TestInteractiveCallersGoFirstAndEachPriorityIsFIFO(t *testing.T) {
	l, clock := newTestLimiter(t, onePerSecond, slowDefault)
	spendBurst(t, l)

	served := make(chan string, 5)
	ctx := context.Background()
	enqueue(t, l, ctx, "backfill-1", Background, served)
	enqueue(t, l, ctx, "backfill-2", Background, served)
	enqueue(t, l, ctx, "open-1", Interactive, served)
	enqueue(t, l, ctx, "backfill-3", Background, served)
	enqueue(t, l, ctx, "open-2", Interactive, served)

	want := []string{"open-1", "open-2", "backfill-1", "backfill-2", "backfill-3"}
	for i, name := range want {
		clock.Advance(time.Second)
		if got := <-served; got != name {
			t.Fatalf("call %d went to %s, want %s", i, got, name)
		}
	}
}

func TestOneTokenServesOneCaller(t *testing.T) {
	l, clock := newTestLimiter(t, onePerSecond, slowDefault)
	spendBurst(t, l)
	served := make(chan string, 2)
	enqueue(t, l, context.Background(), "a", Background, served)
	enqueue(t, l, context.Background(), "b", Background, served)

	clock.Advance(time.Second)
	<-served
	waitForTimers(t, clock, 1)
	select {
	case name := <-served:
		t.Fatalf("%s passed without a token", name)
	default:
	}
	if n := l.queued(method); n != 1 {
		t.Fatalf("queued = %d, want 1", n)
	}
}

// Spec sync-engine, "Slack answers 429".
func TestPenaltyStopsTheMethodEvenWithTokensLeft(t *testing.T) {
	l, clock := newTestLimiter(t, roomy, slowDefault)
	l.Penalize(method, 30*time.Second)

	done := waitAsync(l, context.Background(), method, Interactive)
	waitForTimers(t, clock, 1)
	clock.Advance(30*time.Second - time.Nanosecond)
	assertBlocked(t, done, "call went out during the penalty")
	clock.Advance(time.Nanosecond)
	if err := <-done; err != nil {
		t.Fatalf("after the penalty: %v", err)
	}
}

func TestPenaltyReachesCallersAlreadyWaitingForAToken(t *testing.T) {
	l, clock := newTestLimiter(t, map[string]Limit{method: {PerMinute: 6, Burst: 1}}, slowDefault)
	spendBurst(t, l)
	done := waitAsync(l, context.Background(), method, Interactive)
	waitForTimers(t, clock, 1) // the token is due in 10 s

	l.Penalize(method, 30*time.Second)
	rearmed := clock.Now().Add(30 * time.Second)
	waitUntil(t, "the waiter rearms for the penalty", func() bool {
		clock.mu.Lock()
		defer clock.mu.Unlock()
		return len(clock.timers) == 1 && clock.timers[0].at.Equal(rearmed)
	})
	clock.Advance(10 * time.Second)
	assertBlocked(t, done, "call went out during the penalty")
	clock.Advance(20 * time.Second)
	if err := <-done; err != nil {
		t.Fatalf("after the penalty: %v", err)
	}
}

func TestShorterPenaltyDoesNotCutALongerOne(t *testing.T) {
	l, clock := newTestLimiter(t, roomy, slowDefault)
	l.Penalize(method, 30*time.Second)
	l.Penalize(method, 5*time.Second)

	done := waitAsync(l, context.Background(), method, Interactive)
	waitForTimers(t, clock, 1)
	clock.Advance(5 * time.Second)
	assertBlocked(t, done, "the shorter penalty replaced the longer one")
	clock.Advance(25 * time.Second)
	if err := <-done; err != nil {
		t.Fatalf("after the penalty: %v", err)
	}
}

func TestPenaltyIsPerMethod(t *testing.T) {
	l, _ := newTestLimiter(t, roomy, Limit{PerMinute: 60, Burst: 5})
	l.Penalize("users.list", time.Minute)
	if err := l.Wait(context.Background(), method, Background); err != nil {
		t.Fatalf("an unpenalized method waited: %v", err)
	}
}

func TestUnknownMethodsUseTheDefaultLimitEachInItsOwnBucket(t *testing.T) {
	l, clock := newTestLimiter(t, roomy, slowDefault)
	for _, m := range []string{"users.list", "emoji.list"} {
		if err := l.Wait(context.Background(), m, Background); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
	}
	done := waitAsync(l, context.Background(), "users.list", Background)
	waitForTimers(t, clock, 1)
	clock.Advance(time.Minute - time.Nanosecond)
	assertBlocked(t, done, "the default limit was not applied")
	clock.Advance(time.Nanosecond)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCancelledWaiterLeavesTheQueueAndSpendsNothing(t *testing.T) {
	l, clock := newTestLimiter(t, onePerSecond, slowDefault)
	spendBurst(t, l)

	served := make(chan string, 1)
	enqueue(t, l, context.Background(), "backfill", Background, served)
	ctx, cancel := context.WithCancel(context.Background())
	done := waitAsync(l, ctx, method, Interactive)
	waitUntil(t, "the interactive caller is queued", func() bool { return l.queued(method) == 2 })
	waitForTimers(t, clock, 1)

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", err)
	}
	clock.Advance(time.Second)
	if got := <-served; got != "backfill" {
		t.Fatalf("served %s", got)
	}
	if l.queued(method) != 0 || clock.active() != 0 {
		t.Fatalf("left behind: %d queued, %d timers", l.queued(method), clock.active())
	}
}

// Wait starts no goroutines, so returning and stopping its timer is all it
// takes not to leak.
func TestCancelledHeadStopsItsTimer(t *testing.T) {
	l, clock := newTestLimiter(t, onePerSecond, slowDefault)
	spendBurst(t, l)
	ctx, cancel := context.WithCancel(context.Background())
	done := waitAsync(l, ctx, method, Background)
	waitForTimers(t, clock, 1)

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", err)
	}
	if clock.active() != 0 || l.queued(method) != 0 {
		t.Fatalf("left behind: %d timers, %d queued", clock.active(), l.queued(method))
	}
}

func TestWaitWithAnEndedContextFailsEvenWithTokens(t *testing.T) {
	l, _ := newTestLimiter(t, roomy, slowDefault)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx, method, Interactive); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", err)
	}
	if l.queued(method) != 0 {
		t.Fatal("an ended caller was queued")
	}
}

func TestNewRejectsLimitsThatCanNeverServeACall(t *testing.T) {
	good := Limit{PerMinute: 1, Burst: 1}
	cases := map[string]struct {
		limits   map[string]Limit
		fallback Limit
	}{
		"zero per minute":  {map[string]Limit{method: {PerMinute: 0, Burst: 1}}, good},
		"zero burst":       {map[string]Limit{method: {PerMinute: 1, Burst: 0}}, good},
		"bad default":      {nil, Limit{PerMinute: 1, Burst: 0}},
		"bad default rate": {nil, Limit{PerMinute: 0, Burst: 1}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(tc.limits, tc.fallback, newFakeClock()); !errors.Is(err, ErrInvalidLimit) {
				t.Fatalf("New = %v, want ErrInvalidLimit", err)
			}
		})
	}
}

func TestNewCopiesTheLimits(t *testing.T) {
	limits := map[string]Limit{method: {PerMinute: 60, Burst: 1}}
	l, _ := newTestLimiter(t, limits, Limit{PerMinute: 60, Burst: 1})
	limits[method] = Limit{PerMinute: 0, Burst: 0}
	if err := l.Wait(context.Background(), method, Background); err != nil {
		t.Fatal(err)
	}
}

func TestSystemClockTimerFiresAtItsInstant(t *testing.T) {
	var c SystemClock
	at := c.Now().Add(time.Millisecond)
	timer := c.NewTimer(at)
	defer timer.Stop()
	if fired := <-timer.C(); fired.Before(at) {
		t.Fatalf("fired at %v, before %v", fired, at)
	}
}

func TestSystemClockTimerStops(t *testing.T) {
	var c SystemClock
	timer := c.NewTimer(c.Now().Add(time.Hour))
	timer.Stop()
	select {
	case <-timer.C():
		t.Fatal("a stopped timer fired")
	default:
	}
}
