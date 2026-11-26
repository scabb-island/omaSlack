package syncengine

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

var (
	cA  = must(domain.ParseConversationID("CAAA"))
	cB  = must(domain.ParseConversationID("CBBB"))
	cD  = must(domain.ParseConversationID("DDDD"))
	usr = must(domain.ParseUserID("U01"))
	t1  = must(domain.ParseTS("1700000001.000000"))
	t2  = must(domain.ParseTS("1700000002.000000"))
	t3  = must(domain.ParseTS("1700000003.000000"))
	t4  = must(domain.ParseTS("1700000004.000000"))
)

func TestBackoffDoublesFromOneSecondAndStaysWithinItsCeiling(t *testing.T) {
	for attempt, ceiling := range []time.Duration{1, 2, 4, 8, 16, 30, 30, 30} {
		ceiling *= time.Second
		low, high := backoff(attempt, 0), backoff(attempt, 0.999999)
		if high > ceiling || high < ceiling-time.Millisecond {
			t.Errorf("attempt %d: highest = %v, want just under %v", attempt, high, ceiling)
		}
		if want := max(time.Second, ceiling/2); low != want {
			t.Errorf("attempt %d: lowest = %v, want %v", attempt, low, want)
		}
	}
}

func TestBackoffNeverLeavesOneToThirtySeconds(t *testing.T) {
	for _, attempt := range []int{0, 1, 5, 62, 63, 64, 1 << 30} {
		for _, r := range []float64{0, 0.5, 0.999999} {
			if d := backoff(attempt, r); d < time.Second || d > 30*time.Second {
				t.Errorf("backoff(%d, %v) = %v", attempt, r, d)
			}
		}
	}
}

func TestStatusShowsTheLinkUnlessRateLimited(t *testing.T) {
	cases := []struct {
		s    status
		want domain.ConnectionState
	}{
		{status{link: domain.Connected}, domain.Connected},
		{status{link: domain.Disconnected}, domain.Disconnected},
		{status{link: domain.Connected, limited: 1}, domain.RateLimited},
		{status{link: domain.Connecting, limited: 2}, domain.RateLimited},
		{status{link: domain.AuthExpired, limited: 1}, domain.AuthExpired},
	}
	for _, c := range cases {
		if got := c.s.shown(); got != c.want {
			t.Errorf("%+v.shown() = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestReconcileSetIsOpenActiveAndUnreadSortedWithoutDuplicates(t *testing.T) {
	type (
		opens   = map[domain.ConversationID]int
		actives = map[domain.ConversationID]struct{}
		list    = []domain.Conversation
	)
	var zero domain.TS
	cases := []struct {
		name   string
		open   opens
		active actives
		list   list
		want   []domain.ConversationID
	}{
		{"nothing", nil, nil, nil, nil},
		{"open and active, merged", opens{cB: 1, cD: 2}, actives{cD: {}, cA: {}}, nil, []domain.ConversationID{cA, cB, cD}},
		{"an unread DM nobody touched", nil, nil, list{{ID: cD, Latest: t2, LastRead: t1}}, []domain.ConversationID{cD}},
		{"never read but has a message", nil, nil, list{{ID: cD, Latest: t1, LastRead: zero}}, []domain.ConversationID{cD}},
		{"read up to the latest", nil, nil, list{{ID: cA, Latest: t2, LastRead: t2}}, nil},
		{"marker past the latest", nil, nil, list{{ID: cA, Latest: t1, LastRead: t2}}, nil},
		{"latest unknown", nil, nil, list{{ID: cA, LastRead: t1}}, nil},
		{"one mention, read state unknown", nil, nil, list{{ID: cB, Mentions: 1}}, []domain.ConversationID{cB}},
		{"a mention in a read channel", nil, nil, list{{ID: cB, Latest: t1, LastRead: t1, Mentions: 1}}, []domain.ConversationID{cB}},
		{"all three sources", opens{cA: 1}, actives{cA: {}}, list{{ID: cA, Mentions: 2}, {ID: cB}, {ID: cD, Latest: t3, LastRead: t1}}, []domain.ConversationID{cA, cD}},
	}
	for _, c := range cases {
		if got := reconcileSet(c.open, c.active, c.list); !slices.Equal(got, c.want) {
			t.Errorf("%s: reconcileSet = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestConversationOfNamesTheConversationAnEventTouches(t *testing.T) {
	cases := []struct {
		ev   domain.Event
		want domain.ConversationID
		ok   bool
	}{
		{domain.MessageCreated{Conversation: cA}, cA, true},
		{domain.MessageUpdated{Conversation: cB}, cB, true},
		{domain.MessageDeleted{Conversation: cD}, cD, true},
		{domain.ReactionChanged{Conversation: cA}, cA, true},
		{domain.ReadChanged{Conversation: cB}, domain.ConversationID{}, false},
		{domain.UserTyping{Conversation: cB}, domain.ConversationID{}, false},
		{domain.PresenceChanged{User: usr}, domain.ConversationID{}, false},
		{domain.ConnectionChanged{}, domain.ConversationID{}, false},
	}
	for _, c := range cases {
		got, ok := activityIn(c.ev)
		if got != c.want || ok != c.ok {
			t.Errorf("activityIn(%T) = %v, %v; want %v, %v", c.ev, got, ok, c.want, c.ok)
		}
	}
}

func m(at domain.TS) domain.Message { return domain.Message{TS: at} }

func TestMissedAreTheFetchedMessagesNotStoredOldestFirst(t *testing.T) {
	fetched := []domain.Message{m(t4), m(t3), m(t2), m(t1)} // newest first, as History returns
	known := map[domain.TS]bool{t1: true, t3: true}
	want := []domain.Message{m(t2), m(t4)}
	if got := missed(fetched, known); !reflect.DeepEqual(got, want) {
		t.Fatalf("missed = %v, want %v", got, want)
	}
}

func TestNothingIsMissedWhenTheStoreKnewNothing(t *testing.T) {
	// A first backfill fills the cache; it is not news for clients.
	if got := missed([]domain.Message{m(t2), m(t1)}, nil); len(got) != 0 {
		t.Fatalf("missed = %v, want none", got)
	}
}

func TestBridgedWhenAPageReachesWhatTheStoreHasOrSlackHasNoMore(t *testing.T) {
	var zero domain.TS
	page := []domain.Message{m(t4), m(t3)}
	cases := []struct {
		name   string
		newest domain.TS
		limit  int
		want   bool
	}{
		{"store empty: one page is enough", zero, 2, true},
		{"short page: Slack has no more", t1, 3, true},
		{"page reaches the newest stored", t3, 2, true},
		{"page goes past the newest stored", t2, 2, false},
	}
	for _, c := range cases {
		if got := bridged(page, c.newest, c.limit); got != c.want {
			t.Errorf("%s: bridged = %v", c.name, got)
		}
	}
}
