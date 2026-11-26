package syncengine_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// awaitListed waits until the provider has been asked for its conversation
// list n times in all. The budget was asked before each, so once this
// returns that grant is recorded.
func (r *rig) awaitListed(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for r.listCalls() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d conversation lists, want %d", r.listCalls(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *rig) listCalls() int {
	n := 0
	for _, c := range r.fake.Calls() {
		if c.Method == providertest.MethodConversations {
			n++
		}
	}
	return n
}

func TestAnUnreadDMNobodyTouchedIsCaughtUpAfterReconnecting(t *testing.T) {
	r := newRig(t)
	r.cache(t, dm, msg(ts1)) // left by the previous session
	live, _ := r.firstSession(t)
	r.fake.SetHistory(dm, msg(ts1), msg(ts2))
	r.fake.SetConversations(domain.Conversation{ID: dm, Kind: domain.DirectMessage, Latest: ts2})

	r.dropAndReconnect(t, live)
	r.pub.expect(t, created(dm, ts2))
	if got := r.stored(t, dm); !slices.Equal(got, []domain.TS{ts2, ts1}) {
		t.Fatalf("store = %v", got)
	}
}

func TestAMentionIsCaughtUpAfterReconnectingAndAReadChannelIsNot(t *testing.T) {
	r := newRig(t)
	r.cache(t, convA, msg(ts1))
	r.cache(t, convB, msg(ts1))
	live, _ := r.firstSession(t)
	r.fake.SetHistory(convA, msg(ts1), msg(ts2))
	r.fake.SetHistory(convB, msg(ts1), msg(ts2))
	r.fake.SetConversations(
		domain.Conversation{ID: convA, Kind: domain.Channel, Mentions: 1},
		domain.Conversation{ID: convB, Kind: domain.Channel, Latest: ts2, LastRead: ts2},
	)
	seen := len(r.historyCalls())

	r.dropAndReconnect(t, live)
	// The markers the list reports are stored first, then the catch-up.
	r.pub.expect(t, readTo(convB, ts2), created(convA, ts2))
	want := []providertest.Call{{Method: providertest.MethodHistory, Conv: convA}}
	if got := r.historyCalls()[seen:]; !slices.Equal(got, want) {
		t.Fatalf("History calls after reconnect = %v, want %v", got, want)
	}
}

func TestAColdStartStoresTheReadMarkersTheListReports(t *testing.T) {
	r := newRig(t)
	r.fake.SetConversations(
		domain.Conversation{ID: dm, Kind: domain.DirectMessage, Latest: ts1}, // never read
		domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts3, LastRead: ts2},
	)
	r.firstSession(t)

	r.pub.expect(t, readTo(convA, ts2))
	if got := r.lastRead(t, convA); got != ts2 {
		t.Fatalf("convA marker = %v", got)
	}
	if got := r.lastRead(t, dm); !got.IsZero() {
		t.Fatalf("dm marker = %v, want none", got)
	}
	if got := r.historyCalls(); len(got) != 0 {
		t.Fatalf("History calls = %v, want none on a cold start", got)
	}
	if g := r.budget.grants(); !slices.Contains(g, grant{"users.conversations", ratebudget.Background}) {
		t.Fatalf("budget waits = %v, want the list at background", g)
	}
}

func TestAColdStartNeverMovesAStoredMarkerBack(t *testing.T) {
	r := newRig(t)
	if err := r.store.Apply(bg, readTo(convA, ts3)); err != nil {
		t.Fatal(err)
	}
	r.fake.SetConversations(
		domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts3, LastRead: ts2},
		domain.Conversation{ID: convB, Kind: domain.Channel, Latest: ts1, LastRead: ts1},
	)
	r.firstSession(t)

	// The list is handled in order, so convB's marker means convA's is done.
	r.pub.expect(t, readTo(convB, ts1))
	r.pub.expectNothing(t)
	if got := r.lastRead(t, convA); got != ts3 {
		t.Fatalf("convA marker = %v, want ts3 kept", got)
	}
}

func TestAMarkerTheStoreRejectsDoesNotStopTheOthers(t *testing.T) {
	r := newRigFrom(t, nil, func(s store.Store) store.Store { return &failingStore{Store: s} })
	r.fake.SetConversations(
		domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts2, LastRead: ts1},
		domain.Conversation{ID: convB, Kind: domain.Channel, Latest: ts2, LastRead: ts1},
	)
	r.firstSession(t)

	r.pub.expect(t, readTo(convB, ts1))
	if logs := r.logs.String(); !strings.Contains(logs, "disk full") || !strings.Contains(logs, convA.String()) {
		t.Fatalf("log = %q, want convA's failure", logs)
	}
}

func TestAListThatFailsFallsBackToOpenAndActiveConversationsAndLogsOnce(t *testing.T) {
	r := newRig(t)
	notYet := errors.New("users.conversations: not implemented yet")
	r.fake.Fail(providertest.MethodConversations, notYet, notYet)
	r.fake.SetHistory(convA, msg(ts1), msg(ts2))
	live, _ := r.firstSession(t, created(convA, ts1))

	r.dropAndReconnect(t, live)
	r.pub.expect(t, created(convA, ts2))
	if n := strings.Count(r.logs.String(), "not implemented yet"); n != 1 {
		t.Fatalf("logged the failing list %d times, want once: %q", n, r.logs.String())
	}
}

func TestRejectedCredentialsWhileListingStopTheEngine(t *testing.T) {
	r := newRig(t)
	live, result := r.firstSession(t, created(convA, ts1))
	r.fake.Fail(providertest.MethodConversations, provider.ErrAuthExpired)

	r.dropAndReconnect(t, live)
	r.pub.expect(t, state(domain.AuthExpired))
	if err := result(); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("Run = %v", err)
	}
	if got := r.historyCalls(); len(got) != 0 {
		t.Fatalf("History calls = %v, want none once the list was rejected", got)
	}
}
