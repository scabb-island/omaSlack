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

// The read-state fill (spec sync-engine, "Provider list without read
// state"), with a provider like internal-app: its list has no latest
// message, read marker or mentions, and it reads them one conversation
// at a time.

var (
	groupBusy = must(domain.ParseConversationID("GBUSY"))
	groupIdle = must(domain.ParseConversationID("GIDLE"))
)

// newStateRig is a rig whose provider lists conversations without read
// state and reads it per conversation, as internal-app.
func newStateRig(t *testing.T) *rig {
	t.Helper()
	return newRigWith(t, func(f *providertest.Fake) provider.Provider { return f.WithConversationState() })
}

// unfilled is a list entry as internal-app lists it: no read state.
func unfilled(c domain.ConversationID, k domain.ConversationKind) domain.Conversation {
	return domain.Conversation{ID: c, Kind: k}
}

// workspace scripts a list in no useful order: a dormant group DM, a
// channel, a group DM with stored messages, a private channel and a DM.
// Slack reports the DM's latest message itself; for the others the
// engine asks History.
func (r *rig) workspace(t *testing.T) {
	t.Helper()
	r.fake.SetConversations(
		unfilled(groupIdle, domain.GroupMessage), unfilled(convA, domain.Channel), unfilled(groupBusy, domain.GroupMessage),
		unfilled(convB, domain.PrivateChannel), unfilled(dm, domain.DirectMessage),
	)
	r.cache(t, groupBusy, msg(ts1))
	r.fake.SetState(domain.Conversation{ID: dm, Latest: ts3, LastRead: ts1, Mentions: 2})
	r.fake.SetState(domain.Conversation{ID: convA, LastRead: ts1})
	r.fake.SetHistory(convA, msg(ts1), msg(ts2))
	r.fake.SetState(domain.Conversation{ID: convB, LastRead: ts2})
	r.fake.SetHistory(convB, msg(ts2))
	r.fake.SetState(domain.Conversation{ID: groupBusy, LastRead: ts1})
	r.fake.SetHistory(groupBusy, msg(ts1), msg(ts2))
}

// filled is each workspace entry once filled in.
var (
	filledDM    = domain.Conversation{ID: dm, Kind: domain.DirectMessage, Latest: ts3, LastRead: ts1, Mentions: 2}
	filledA     = domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts2, LastRead: ts1}
	filledB     = domain.Conversation{ID: convB, Kind: domain.PrivateChannel, Latest: ts2, LastRead: ts2}
	filledGroup = domain.Conversation{ID: groupBusy, Kind: domain.GroupMessage, Latest: ts2, LastRead: ts1}
)

// expectBareList expects the workspace's list as first stored: only the
// busy group DM has a latest message, from the store.
func (r *rig) expectBareList(t *testing.T) {
	t.Helper()
	r.pub.expect(t,
		entry(unfilled(convA, domain.Channel)), entry(unfilled(convB, domain.PrivateChannel)), entry(unfilled(dm, domain.DirectMessage)),
		entry(domain.Conversation{ID: groupBusy, Kind: domain.GroupMessage, Latest: ts1}), entry(unfilled(groupIdle, domain.GroupMessage)),
	)
}

// expectFilled expects the first full pass over the workspace, and waits
// until it has ended.
func (r *rig) expectFilled(t *testing.T) {
	t.Helper()
	r.pub.expect(t,
		readTo(dm, ts1), entry(filledDM),
		readTo(convA, ts1), entry(filledA),
		readTo(convB, ts2), entry(filledB),
		readTo(groupBusy, ts1), entry(filledGroup),
	)
	r.waitForLog(t, "read state filled in", 1)
}

// stateCalls are the calls a fill makes, in order.
func (r *rig) stateCalls() []providertest.Call {
	return slices.DeleteFunc(r.fake.Calls(), func(c providertest.Call) bool {
		return c.Method != providertest.MethodReadMarker && c.Method != providertest.MethodHistory
	})
}

// awaitStateCalls waits until the fill has made n calls in all.
func (r *rig) awaitStateCalls(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for len(r.stateCalls()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d fill calls, want %d: %v", len(r.stateCalls()), n, r.stateCalls())
		}
		time.Sleep(time.Millisecond)
	}
}

func info(c domain.ConversationID) providertest.Call {
	return providertest.Call{Method: providertest.MethodReadMarker, Conv: c}
}

func newest(c domain.ConversationID) providertest.Call {
	return providertest.Call{Method: providertest.MethodHistory, Conv: c}
}

// onePass is the calls of one full pass over the workspace.
var onePass = []providertest.Call{
	info(dm), info(convA), newest(convA), info(convB), newest(convB), info(groupBusy), newest(groupBusy),
}

func TestAListWithoutReadStateIsFilledInDirectMessagesFirst(t *testing.T) {
	r := newStateRig(t)
	r.workspace(t)
	r.connected(t)

	r.expectBareList(t)
	r.expectFilled(t)
	// DMs first, then channels, then the group DM with messages; the
	// dormant one is never asked about.
	if got := r.stateCalls(); !slices.Equal(got, onePass) {
		t.Fatalf("calls = %v\nwant    %v", got, onePass)
	}
	if got := r.listed(t); !slices.Equal(got, []domain.Conversation{filledA, filledB, filledDM, filledGroup, unfilled(groupIdle, domain.GroupMessage)}) {
		t.Fatalf("stored list = %+v", got)
	}
	for _, g := range r.budget.grants() {
		if (g.method == "conversations.info" || g.method == "conversations.history") && g.priority != ratebudget.Background {
			t.Fatalf("budget wait %v, want every fill call at background priority", g)
		}
	}
}

func TestAReconnectSoonAfterAFullPassKeepsWhatItLearned(t *testing.T) {
	r := newStateRig(t)
	r.workspace(t)
	live, _ := r.firstSession(t)
	r.expectBareList(t)
	r.expectFilled(t)

	// The new list has no read state again: the entries keep theirs, and
	// no second pass starts within half an hour.
	r.clock.advance(29 * time.Minute)
	live = r.reconnectKeepingLive(t, live)
	r.waitForLog(t, "read state is recent", 1)

	// Half an hour after the pass, the next reconnect runs another; what
	// it finds has not changed, so nothing is published.
	r.clock.advance(time.Minute)
	r.dropAndReconnect(t, live)
	r.awaitStateCalls(t, 2*len(onePass))
	r.waitForLog(t, "read state filled in", 2)
	r.pub.expectNothing(t)
	if got := r.stateCalls(); !slices.Equal(got, slices.Concat(onePass, onePass)) {
		t.Fatalf("calls = %v, want two passes", got)
	}
}

func TestOnlyEntriesThatChangedArePublished(t *testing.T) {
	r := newStateRig(t)
	r.workspace(t)
	live, _ := r.firstSession(t)
	r.expectBareList(t)
	r.expectFilled(t)

	r.fake.SetState(domain.Conversation{ID: convB, LastRead: ts2})
	r.fake.SetHistory(convB, msg(ts2), msg(ts3)) // one new message in convB
	r.clock.advance(time.Hour)
	r.dropAndReconnect(t, live)

	changed := filledB
	changed.Latest = ts3
	r.pub.expect(t, entry(changed))
	r.waitForLog(t, "read state filled in", 2)
	r.pub.expectNothing(t)
}

func TestAReconnectRestartsAPassWithoutRunningTwo(t *testing.T) {
	r := newStateRig(t)
	r.workspace(t)
	r.budget.holdOn("conversations.history") // the pass stops at convA's
	live, _ := r.firstSession(t)
	r.expectBareList(t)
	r.budget.awaitHeld(t)
	r.pub.expect(t, readTo(dm, ts1), entry(filledDM))

	r.dropAndReconnect(t, live)
	r.budget.awaitHeld(t)

	// The restart picks up where the pass stopped: the DM is not asked
	// again, convA is.
	want := []providertest.Call{info(dm), info(convA), info(convA)}
	if got := r.stateCalls(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if n := r.budget.mostHeldAtOnce(); n != 1 {
		t.Fatalf("%d fill calls waited at once, want one pass at a time", n)
	}
}

func TestAFailedConversationIsLoggedAndThePassGoesOn(t *testing.T) {
	r := newStateRig(t)
	r.workspace(t)
	r.fake.Fail(providertest.MethodReadMarker, errors.New("channel_not_found"))
	r.connected(t)

	r.expectBareList(t)
	r.pub.expect(t,
		readTo(convA, ts1), entry(filledA),
		readTo(convB, ts2), entry(filledB),
		readTo(groupBusy, ts1), entry(filledGroup),
	)
	r.waitForLog(t, "could not fill in the read state of a conversation", 1)
}

func TestARateLimitedFillWaitsOnTheBudget(t *testing.T) {
	r := newStateRig(t)
	r.workspace(t)
	r.fake.Fail(providertest.MethodReadMarker, &provider.RateLimitedError{RetryAfter: 30 * time.Second})
	r.connected(t)

	r.expectBareList(t)
	r.pub.expect(t, state(domain.RateLimited), state(domain.Connected))
	r.expectFilled(t)
	if got := r.budget.penalized(); !slices.Equal(got, []penalty{{"conversations.info", 30 * time.Second}}) {
		t.Fatalf("penalties = %v", got)
	}
}

func TestAProviderWithReadStateInItsListIsNotFilledIn(t *testing.T) {
	r := newRig(t) // as desktop-session: no ConversationStateReader
	general := domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts2, LastRead: ts1}
	r.fake.SetConversations(general, unfilled(dm, domain.DirectMessage))
	r.connected(t)

	r.pub.expect(t, readTo(convA, ts1), entry(general), entry(unfilled(dm, domain.DirectMessage)))
	if got := r.stateCalls(); len(got) != 0 {
		t.Fatalf("calls = %v, want none", got)
	}
}
