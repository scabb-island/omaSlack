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
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
)

var bg = context.Background()

// lastGrant is the most recent permission the engine asked the budget for.
func (r *rig) lastGrant(t *testing.T) grant {
	t.Helper()
	g := r.budget.grants()
	if len(g) == 0 {
		t.Fatal("no budget waits")
	}
	return g[len(g)-1]
}

// callsAfterConnect drops the Connect call every connected rig starts with.
func (r *rig) callsAfterConnect() []providertest.Call { return r.fake.Calls()[1:] }

func TestOpeningAnUncachedConversationFetchesTheNewestPage(t *testing.T) {
	r := newRig(t)
	r.fake.SetHistory(convA, msg(ts1), msg(ts2), msg(ts3), msg(ts4))
	r.connected(t)

	if err := r.engine.OpenConversation(bg, convA); err != nil {
		t.Fatal(err)
	}
	if got := r.stored(t, convA); !slices.Equal(got, []domain.TS{ts4, ts3, ts2}) {
		t.Fatalf("store = %v, want the newest page", got)
	}
	if got := r.callsAfterConnect(); !slices.Equal(got, []providertest.Call{{Method: providertest.MethodHistory, Conv: convA}}) {
		t.Fatalf("calls = %v", got)
	}
	if g := r.lastGrant(t); g != (grant{"conversations.history", ratebudget.Interactive}) {
		t.Fatalf("budget wait = %v", g)
	}
	r.pub.expectNothing(t) // a first backfill is not news
}

func TestOpeningACachedConversationCatchesUpAndPublishesWhatItLacked(t *testing.T) {
	r := newRig(t)
	r.cache(t, convA, msg(ts1)) // left by the previous session
	r.fake.SetHistory(convA, msg(ts1), msg(ts2), msg(ts3), msg(ts4))
	r.connected(t)

	if err := r.engine.OpenConversation(bg, convA); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, created(convA, ts2), created(convA, ts3), created(convA, ts4))
	r.pub.expectNothing(t)
	if got := r.stored(t, convA); !slices.Equal(got, []domain.TS{ts4, ts3, ts2, ts1}) {
		t.Fatalf("store = %v", got)
	}
	want := []providertest.Call{ // a full page that did not reach ts1 asks for the next one
		{Method: providertest.MethodHistory, Conv: convA},
		{Method: providertest.MethodHistory, Conv: convA, TS: ts2},
	}
	if got := r.callsAfterConnect(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v", got)
	}
}

func TestOpeningAConversationSlackHasNothingInStoresNothing(t *testing.T) {
	r := newRig(t)
	r.connected(t)
	if err := r.engine.OpenConversation(bg, convA); err != nil {
		t.Fatal(err)
	}
	if got := r.stored(t, convA); len(got) != 0 {
		t.Fatalf("store = %v", got)
	}
}

func TestLoadOlderContinuesFromTheOldestStoredMessage(t *testing.T) {
	r := newRig(t)
	r.cache(t, convA, msg(ts4), msg(ts3))
	r.fake.SetHistory(convA, msg(ts1), msg(ts2), msg(ts3), msg(ts4))
	r.connected(t)

	n, err := r.engine.LoadOlder(bg, convA)
	if err != nil || n != 2 {
		t.Fatalf("LoadOlder = %d, %v; want 2", n, err)
	}
	if got := r.stored(t, convA); !slices.Equal(got, []domain.TS{ts4, ts3, ts2, ts1}) {
		t.Fatalf("store = %v", got)
	}
	if got := r.callsAfterConnect(); !slices.Equal(got, []providertest.Call{{Method: providertest.MethodHistory, Conv: convA, TS: ts3}}) {
		t.Fatalf("calls = %v", got)
	}
	if g := r.lastGrant(t); g != (grant{"conversations.history", ratebudget.Interactive}) {
		t.Fatalf("budget wait = %v", g)
	}
	r.pub.expectNothing(t)
}

func TestLoadOlderOnAnEmptyStoreFetchesTheNewestPage(t *testing.T) {
	r := newRig(t)
	r.fake.SetHistory(convA, msg(ts1), msg(ts2), msg(ts3), msg(ts4))
	r.connected(t)

	n, err := r.engine.LoadOlder(bg, convA)
	if err != nil || n != 3 {
		t.Fatalf("LoadOlder = %d, %v; want a full page", n, err)
	}
	if got := r.stored(t, convA); !slices.Equal(got, []domain.TS{ts4, ts3, ts2}) {
		t.Fatalf("store = %v, want the newest page", got)
	}
	if got := r.callsAfterConnect(); !slices.Equal(got, []providertest.Call{{Method: providertest.MethodHistory, Conv: convA}}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestLoadOlderFailsWhenTheStoreCannotSayWhereToContinue(t *testing.T) {
	boom := errors.New("disk on fire")
	r := newRigFrom(t, nil, func(s store.Store) store.Store { return oldestFails{s, boom} })
	r.connected(t)

	if n, err := r.engine.LoadOlder(bg, convA); !errors.Is(err, boom) || n != 0 {
		t.Fatalf("LoadOlder = %d, %v", n, err)
	}
	if got := r.callsAfterConnect(); len(got) != 0 {
		t.Fatalf("calls = %v, want none", got)
	}
}

// oldestFails is a store whose Oldest always fails.
type oldestFails struct {
	store.Store
	err error
}

func (s oldestFails) Oldest(context.Context, domain.ConversationID) (domain.TS, error) {
	return domain.TS{}, s.err
}

func TestOpeningAThreadStoresItsReplies(t *testing.T) {
	r := newRig(t)
	replies := []domain.Message{{TS: ts2, ThreadTS: ts1, Text: "a"}, {TS: ts3, ThreadTS: ts1, Text: "b"}}
	r.fake.SetReplies(convA, ts1, replies...)
	r.connected(t)

	if err := r.engine.OpenThread(bg, convA, ts1); err != nil {
		t.Fatal(err)
	}
	got, err := r.store.Replies(bg, convA, ts1)
	if err != nil || !slices.Equal(got, replies) {
		t.Fatalf("stored replies = %v, %v", got, err)
	}
	if g := r.lastGrant(t); g != (grant{"conversations.replies", ratebudget.Interactive}) {
		t.Fatalf("budget wait = %v", g)
	}
	r.pub.expectNothing(t)
}

func TestProviderErrorsReachTheCaller(t *testing.T) {
	r := newRig(t)
	r.connected(t)
	boom := errors.New("channel_not_found")
	r.fake.Fail(providertest.MethodReplies, boom)
	r.fake.Fail(providertest.MethodHistory, boom, boom)

	if err := r.engine.OpenThread(bg, convA, ts1); !errors.Is(err, boom) {
		t.Fatalf("OpenThread = %v", err)
	}
	if err := r.engine.OpenConversation(bg, convA); !errors.Is(err, boom) {
		t.Fatalf("OpenConversation = %v", err)
	}
	if n, err := r.engine.LoadOlder(bg, convA); !errors.Is(err, boom) || n != 0 {
		t.Fatalf("LoadOlder = %d, %v", n, err)
	}
	r.pub.expectNothing(t)
}

func TestARateLimitedBackfillShowsRateLimitedUntilItGetsThrough(t *testing.T) {
	r := newRig(t)
	r.fake.SetHistory(convA, msg(ts1))
	r.connected(t)
	limited := &provider.RateLimitedError{Method: "conversations.history", RetryAfter: 30 * time.Second}
	r.fake.Fail(providertest.MethodHistory, limited, limited)

	if _, err := r.engine.LoadOlder(bg, convA); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, state(domain.RateLimited), state(domain.Connected))
	r.pub.expectNothing(t)
	want := []penalty{{"conversations.history", 30 * time.Second}, {"conversations.history", 30 * time.Second}}
	if got := r.budget.penalized(); !slices.Equal(got, want) {
		t.Fatalf("penalties = %v", got)
	}
	if got := r.budget.grants(); len(got) != 4 { // connect + three tries
		t.Fatalf("budget waits = %v", got)
	}
}

func TestRejectedCredentialsDuringBackfillStopTheEngine(t *testing.T) {
	r := newRig(t)
	_, result := r.connected(t)
	r.fake.Fail(providertest.MethodHistory, provider.ErrAuthExpired)

	if _, err := r.engine.LoadOlder(bg, convA); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("LoadOlder = %v", err)
	}
	r.pub.expect(t, state(domain.AuthExpired))
	if err := result(); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("Run = %v", err)
	}
}

func TestBackfillFailsOnceRunHasStopped(t *testing.T) {
	r := newRig(t)
	cancel, result := r.connected(t)
	cancel()
	_ = result()

	if err := r.engine.OpenConversation(bg, convA); !errors.Is(err, syncengine.ErrStopped) {
		t.Fatalf("OpenConversation = %v", err)
	}
	if err := r.engine.CloseConversation(bg, convA); !errors.Is(err, syncengine.ErrStopped) {
		t.Fatalf("CloseConversation = %v", err)
	}
}

func TestBackfillGivesUpWhenItsCallerDoes(t *testing.T) {
	r := newRig(t) // never started: nothing serves the store's writes
	r.fake.SetHistory(convA, msg(ts1))
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := r.engine.LoadOlder(ctx, convA); !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadOlder = %v", err)
	}
}
