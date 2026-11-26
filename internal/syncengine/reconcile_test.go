package syncengine_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

// firstSession starts the engine with a session that comes up, delivers
// events and stays open until the test closes live. result is Run's.
func (r *rig) firstSession(t *testing.T, events ...domain.Event) (live chan domain.Event, result func() error) {
	t.Helper()
	live = make(chan domain.Event)
	r.fake.Script(providertest.Session{Events: append([]domain.Event{state(domain.Connected)}, events...), Live: live})
	_, result = r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected))
	r.pub.expect(t, events...)
	return live, result
}

// dropAndReconnect ends the current session, fires the 1 s backoff and
// lets the next session come up and stay open.
func (r *rig) dropAndReconnect(t *testing.T, live chan domain.Event) {
	t.Helper()
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: make(chan domain.Event)})
	close(live)
	r.pub.expect(t, state(domain.Disconnected))
	r.clock.fire(t, time.Second)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected))
}

// historyCalls are the History calls the fake received, in order.
func (r *rig) historyCalls() []providertest.Call {
	return slices.DeleteFunc(r.fake.Calls(), func(c providertest.Call) bool {
		return c.Method != providertest.MethodHistory
	})
}

func TestMessagesThatArrivedDuringAnOutageArePublishedAfterReconnecting(t *testing.T) {
	r := newRig(t)
	live, _ := r.firstSession(t, created(dm, ts1))
	r.fake.SetHistory(dm, msg(ts1), msg(ts2), msg(ts3), msg(ts4)) // three DMs while away

	r.dropAndReconnect(t, live)
	r.pub.expect(t, created(dm, ts2), created(dm, ts3), created(dm, ts4))
	if got := r.stored(t, dm); !slices.Equal(got, []domain.TS{ts4, ts3, ts2, ts1}) {
		t.Fatalf("store = %v", got)
	}
	if g := r.lastGrant(t); g != (grant{"conversations.history", ratebudget.Background}) {
		t.Fatalf("budget wait = %v, want background", g)
	}
}

func TestTheFirstConnectionHasNothingToCatchUp(t *testing.T) {
	r := newRig(t)
	r.firstSession(t, created(convA, ts1))
	r.pub.expectNothing(t)
	if got := r.historyCalls(); len(got) != 0 {
		t.Fatalf("History calls = %v", got)
	}
}

func TestOpenConversationsAreCaughtUpUntilEveryClientClosesThem(t *testing.T) {
	r := newRig(t)
	live, _ := r.firstSession(t)
	all := []domain.ConversationID{convA, convB, dm}
	for _, c := range all {
		r.fake.SetHistory(c, msg(ts1))
	}
	for _, c := range []domain.ConversationID{convA, convB, convB, dm} {
		if err := r.engine.OpenConversation(bg, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []domain.ConversationID{convA, convB} {
		if err := r.engine.CloseConversation(bg, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range all {
		r.fake.SetHistory(c, msg(ts1), msg(ts2))
	}
	seen := len(r.historyCalls())

	r.dropAndReconnect(t, live)
	// convB is still open in one client; convA is open in none.
	r.pub.expect(t, created(convB, ts2), created(dm, ts2))
	want := []providertest.Call{{Method: providertest.MethodHistory, Conv: convB}, {Method: providertest.MethodHistory, Conv: dm}}
	if got := r.historyCalls()[seen:]; !slices.Equal(got, want) {
		t.Fatalf("History calls after reconnect = %v, want %v", got, want)
	}
}

func TestAConversationThatCannotBeFetchedDoesNotStopTheOthers(t *testing.T) {
	r := newRig(t)
	live, _ := r.firstSession(t, created(convA, ts1), created(convB, ts1))
	r.fake.SetHistory(convB, msg(ts1), msg(ts2))
	r.fake.Fail(providertest.MethodHistory, errors.New("channel_not_found"))

	r.dropAndReconnect(t, live)
	r.pub.expect(t, created(convB, ts2))
}

func TestRejectedCredentialsDuringReconcileStopTheEngine(t *testing.T) {
	r := newRig(t)
	live, result := r.firstSession(t, created(convA, ts1), created(convB, ts1))
	r.fake.Fail(providertest.MethodHistory, provider.ErrAuthExpired)

	r.dropAndReconnect(t, live)
	r.pub.expect(t, state(domain.AuthExpired))
	if err := result(); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("Run = %v", err)
	}
	if got := r.historyCalls(); len(got) != 1 {
		t.Fatalf("History calls = %v, want only the rejected one", got)
	}
}
