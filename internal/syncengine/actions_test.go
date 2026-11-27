package syncengine_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
)

func draft(c domain.ConversationID, thread domain.TS, text string) domain.Event {
	return domain.DraftChanged{Team: team, Conversation: c, Thread: thread, Text: text}
}

func reaction(c domain.ConversationID, at domain.TS, emoji string, added bool) domain.Event {
	return domain.ReactionChanged{Team: team, Conversation: c, TS: at, User: me, Emoji: emoji, Added: added}
}

// expectGrant checks the last budget wait was method at Interactive.
func (r *rig) expectGrant(t *testing.T, method string) {
	t.Helper()
	if g := r.lastGrant(t); g != (grant{method, ratebudget.Interactive}) {
		t.Fatalf("budget wait = %v, want %s at interactive", g, method)
	}
}

func (r *rig) draft(t *testing.T, c domain.ConversationID, thread domain.TS) string {
	t.Helper()
	got, err := r.store.Draft(bg, c, thread)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (r *rig) reactions(t *testing.T, c domain.ConversationID, at domain.TS) []store.Reaction {
	t.Helper()
	got, err := r.store.Reactions(bg, c, at)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestStateIsTheLastPublishedConnectionState(t *testing.T) {
	r := newRig(t)
	if got := r.engine.State(); got != domain.Connecting {
		t.Fatalf("State before Run = %v", got)
	}
	live := make(chan domain.Event)
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: live})
	r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected))
	if got := r.engine.State(); got != domain.Connected {
		t.Fatalf("State = %v, want connected", got)
	}
	live <- state(domain.AuthExpired)
	r.pub.expect(t, state(domain.AuthExpired))
	if got := r.engine.State(); got != domain.AuthExpired {
		t.Fatalf("State = %v, want auth_expired", got)
	}
}

func TestSendStoresPublishesAndClearsTheDraft(t *testing.T) {
	r, _ := newWritesRig(t)
	general := domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts1, LastRead: ts1}
	r.fake.SetConversations(general)
	r.connected(t)
	r.pub.expect(t, readTo(convA, ts1), entry(general))
	if err := r.engine.SetDraft(bg, convA, zeroTS, "hello"); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, draft(convA, zeroTS, "hello"))

	got, err := r.engine.Send(bg, convA, zeroTS, "hello")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Message{TS: ts4, Author: domain.UserAuthor(me), Text: "hello"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Send = %+v, want %+v", got, want)
	}
	general.Latest = ts4
	r.pub.expect(t, domain.MessageCreated{Team: team, Conversation: convA, Message: want}, entry(general), draft(convA, zeroTS, ""))
	if s := r.stored(t, convA); !slices.Equal(s, []domain.TS{ts4}) {
		t.Fatalf("store = %v", s)
	}
	if d := r.draft(t, convA, zeroTS); d != "" {
		t.Fatalf("draft = %q after sending", d)
	}
	r.expectGrant(t, "chat.postMessage")
}

func TestSendingInAThreadClearsOnlyThatThreadsDraft(t *testing.T) {
	r, _ := newWritesRig(t)
	r.connected(t)
	for _, th := range []domain.TS{zeroTS, ts1} {
		if err := r.engine.SetDraft(bg, convA, th, "draft"); err != nil {
			t.Fatal(err)
		}
	}
	r.pub.expect(t, draft(convA, zeroTS, "draft"), draft(convA, ts1, "draft"))

	if _, err := r.engine.Send(bg, convA, ts1, "reply"); err != nil {
		t.Fatal(err)
	}
	reply := domain.Message{TS: ts4, ThreadTS: ts1, Author: domain.UserAuthor(me), Text: "reply"}
	r.pub.expect(t, domain.MessageCreated{Team: team, Conversation: convA, Message: reply}, draft(convA, ts1, ""))
	if d := r.draft(t, convA, zeroTS); d != "draft" {
		t.Fatalf("channel draft = %q, want it kept", d)
	}
}

func TestSendingWithoutADraftPublishesNoDraftChange(t *testing.T) {
	r, _ := newWritesRig(t)
	r.connected(t)
	m, err := r.engine.Send(bg, convA, zeroTS, "hi")
	if err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, domain.MessageCreated{Team: team, Conversation: convA, Message: m})
	r.pub.expectNothing(t)
}

func TestASendSlackRefusesStoresNothing(t *testing.T) {
	r, _ := newWritesRig(t)
	r.connected(t)
	refused := errors.New("channel_not_found")
	r.fake.Fail(providertest.MethodPost, refused)
	if _, err := r.engine.Send(bg, convA, zeroTS, "hi"); !errors.Is(err, refused) {
		t.Fatalf("Send = %v", err)
	}
	r.pub.expectNothing(t)
	if s := r.stored(t, convA); len(s) != 0 {
		t.Fatalf("store = %v", s)
	}
}

func TestEditKeepsTheThreadAndPublishesTheUpdate(t *testing.T) {
	r, w := newWritesRig(t)
	reply := domain.Message{TS: ts2, ThreadTS: ts1, Author: domain.UserAuthor(me), Text: "typo"}
	r.cache(t, convA, reply)
	r.connected(t)

	got, err := r.engine.Edit(bg, convA, ts2, "fixed")
	if err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, domain.MessageUpdated{Team: team, Conversation: convA, Message: got})
	replies, err := r.store.Replies(bg, convA, ts1)
	want := reply
	want.Text, want.Edited = "fixed", ts4
	if err != nil || len(replies) != 1 || !reflect.DeepEqual(replies[0], want) {
		t.Fatalf("thread = %+v, %v; want %+v", replies, err, want)
	}
	if c := w.recorded(); len(c) != 1 {
		t.Fatalf("calls = %q", c)
	}
	r.expectGrant(t, "chat.update")
}

func TestDeleteTombstonesAndPublishes(t *testing.T) {
	r, _ := newWritesRig(t)
	r.cache(t, convA, msg(ts1), msg(ts2))
	r.connected(t)

	if err := r.engine.Delete(bg, convA, ts2); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, domain.MessageDeleted{Team: team, Conversation: convA, TS: ts2})
	if s := r.stored(t, convA); !slices.Equal(s, []domain.TS{ts1}) {
		t.Fatalf("store = %v", s)
	}
	r.expectGrant(t, "chat.delete")
}

func TestReactStoresAndPublishesTheUsersReaction(t *testing.T) {
	r, _ := newWritesRig(t)
	r.cache(t, convA, msg(ts1))
	r.connected(t)

	if err := r.engine.React(bg, convA, ts1, "thumbsup", true); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, reaction(convA, ts1, "thumbsup", true))
	want := []store.Reaction{{Emoji: "thumbsup", Users: []domain.UserID{me}}}
	if got := r.reactions(t, convA, ts1); !reflect.DeepEqual(got, want) {
		t.Fatalf("reactions = %+v", got)
	}
	r.expectGrant(t, "reactions.add")

	if err := r.engine.React(bg, convA, ts1, "thumbsup", false); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, reaction(convA, ts1, "thumbsup", false))
	if got := r.reactions(t, convA, ts1); len(got) != 0 {
		t.Fatalf("reactions = %+v", got)
	}
	r.expectGrant(t, "reactions.remove")
}

// Slack echoes the user's own reaction on the stream, before or after the
// call returns. Whichever comes second changes nothing and is not
// published, so no client counts it twice.
func TestTheEchoOfAReactionIsNotPublishedTwice(t *testing.T) {
	r, _ := newWritesRig(t)
	r.cache(t, convA, msg(ts1))
	live, _ := r.firstSession(t)

	// The echo after the call.
	if err := r.engine.React(bg, convA, ts1, "tada", true); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, reaction(convA, ts1, "tada", true))
	live <- reaction(convA, ts1, "tada", true)
	live <- created(convB, ts1) // a marker: the echo was handled before it
	r.pub.expect(t, created(convB, ts1))

	// The echo before the call returns.
	live <- reaction(convA, ts1, "tada", false)
	r.pub.expect(t, reaction(convA, ts1, "tada", false))
	if err := r.engine.React(bg, convA, ts1, "tada", false); err != nil {
		t.Fatal(err)
	}
	r.pub.expectNothing(t)
}

func TestMarkReadMovesTheMarkerInSlackAndTheStore(t *testing.T) {
	r, _ := newWritesRig(t)
	general := domain.Conversation{ID: convA, Kind: domain.Channel, Latest: ts2, LastRead: ts1, Mentions: 1}
	r.fake.SetConversations(general)
	r.connected(t)
	r.pub.expect(t, readTo(convA, ts1), entry(general))

	if err := r.engine.MarkRead(bg, convA, ts2); err != nil {
		t.Fatal(err)
	}
	general.LastRead, general.Mentions = ts2, 0
	r.pub.expect(t, readTo(convA, ts2), entry(general))
	if got := r.lastRead(t, convA); got != ts2 {
		t.Fatalf("marker = %v", got)
	}
	r.expectGrant(t, "conversations.mark")
	if c := r.callsAfterConnect(); !slices.Equal(c, []providertest.Call{{Method: providertest.MethodMarkRead, Conv: convA, TS: ts2}}) {
		t.Fatalf("calls = %v", c)
	}
}

func TestMarkReadNeverMovesTheMarkerBack(t *testing.T) {
	r, _ := newWritesRig(t)
	r.connected(t)
	if err := r.engine.MarkRead(bg, convA, ts2); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, readTo(convA, ts2))
	for _, at := range []domain.TS{ts1, ts2} {
		if err := r.engine.MarkRead(bg, convA, at); err != nil {
			t.Fatal(err)
		}
	}
	r.pub.expectNothing(t)
	if c := r.callsAfterConnect(); len(c) != 1 {
		t.Fatalf("calls = %v, want only the first mark", c)
	}
}

func TestSetDraftNeverReachesSlack(t *testing.T) {
	r, w := newWritesRig(t)
	r.connected(t)
	grants := len(r.budget.grants())
	if err := r.engine.SetDraft(bg, convA, ts1, "later"); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, draft(convA, ts1, "later"))
	if d := r.draft(t, convA, ts1); d != "later" {
		t.Fatalf("draft = %q", d)
	}
	if err := r.engine.SetDraft(bg, convA, ts1, ""); err != nil {
		t.Fatal(err)
	}
	r.pub.expect(t, draft(convA, ts1, ""))
	if len(r.budget.grants()) != grants || len(r.callsAfterConnect()) != 0 || len(w.recorded()) != 0 {
		t.Fatal("a draft reached Slack")
	}
}

func TestPassThroughsReachTheProviderAndPublishNothing(t *testing.T) {
	r, w := newWritesRig(t, domain.Search, domain.PresenceWrite)
	w.hits = []domain.SearchHit{{Conversation: convA, Message: msg(ts1)}}
	w.users = []domain.User{{ID: me, Name: "me"}}
	r.connected(t)

	hits, err := r.engine.SearchMessages(bg, "in:#general", 5)
	if err != nil || !reflect.DeepEqual(hits, w.hits) {
		t.Fatalf("SearchMessages = %+v, %v", hits, err)
	}
	r.expectGrant(t, "search.messages")
	users, err := r.engine.SearchUsers(bg, "me", 3)
	if err != nil || !reflect.DeepEqual(users, w.users) {
		t.Fatalf("SearchUsers = %+v, %v", users, err)
	}
	r.expectGrant(t, "users.search")
	for method, call := range map[string]func() error{
		"users.setPresence": func() error { return r.engine.SetPresence(bg, true) },
		"dnd.setSnooze":     func() error { return r.engine.SnoozeDND(bg, 30) },
		"dnd.endSnooze":     func() error { return r.engine.EndDND(bg) },
	} {
		if err := call(); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		r.expectGrant(t, method)
	}
	want := []string{"search.messages in:#general 5\n", "users.search me 3\n"}
	if got := w.recorded(); !slices.Equal(got[:2], want) || !slices.Contains(got, "users.setPresence true\n") || !slices.Contains(got, "dnd.setSnooze 30\n") {
		t.Fatalf("calls = %q", got)
	}
	r.pub.expectNothing(t)
}

func TestNamesEmojiAndTypingReachTheProviderAndPublishNothing(t *testing.T) {
	r, w := newWritesRig(t, domain.CustomEmoji, domain.Typing)
	w.users = []domain.User{{ID: me, Name: "me"}}
	r.connected(t)

	users, err := r.engine.Users(bg, []domain.UserID{me})
	if err != nil || !reflect.DeepEqual(users, w.users) {
		t.Fatalf("Users = %+v, %v", users, err)
	}
	r.expectGrant(t, "users.info")
	emoji, err := r.engine.Emoji(bg)
	if err != nil || emoji["shipit"] != "alias:squirrel" {
		t.Fatalf("Emoji = %v, %v", emoji, err)
	}
	r.expectGrant(t, "emoji.list")
	if err := r.engine.SendTyping(bg, convA); err != nil {
		t.Fatalf("SendTyping = %v", err)
	}
	r.expectGrant(t, "typing")
	want := []string{"users.info [U0ME]\n", "emoji.list\n", "typing CAAA\n"}
	if got := w.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	r.pub.expectNothing(t)
}

func TestProviderErrorsOfActionsReachTheCaller(t *testing.T) {
	r, w := newWritesRig(t, domain.Search)
	r.connected(t)
	refused := errors.New("cant_update_message")
	w.failNext("chat.update", refused)
	if _, err := r.engine.Edit(bg, convA, ts1, "x"); !errors.Is(err, refused) {
		t.Fatalf("Edit = %v", err)
	}
	w.failNext("search.messages", refused)
	if _, err := r.engine.SearchMessages(bg, "x", 1); !errors.Is(err, refused) {
		t.Fatalf("SearchMessages = %v", err)
	}
	r.pub.expectNothing(t)
}

func TestRejectedCredentialsDuringAnActionStopTheEngine(t *testing.T) {
	r, w := newWritesRig(t)
	_, result := r.connected(t)
	w.failNext("dnd.endSnooze", provider.ErrAuthExpired)
	if err := r.engine.EndDND(bg); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("EndDND = %v", err)
	}
	r.pub.expect(t, state(domain.AuthExpired))
	if err := result(); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("Run = %v", err)
	}
}

// actions calls every action once and returns what each returned.
func actions(e *syncengine.Engine) map[string]error {
	ctx := context.Background()
	return map[string]error{
		"Send":           second(e.Send(ctx, convA, zeroTS, "x")),
		"Edit":           second(e.Edit(ctx, convA, ts1, "x")),
		"Delete":         e.Delete(ctx, convA, ts1),
		"React":          e.React(ctx, convA, ts1, "x", true),
		"MarkRead":       e.MarkRead(ctx, convA, ts1),
		"SetDraft":       e.SetDraft(ctx, convA, zeroTS, "x"),
		"SearchMessages": second(e.SearchMessages(ctx, "x", 1)),
		"SearchUsers":    second(e.SearchUsers(ctx, "x", 1)),
		"SetPresence":    e.SetPresence(ctx, true),
		"SnoozeDND":      e.SnoozeDND(ctx, 1),
		"EndDND":         e.EndDND(ctx),
		"Users":          second(e.Users(ctx, []domain.UserID{me})),
		"Emoji":          second(e.Emoji(ctx)),
		"SendTyping":     e.SendTyping(ctx, convA),
	}
}

func second[T any](_ T, err error) error { return err }

func TestActionsTheProviderLacksAreUnsupported(t *testing.T) {
	r := newBareRig(t)
	r.connected(t)
	for name, err := range actions(r.engine) {
		// Every provider can post and mark read, and drafts stay local.
		required := name == "Send" || name == "MarkRead" || name == "SetDraft"
		if !required && !errors.Is(err, domain.ErrUnsupported) {
			t.Errorf("%s = %v, want unsupported", name, err)
		}
	}
}

func TestActionsNeedingAnUndeclaredCapabilityAreUnsupported(t *testing.T) {
	r, w := newWritesRig(t) // has the interfaces, declares neither capability
	r.connected(t)
	if _, err := r.engine.SearchMessages(bg, "x", 1); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("SearchMessages = %v", err)
	}
	if err := r.engine.SetPresence(bg, true); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("SetPresence = %v", err)
	}
	if _, err := r.engine.Emoji(bg); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("Emoji = %v", err)
	}
	if err := r.engine.SendTyping(bg, convA); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("SendTyping = %v", err)
	}
	if c := w.recorded(); len(c) != 0 {
		t.Errorf("calls = %q", c)
	}
}

func TestActionsFailWhenRunIsNotRunning(t *testing.T) {
	r, w := newWritesRig(t, domain.Search, domain.PresenceWrite)
	check := func(when string) {
		t.Helper()
		for name, err := range actions(r.engine) {
			if !errors.Is(err, syncengine.ErrStopped) {
				t.Errorf("%s %s = %v, want ErrStopped", name, when, err)
			}
		}
	}
	check("before Run")
	cancel, result := r.connected(t)
	cancel()
	_ = result()
	check("after Run")
	if c := w.recorded(); len(c) != 0 || len(r.callsAfterConnect()) != 0 {
		t.Errorf("calls = %q, %v", c, r.callsAfterConnect())
	}
}
