package provider

import (
	"context"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func TestEmitHandsTheEventOver(t *testing.T) {
	out := make(chan domain.Event, 1)
	ev := domain.ConnectionChanged{State: domain.Connected}

	Emit(context.Background(), out, ev)

	if got := <-out; got != ev {
		t.Fatalf("got %v, want %v", got, ev)
	}
}

// Once ctx has ended, Emit must not hand over another event, even when a
// consumer is ready: a select with both cases ready picks one at random,
// so this runs many times.
func TestEmitDropsEventsOnceTheContextHasEnded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan domain.Event, 1)
	for range 100 {
		Emit(ctx, out, domain.ConnectionChanged{})
		if len(out) != 0 {
			t.Fatal("Emit delivered an event after ctx ended")
		}
	}
}

func TestEmitDoesNotBlockWhenTheContextEndsWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Emit(ctx, make(chan domain.Event), domain.ConnectionChanged{})
		close(done)
	}()

	cancel()

	<-done
}
