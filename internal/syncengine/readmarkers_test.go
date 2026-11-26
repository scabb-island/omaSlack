package syncengine_test

import (
	"errors"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

// newReadMarkerRig is a rig whose provider can read markers but never
// streams read.changed, as internal-app.
func newReadMarkerRig(t *testing.T) *rig {
	t.Helper()
	return newRigWith(t, func(f *providertest.Fake) provider.Provider { return f.WithReadMarkers() })
}

func readTo(c domain.ConversationID, at domain.TS) domain.Event {
	return domain.ReadChanged{Team: team, Conversation: c, LastRead: at}
}

func (r *rig) lastRead(t *testing.T, c domain.ConversationID) domain.TS {
	t.Helper()
	got, err := r.store.LastRead(bg, c)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestReadMarkersArePolledForConversationsThatMatter(t *testing.T) {
	r := newReadMarkerRig(t)
	r.connected(t)
	if err := r.engine.OpenConversation(bg, convA); err != nil {
		t.Fatal(err)
	}
	r.fake.SetLastRead(convA, ts2)
	r.fake.SetLastRead(convB, ts2) // nobody has it open: never asked

	r.clock.fire(t, time.Minute)
	r.pub.expect(t, readTo(convA, ts2))
	if got := r.lastRead(t, convA); got != ts2 {
		t.Fatalf("store marker = %v", got)
	}
	if g := r.lastGrant(t); g != (grant{"conversations.info", ratebudget.Background}) {
		t.Fatalf("budget wait = %v", g)
	}

	r.fake.SetLastRead(convA, ts1) // an older marker changes nothing
	r.clock.fire(t, time.Minute)
	r.clock.await(t, time.Minute) // the next poll is scheduled: this one is done
	r.pub.expectNothing(t)
	if got := r.lastRead(t, convA); got != ts2 {
		t.Fatalf("store marker = %v after an older one", got)
	}
}

func TestAConversationNeverReadHasNoMarkerToMove(t *testing.T) {
	r := newReadMarkerRig(t)
	r.connected(t)
	if err := r.engine.OpenConversation(bg, convA); err != nil {
		t.Fatal(err)
	}
	r.clock.fire(t, time.Minute)
	r.clock.await(t, time.Minute)
	r.pub.expectNothing(t)
}

func TestAMarkerThatCannotBeReadDoesNotStopTheOthers(t *testing.T) {
	r := newReadMarkerRig(t)
	r.connected(t)
	for _, c := range []domain.ConversationID{convA, convB} {
		if err := r.engine.OpenConversation(bg, c); err != nil {
			t.Fatal(err)
		}
		r.fake.SetLastRead(c, ts1)
	}
	r.fake.Fail(providertest.MethodReadMarker, errors.New("channel_not_found"))

	r.clock.fire(t, time.Minute)
	r.pub.expect(t, readTo(convB, ts1))
}

func TestRejectedCredentialsWhilePollingStopTheEngine(t *testing.T) {
	r := newReadMarkerRig(t)
	_, result := r.connected(t)
	if err := r.engine.OpenConversation(bg, convA); err != nil {
		t.Fatal(err)
	}
	r.fake.Fail(providertest.MethodReadMarker, provider.ErrAuthExpired)

	r.clock.fire(t, time.Minute)
	r.pub.expect(t, state(domain.AuthExpired))
	if err := result(); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("Run = %v", err)
	}
}

func TestReconnectAlsoCatchesUpReadMarkers(t *testing.T) {
	r := newReadMarkerRig(t)
	live, _ := r.firstSession(t, created(convA, ts1))
	r.fake.SetHistory(convA, msg(ts1))
	r.fake.SetLastRead(convA, ts1)

	r.dropAndReconnect(t, live)
	r.pub.expect(t, readTo(convA, ts1))
}
