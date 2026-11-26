package syncengine_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

func TestAnEventIsStoredBeforeItIsPublished(t *testing.T) {
	r := newRig(t)
	live := make(chan domain.Event)
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: live})
	r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected))

	live <- created(convA, ts1)
	r.pub.expect(t, created(convA, ts1))
	if got := r.stored(t, convA); !slices.Equal(got, []domain.TS{ts1}) {
		t.Fatalf("store = %v when the event was published", got)
	}
}

func TestLiveOnlyEventsArePublished(t *testing.T) {
	r := newRig(t)
	user := must(domain.ParseUserID("U01"))
	typing := domain.UserTyping{Team: team, Conversation: convA, User: user}
	presence := domain.PresenceChanged{Team: team, User: user, Presence: domain.Active}
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected), typing, presence}, Live: make(chan domain.Event)})
	r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected), typing, presence)
}

func TestReplayedEventsLeaveOneCopy(t *testing.T) {
	r := newRig(t)
	edit := domain.MessageUpdated{Team: team, Conversation: convA, Message: domain.Message{TS: ts1, Text: "edited", Edited: ts2}}
	session := []domain.Event{state(domain.Connected), created(convA, ts1), edit, created(convA, ts1)}
	r.fake.Script(
		providertest.Session{Events: session},
		providertest.Session{Events: session, Live: make(chan domain.Event)},
	)
	r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected), created(convA, ts1), edit, created(convA, ts1), state(domain.Disconnected))
	r.clock.fire(t, time.Second)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected), created(convA, ts1), edit, created(convA, ts1))

	msgs, err := r.store.Messages(context.Background(), convA, zeroTS, 10)
	if err != nil || len(msgs) != 1 || msgs[0].Text != "edited" {
		t.Fatalf("store = %+v, %v", msgs, err)
	}
}

func TestReconnectsWithGrowingBackoffAndReportsEveryTransition(t *testing.T) {
	r := newRig(t)
	boom := errors.New("network down")
	r.fake.Script(
		providertest.Session{Err: boom},
		providertest.Session{Err: boom},
		providertest.Session{Err: boom},
		providertest.Session{Events: []domain.Event{state(domain.Connected)}}, // up, then dropped
		providertest.Session{Err: boom},
	)
	r.start(t)

	r.pub.expect(t, state(domain.Connecting), state(domain.Disconnected))
	r.clock.fire(t, 1*time.Second) // attempt 0: ceiling 1 s
	r.pub.expect(t, state(domain.Connecting), state(domain.Disconnected))
	r.clock.fire(t, 1500*time.Millisecond) // attempt 1: 1 s + half of 1 s
	r.pub.expect(t, state(domain.Connecting), state(domain.Disconnected))
	r.clock.fire(t, 3*time.Second) // attempt 2: 2 s + half of 2 s
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected), state(domain.Disconnected))
	r.clock.fire(t, 1*time.Second) // a connection that worked starts over
	r.pub.expect(t, state(domain.Connecting), state(domain.Disconnected))
	r.clock.fire(t, 1500*time.Millisecond)
	r.pub.expect(t, state(domain.Connecting)) // the idle session never says it is up
	r.pub.expectNothing(t)
}

func TestTheStreamsOwnStatesAreNotRepeated(t *testing.T) {
	r := newRig(t)
	r.fake.Script(providertest.Session{Events: []domain.Event{
		state(domain.Connecting), state(domain.Connected), state(domain.Connected),
		state(domain.Disconnected), state(domain.Connecting), state(domain.Connected),
	}, Live: make(chan domain.Event)})
	r.start(t)
	r.pub.expect(t,
		state(domain.Connecting), state(domain.Connected),
		state(domain.Disconnected), state(domain.Connecting), state(domain.Connected))
	r.pub.expectNothing(t)
}

func TestRejectedCredentialsOnConnectStopTheEngine(t *testing.T) {
	r := newRig(t)
	r.fake.Script(providertest.Session{Err: provider.ErrAuthExpired})
	_, result := r.start(t)

	if err := result(); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("Run = %v, want ErrAuthExpired", err)
	}
	r.pub.expect(t, state(domain.Connecting), state(domain.AuthExpired))
	r.pub.expectNothing(t)
	r.clock.expectNoTimer(t)
	if n := len(r.fake.Calls()); n != 1 {
		t.Fatalf("%d calls, want one Connect", n)
	}
}

func TestAStreamEndingInAuthExpiredStopsTheEngine(t *testing.T) {
	r := newRig(t)
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected), state(domain.AuthExpired)}})
	_, result := r.start(t)

	if err := result(); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("Run = %v, want ErrAuthExpired", err)
	}
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected), state(domain.AuthExpired))
	r.clock.expectNoTimer(t)
}

func TestARateLimitedConnectWaitsOutItsPenaltyUnderTheBudget(t *testing.T) {
	r := newRig(t)
	r.fake.Script(
		providertest.Session{Err: &provider.RateLimitedError{Method: "rtm.connect", RetryAfter: 30 * time.Second}},
		providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: make(chan domain.Event)},
	)
	r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.RateLimited), state(domain.Connecting), state(domain.Connected))
	if got := r.budget.penalized(); !slices.Equal(got, []penalty{{"connect", 30 * time.Second}}) {
		t.Fatalf("penalties = %v", got)
	}
	want := []grant{{"connect", ratebudget.Background}, {"connect", ratebudget.Background}}
	if got := r.budget.grants(); !slices.Equal(got, want) {
		t.Fatalf("budget waits = %v", got)
	}
	r.clock.expectNoTimer(t) // the budget waits, not the backoff
}

func TestNothingIsProcessedOnceCtxEnds(t *testing.T) {
	stream := make(chan domain.Event, 2)
	r := newRigWith(t, func(f *providertest.Fake) provider.Provider { return fixedStream{f, stream} })
	cancel, result := r.start(t)
	r.pub.expect(t, state(domain.Connecting))
	stream <- state(domain.Connected)
	r.pub.expect(t, state(domain.Connected))

	cancel()
	stream <- created(convA, ts1)
	if err := result(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	r.pub.expectNothing(t)
	if got := r.stored(t, convA); len(got) != 0 {
		t.Fatalf("stored %v after ctx ended", got)
	}
}

func TestRunWithAnEndedCtxDoesNothing(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.engine.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	r.pub.expectNothing(t)
	if calls := r.fake.Calls(); len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
}

// fixedStream connects to a stream the test feeds and that ignores ctx, so
// the test controls exactly when an event becomes readable.
type fixedStream struct {
	*providertest.Fake
	stream chan domain.Event
}

func (f fixedStream) Connect(context.Context) (<-chan domain.Event, error) { return f.stream, nil }
