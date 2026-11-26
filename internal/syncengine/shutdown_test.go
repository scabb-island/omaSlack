package syncengine_test

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

func TestASessionThatNeverCameUpDoesNotResetTheBackoff(t *testing.T) {
	r := newRig(t)
	r.fake.Script(
		providertest.Session{Events: []domain.Event{created(convA, ts1)}},      // events, but never "connected"
		providertest.Session{Events: []domain.Event{state(domain.Connecting)}}, // a state that is not "connected"
	)
	r.start(t)

	r.pub.expect(t, state(domain.Connecting), created(convA, ts1), state(domain.Disconnected))
	r.clock.fire(t, 1*time.Second)
	r.pub.expect(t, state(domain.Connecting), state(domain.Disconnected))
	r.clock.fire(t, 1500*time.Millisecond) // still growing
	r.pub.expect(t, state(domain.Connecting))
}

func TestShuttingDownWhileConnectingIsNotAFailure(t *testing.T) {
	r := newRig(t)
	r.budget.holdOn("connect")
	cancel, result := r.start(t)
	r.budget.awaitHeld(t)

	cancel()
	if err := result(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	if logs := r.logs.String(); logs != "" {
		t.Fatalf("logged %q", logs)
	}
}

func TestShuttingDownStopsAReconcileHalfway(t *testing.T) {
	r := newRig(t)
	live, result := r.firstSession(t, created(convA, ts1), created(convB, ts1))
	r.budget.holdOn("conversations.history")

	r.dropAndReconnect(t, live)
	r.budget.awaitHeld(t) // catching up convA
	r.stop()
	_ = result()
	history := slices.DeleteFunc(r.budget.grants(), func(g grant) bool { return g.method != "conversations.history" })
	if len(history) != 1 {
		t.Fatalf("History budget waits = %v, want convA's only", history)
	}
	if logs := r.logs.String(); logs != "" {
		t.Fatalf("logged %q", logs)
	}
}

// countingStore counts Messages reads.
type countingStore struct {
	store.Store
	reads atomic.Int32
}

func (s *countingStore) Messages(ctx context.Context, c domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error) {
	s.reads.Add(1)
	return s.Store.Messages(ctx, c, before, limit)
}

func TestCatchingUpReadsTheStoreOnlyAsFarAsSlacksPages(t *testing.T) {
	counter := &countingStore{}
	r := newRigFrom(t, nil, func(s store.Store) store.Store { counter.Store = s; return counter })
	var cached []domain.Message
	for i := range 30 { // ten store pages
		cached = append(cached, msg(tsAt(i)))
	}
	r.cache(t, convA, cached...)
	r.fake.SetHistory(convA, append(cached, msg(tsAt(30)))...)
	r.connected(t)

	if err := r.engine.OpenConversation(bg, convA); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, created(convA, tsAt(30)))
	if n := counter.reads.Load(); n > 2 {
		t.Fatalf("%d store reads to catch up one message, want at most 2", n)
	}
}
