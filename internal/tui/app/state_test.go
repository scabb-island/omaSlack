package app

import (
	"encoding/json"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

const (
	tsA = "1700000100.000100"
	tsB = "1700000200.000200"
	tsC = "1700000300.000300"
)

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hello(ws ...wire.Workspace) map[string]any {
	return map[string]any{"protocol": 1, "workspaces": ws}
}

func acme() wire.Workspace {
	return wire.Workspace{Team: "T1", Name: "Acme", Provider: "internal-app", State: "connected"}
}

func newTestState(t *testing.T) *State {
	t.Helper()
	s, err := NewState(raw(t, hello(acme(), wire.Workspace{Team: "T2", Name: "Beta", State: "connecting"})))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func apply(t *testing.T, s *State, topic string, params any) {
	t.Helper()
	if err := s.Apply(topic, raw(t, params), time.Unix(1700000400, 0)); err != nil {
		t.Fatalf("Apply(%s): %v", topic, err)
	}
}

func texts(msgs []wire.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Text
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestStateFromHello(t *testing.T) {
	s := newTestState(t)
	if got := s.Teams(); !equal(got, []string{"T1", "T2"}) {
		t.Fatalf("teams = %v", got)
	}
	if w := s.Workspace("T2"); w.Name != "Beta" || w.State != "connecting" {
		t.Fatalf("T2 = %+v", w)
	}
	if _, err := NewState(json.RawMessage(`{"workspaces":3}`)); err == nil {
		t.Fatal("a hello that does not parse was accepted")
	}
	empty, err := NewState(json.RawMessage(`{"protocol":1}`))
	if err != nil || len(empty.Teams()) != 0 {
		t.Fatalf("hello without workspaces: %v, %v", empty.Teams(), err)
	}
}

func TestMessagesAreKeyedByTSAndSorted(t *testing.T) {
	s := newTestState(t)
	// messages.history answers newest first.
	s.PutMessages("T1", "C1", []wire.Message{{TS: tsC, Text: "c"}, {TS: tsA, Text: "a"}})
	// The daemon may publish a sent message twice.
	for range 2 {
		apply(t, s, "message.created", wire.MessageEvent{Team: "T1", Conv: "C1", Message: wire.Message{TS: tsB, Text: "b"}})
	}
	if got := texts(s.Messages("T1", "C1")); !equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("messages = %v", got)
	}
	if got := s.Messages("T2", "C1"); len(got) != 0 {
		t.Fatalf("another team's conversation has %v", got)
	}
}

func TestThreadRepliesStayOutOfTheConversation(t *testing.T) {
	s := newTestState(t)
	s.PutMessages("T1", "C1", []wire.Message{
		{TS: tsA, ThreadTS: tsA, Text: "root"},
		{TS: tsB, ThreadTS: tsA, Text: "reply"},
		{TS: tsC, Text: "plain"},
	})
	if got := texts(s.Messages("T1", "C1")); !equal(got, []string{"root", "plain"}) {
		t.Fatalf("messages = %v", got)
	}
	if got := texts(s.Replies("T1", "C1", tsA)); !equal(got, []string{"root", "reply"}) {
		t.Fatalf("replies = %v", got)
	}
}

func TestMessageUpdatesKeepReactionsAndDeletesRemove(t *testing.T) {
	s := newTestState(t)
	s.PutMessages("T1", "C1", []wire.Message{{TS: tsA, Text: "hi", Reactions: []wire.Reaction{{Emoji: "tada", Users: []string{"U1"}}}}})
	apply(t, s, "message.updated", wire.MessageEvent{Team: "T1", Conv: "C1", Message: wire.Message{TS: tsA, Text: "hello", Edited: tsB}})
	m := s.Messages("T1", "C1")[0]
	if m.Text != "hello" || len(m.Reactions) != 1 {
		t.Fatalf("updated = %+v", m)
	}
	apply(t, s, "message.deleted", wire.MessageDeletedEvent{Team: "T1", Conv: "C1", TS: tsA})
	if got := s.Messages("T1", "C1"); len(got) != 0 {
		t.Fatalf("after delete = %v", got)
	}
}

func TestReactionChanged(t *testing.T) {
	s := newTestState(t)
	s.PutMessages("T1", "C1", []wire.Message{{TS: tsA, Text: "hi"}})
	react := func(user, emoji string, added bool) {
		apply(t, s, "reaction.changed", wire.ReactionEvent{Team: "T1", Conv: "C1", TS: tsA, User: user, Emoji: emoji, Added: added})
	}
	react("U1", "tada", true)
	react("U2", "tada", true)
	react("U2", "tada", true) // the same reaction twice counts once
	react("U1", "eyes", true)
	got := s.Messages("T1", "C1")[0].Reactions
	if len(got) != 2 || got[0].Emoji != "tada" || !equal(got[0].Users, []string{"U1", "U2"}) || got[1].Emoji != "eyes" {
		t.Fatalf("reactions = %+v", got)
	}
	react("U1", "eyes", false)
	react("U1", "tada", false)
	got = s.Messages("T1", "C1")[0].Reactions
	if len(got) != 1 || !equal(got[0].Users, []string{"U2"}) {
		t.Fatalf("after removing = %+v", got)
	}
	react("U9", "tada", true) // unknown message: ignored
	apply(t, s, "reaction.changed", wire.ReactionEvent{Team: "T1", Conv: "C1", TS: tsC, User: "U1", Emoji: "x", Added: true})
}

func TestConversationsReadStateAndConnection(t *testing.T) {
	s := newTestState(t)
	s.SetConversations("T1", []wire.Conversation{
		{ID: "C1", Kind: "channel", Name: "general", Latest: tsB, LastRead: tsA, Unread: true},
	})
	if !s.Loaded("T1") || s.Loaded("T2") {
		t.Fatal("loaded flags wrong")
	}
	apply(t, s, "read.changed", wire.ReadEvent{Team: "T1", Conv: "C1", LastRead: tsB})
	if c, _ := s.Conversation("T1", "C1"); c.Unread || c.LastRead != tsB {
		t.Fatalf("after read.changed: %+v", c)
	}
	apply(t, s, "conversation.updated", wire.ConversationEvent{Team: "T1", Conversation: wire.Conversation{ID: "D1", Kind: "im", User: "U1", Latest: tsC, Unread: true, Mentions: 1}})
	if c, ok := s.Conversation("T1", "D1"); !ok || c.Mentions != 1 {
		t.Fatalf("D1 = %+v, %v", c, ok)
	}
	apply(t, s, "connection.state", wire.ConnectionEvent{Team: "T1", State: "disconnected"})
	if s.Workspace("T1").State != "disconnected" {
		t.Fatal("connection state not applied")
	}
	apply(t, s, "unread.summary", wire.Summary{Workspaces: map[string]wire.Counts{"T1": {DMs: 1}}})
	if s.Summary().Workspaces["T1"].DMs != 1 {
		t.Fatal("summary not applied")
	}
	// Events for a workspace the TUI does not know are ignored.
	apply(t, s, "connection.state", wire.ConnectionEvent{Team: "T9", State: "connected"})
	apply(t, s, "conversation.updated", wire.ConversationEvent{Team: "T9", Conversation: wire.Conversation{ID: "C1"}})
	apply(t, s, "read.changed", wire.ReadEvent{Team: "T1", Conv: "C404", LastRead: tsB})
}

func TestDraftsAndTyping(t *testing.T) {
	s := newTestState(t)
	apply(t, s, "draft.changed", wire.DraftEvent{Team: "T1", Conv: "C1", Text: "half a thought"})
	apply(t, s, "draft.changed", wire.DraftEvent{Team: "T1", Conv: "C1", ThreadTS: tsA, Text: "in thread"})
	if s.Draft(target{"T1", "C1", ""}) != "half a thought" || s.Draft(target{"T1", "C1", tsA}) != "in thread" {
		t.Fatal("drafts not kept per conversation and thread")
	}
	apply(t, s, "typing", wire.TypingEvent{Team: "T1", Conv: "C1", User: "U1"})
	at := time.Unix(1700000400, 0)
	if got := s.Typing("T1", "C1", at.Add(2*time.Second)); !equal(got, []string{"U1"}) {
		t.Fatalf("typing = %v", got)
	}
	if got := s.Typing("T1", "C1", at.Add(TypingTimeout+time.Second)); len(got) != 0 {
		t.Fatalf("typing after the timeout = %v", got)
	}
	// A message from the typist ends their typing.
	apply(t, s, "typing", wire.TypingEvent{Team: "T1", Conv: "C1", User: "U1"})
	apply(t, s, "message.created", wire.MessageEvent{Team: "T1", Conv: "C1", Message: wire.Message{TS: tsA, Author: "U1"}})
	if got := s.Typing("T1", "C1", at); len(got) != 0 {
		t.Fatalf("typing after their message = %v", got)
	}
}

func TestApplyRejectsWhatDoesNotParse(t *testing.T) {
	s := newTestState(t)
	if err := s.Apply("message.created", json.RawMessage(`[`), time.Time{}); err == nil {
		t.Fatal("bad params accepted")
	}
	if err := s.Apply("no.such.topic", json.RawMessage(`{}`), time.Time{}); err != nil {
		t.Fatalf("unknown topics are ignored, got %v", err)
	}
}

func TestUsersAndEmoji(t *testing.T) {
	s := newTestState(t)
	s.PutUsers("T1", []wire.User{{ID: "U1", Name: "ana", DisplayName: "Ana"}, {ID: "U2", Name: "bo", RealName: "Bo Li"}, {ID: "U3", Name: "cy"}})
	for id, want := range map[string]string{"U1": "Ana", "U2": "Bo Li", "U3": "cy", "U4": ""} {
		if got := s.UserName("T1", id); got != want {
			t.Errorf("UserName(%s) = %q, want %q", id, got, want)
		}
	}
	s.SetEmoji("T1", map[string]string{"party-parrot": "https://emoji/pp.gif"})
	if _, ok := s.CustomEmoji("T1", "party-parrot"); !ok {
		t.Fatal("custom emoji not kept")
	}
	if _, ok := s.CustomEmoji("T2", "party-parrot"); ok {
		t.Fatal("custom emoji leaked across workspaces")
	}
}
