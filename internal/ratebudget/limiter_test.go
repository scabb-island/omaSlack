package ratebudget

import (
	"context"
	"testing"
	"time"
)

const method = "conversations.history"

func newTestLimiter(t *testing.T, limits map[string]Limit, def Limit) (*Limiter, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	l, err := New(limits, def, clock)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l, clock
}

// waitAsync runs Wait in a goroutine and reports its result on the channel.
func waitAsync(l *Limiter, ctx context.Context, m string, p Priority) <-chan error {
	done := make(chan error, 1)
	go func() { done <- l.Wait(ctx, m, p) }()
	return done
}

func TestBurstIsAvailableAtOnceThenOneTokenPerInterval(t *testing.T) {
	l, clock := newTestLimiter(t, map[string]Limit{method: {PerMinute: 6, Burst: 2}}, Limit{PerMinute: 60, Burst: 1})

	for i := range 2 {
		if err := l.Wait(context.Background(), method, Background); err != nil {
			t.Fatalf("call %d of the burst: %v", i, err)
		}
	}
	done := waitAsync(l, context.Background(), method, Background)
	waitForTimers(t, clock, 1)

	clock.Advance(10*time.Second - time.Nanosecond)
	select {
	case <-done:
		t.Fatal("third call passed before a token was due")
	default:
	}
	clock.Advance(time.Nanosecond)
	if err := <-done; err != nil {
		t.Fatalf("third call: %v", err)
	}
}
