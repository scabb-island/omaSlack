package syncengine_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

// The signed-in user's own Do Not Disturb and presence (task 5.3).

var (
	snoozeEnd = time.Unix(1700003600, 0)
	other     = must(domain.ParseUserID("U0BO"))
)

func dndAt(until time.Time) domain.Event { return domain.DNDChanged{Team: team, Until: until} }

func presenceOf(u domain.UserID, p domain.Presence) domain.Event {
	return domain.PresenceChanged{Team: team, User: u, Presence: p}
}

func (r *rig) expectDND(t *testing.T, want time.Time) {
	t.Helper()
	got, err := r.engine.DND(bg)
	if err != nil || !got.Equal(want) {
		t.Fatalf("DND = %v, %v; want %v", got, err, want)
	}
}

func (r *rig) expectPresence(t *testing.T, want domain.Presence) {
	t.Helper()
	got, err := r.engine.Presence(bg)
	if err != nil || got != want {
		t.Fatalf("Presence = %v, %v; want %v", got, err, want)
	}
}

func count(calls []string, call string) int {
	n := 0
	for _, c := range calls {
		if c == call {
			n++
		}
	}
	return n
}

func TestEveryConnectionReadsTheSnoozeAndPublishesIt(t *testing.T) {
	r, w := newWritesRig(t)
	w.dnd = snoozeEnd
	live, _ := r.firstSession(t)

	r.pub.expect(t, dndAt(snoozeEnd))
	if !slices.Contains(r.budget.grants(), grant{"dnd.info", ratebudget.Background}) {
		t.Fatalf("grants = %v, want dnd.info at background", r.budget.grants())
	}
	r.expectDND(t, snoozeEnd)
	if n := w.dndInfoCalls(); n != 1 {
		t.Fatalf("dnd.info called %d times, want once: DND answers what the engine knows", n)
	}

	w.mu.Lock()
	w.dnd = time.Time{} // ended in another client while omaslack was offline
	w.mu.Unlock()
	r.dropAndReconnect(t, live)
	r.pub.expect(t, dndAt(time.Time{}))
	r.expectDND(t, time.Time{})
}

func TestAnUnchangedSnoozeIsNotPublishedAgain(t *testing.T) {
	r, w := newWritesRig(t)
	live, _ := r.firstSession(t) // off, as clients assume until told
	r.dropAndReconnect(t, live)
	r.awaitListed(t, 2)
	r.pub.expectNothing(t)
	if n := w.dndInfoCalls(); n != 2 {
		t.Fatalf("dnd.info called %d times, want once per connection", n)
	}
}

func TestDNDAsksSlackWhenTheConnectFailedToLearnIt(t *testing.T) {
	r, w := newWritesRig(t)
	w.failNext("dnd.info", errors.New("internal_error"))
	r.connected(t)
	w.dnd = snoozeEnd

	r.expectDND(t, snoozeEnd)
	r.expectGrant(t, "dnd.info")
	r.pub.expect(t, dndAt(snoozeEnd))
	r.expectDND(t, snoozeEnd)
	if n := w.dndInfoCalls(); n != 2 {
		t.Fatalf("dnd.info called %d times, want 2", n)
	}
}

func TestSnoozingAndEndingPublishTheNewState(t *testing.T) {
	r, w := newWritesRig(t)
	w.snoozeEnd = snoozeEnd
	r.connected(t)

	if err := r.engine.SnoozeDND(bg, 60); err != nil {
		t.Fatal(err)
	}
	r.expectGrant(t, "dnd.setSnooze")
	r.pub.expect(t, dndAt(snoozeEnd))
	r.expectDND(t, snoozeEnd)
	if err := r.engine.EndDND(bg); err != nil {
		t.Fatal(err)
	}
	r.expectGrant(t, "dnd.endSnooze")
	r.pub.expect(t, dndAt(time.Time{}))
	r.expectDND(t, time.Time{})
	if got := w.recorded(); !slices.Equal(got, []string{"dnd.setSnooze 60\n", "dnd.endSnooze\n"}) {
		t.Fatalf("calls = %q", got)
	}
}

func TestAFailedSnoozeChangesNothing(t *testing.T) {
	r, w := newWritesRig(t)
	r.connected(t)
	w.failNext("dnd.setSnooze", errors.New("snooze_failed"))
	if err := r.engine.SnoozeDND(bg, 60); err == nil {
		t.Fatal("SnoozeDND = nil, want Slack's error")
	}
	r.pub.expectNothing(t)
	r.expectDND(t, time.Time{})
}

func TestTheStreamsDNDIsRemembered(t *testing.T) {
	r, _ := newWritesRig(t)
	live, _ := r.firstSession(t)
	live <- dndAt(snoozeEnd)
	r.pub.expect(t, dndAt(snoozeEnd))
	r.expectDND(t, snoozeEnd)
}

func TestSettingPresencePublishesTheUsersOwn(t *testing.T) {
	r, w := newWritesRig(t, domain.PresenceWrite)
	r.connected(t)

	if err := r.engine.SetPresence(bg, true); err != nil {
		t.Fatal(err)
	}
	r.expectGrant(t, "users.setPresence")
	r.pub.expect(t, presenceOf(me, domain.Away))
	r.expectPresence(t, domain.Away)
	if err := r.engine.SetPresence(bg, false); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, presenceOf(me, domain.Active))
	r.expectPresence(t, domain.Active)
	if n := count(w.recorded(), "users.getPresence\n"); n != 0 {
		t.Fatalf("users.getPresence called %d times, want none: the engine knew", n)
	}
}

func TestPresenceAsksSlackOnceAndRemembers(t *testing.T) {
	r, w := newWritesRig(t)
	w.presence = domain.Away
	r.connected(t)

	r.expectPresence(t, domain.Away)
	r.expectGrant(t, "users.getPresence")
	r.pub.expect(t, presenceOf(me, domain.Away))
	r.expectPresence(t, domain.Away)
	if n := count(w.recorded(), "users.getPresence\n"); n != 1 {
		t.Fatalf("users.getPresence called %d times, want once", n)
	}
}

func TestTheStreamsOwnPresenceNamesTheUser(t *testing.T) {
	r, w := newWritesRig(t)
	live, _ := r.firstSession(t)
	live <- domain.PresenceChanged{Team: team, Presence: domain.Away} // manual_presence_change
	r.pub.expect(t, presenceOf(me, domain.Away))
	r.expectPresence(t, domain.Away)
	live <- presenceOf(me, domain.Away) // the same, as presence_change
	live <- presenceOf(other, domain.Away)
	r.pub.expect(t, presenceOf(other, domain.Away))
	if n := count(w.recorded(), "users.getPresence\n"); n != 0 {
		t.Fatalf("users.getPresence called %d times, want none", n)
	}
}

func TestOwnPresenceWithoutASignedInUserIsDropped(t *testing.T) {
	r := newRigWithSelf(t, domain.UserID{})
	live, _ := r.firstSession(t)
	live <- domain.PresenceChanged{Team: team, Presence: domain.Away}
	live <- created(convA, ts1)
	r.pub.expect(t, created(convA, ts1))
}

func TestOwnStateNeedsTheProvidersInterfaces(t *testing.T) {
	r := newBareRig(t)
	r.connected(t)
	if _, err := r.engine.DND(bg); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("DND = %v, want unsupported", err)
	}
	if _, err := r.engine.Presence(bg); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("Presence = %v, want unsupported", err)
	}
}

// subscriber records the presence subscriptions the engine asks for.
type subscriber struct {
	*providertest.Fake
	mu   sync.Mutex
	subs [][]domain.UserID
}

var _ provider.PresenceSubscriber = (*subscriber)(nil)

func (s *subscriber) SubscribePresence(_ context.Context, users []domain.UserID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, users)
	return nil
}

func (s *subscriber) subscriptions() [][]domain.UserID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.subs)
}

func TestTheListSubscribesToThePresenceOfDMPartners(t *testing.T) {
	var sub *subscriber
	r := newRigWith(t, func(f *providertest.Fake) provider.Provider {
		sub = &subscriber{Fake: f}
		return sub
	})
	dm2 := must(domain.ParseConversationID("DEEE"))
	r.fake.SetConversations(
		domain.Conversation{ID: convA, Kind: domain.Channel},
		domain.Conversation{ID: dm, Kind: domain.DirectMessage, User: other, Latest: ts1},
		domain.Conversation{ID: dm2, Kind: domain.DirectMessage, User: me, Latest: ts2}, // the user's own DM
	)
	r.connected(t)

	deadline := time.Now().Add(patience)
	for len(sub.subscriptions()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := sub.subscriptions()
	// The user first (their own away when idle), then DM partners, the
	// most recent first, each once.
	if len(got) != 1 || !slices.Equal(got[0], []domain.UserID{me, other}) {
		t.Fatalf("subscriptions = %v, want [[%v %v]]", got, me, other)
	}
}
