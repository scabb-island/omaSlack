package app

import (
	"slices"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func TestRankEmoji(t *testing.T) {
	names := []string{"+1", "smile", "smiley", "tada", "partyparrot"}
	recent := []string{"tada", "smiley"}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		// No query: the recently used first, most recent first, then the rest in order.
		{"", []string{"tada", "smiley", "+1", "smile", "partyparrot"}},
		// A recent one wins among equal matches, not over a better one.
		{"smile", []string{"smile", "smiley"}},
		{"smi", []string{"smiley", "smile"}},
		{"party", []string{"partyparrot"}},
		{"zz", nil},
	} {
		if got := rankEmoji(names, recent, tc.query); !slices.Equal(got, tc.want) {
			t.Errorf("rankEmoji(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestReactionMethod(t *testing.T) {
	msg := wire.Message{Reactions: []wire.Reaction{{Emoji: "+1", Users: []string{"U2", "U1"}}}}
	for _, tc := range []struct{ emoji, self, want string }{
		{"+1", "U1", "reactions.remove"}, // already there: picking it again removes it
		{"+1", "U3", "reactions.add"},
		{"tada", "U1", "reactions.add"},
		{"+1", "", "reactions.add"}, // who the user is is unknown: always add
	} {
		if got := reactionMethod(msg, tc.emoji, tc.self); got != tc.want {
			t.Errorf("reactionMethod(%s, %q) = %s, want %s", tc.emoji, tc.self, got, tc.want)
		}
	}
}

func TestRememberEmoji(t *testing.T) {
	got := rememberEmoji([]string{"a", "b", "c"}, "b")
	if !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Fatalf("remember = %v", got)
	}
	var many []string
	for _, n := range "abcdefghijklmnop" {
		many = rememberEmoji(many, string(n))
	}
	if len(many) != recentEmoji || many[0] != "p" {
		t.Fatalf("remember keeps %v", many)
	}
}

// openGeneral opens #general with its messages and selects "standup at 10?".
func openGeneral(t *testing.T, extra map[string]client.Handler) *harness {
	t.Helper()
	handlers := map[string]client.Handler{
		"reactions.add":    client.Result(nil),
		"reactions.remove": client.Result(nil),
		"emoji.list":       client.Result(wire.EmojiList{Emoji: map[string]string{"partyparrot": "https://emoji/partyparrot.gif"}}),
	}
	for k, v := range extra {
		handlers[k] = v
	}
	h := newHarness(t, daemon(handlers), 120, 40)
	h.keys("j", "enter", "k")
	if h.m.msgSel != tsB {
		t.Fatalf("selected %q", h.m.msgSel)
	}
	return h
}

func TestReactionPickerAddsTheReactionAndRemembersIt(t *testing.T) {
	h := openGeneral(t, nil)
	h.keys("r")
	h.typeText("tada")
	h.keys("enter")
	if p := h.lastCall("reactions.add"); p["team"] != "T1" || p["conv"] != "C1" || p["ts"] != tsB || p["emoji"] != "tada" {
		t.Fatalf("reactions.add %v", p)
	}
	h.keys("r")
	if !strings.Contains(h.screen(), "▌🎉 :tada:") {
		t.Fatalf("tada is not first:\n%s", h.screen())
	}
	h.typeText("partyp")
	h.keys("enter")
	if p := h.lastCall("reactions.add"); p["emoji"] != "partyparrot" {
		t.Fatalf("custom emoji %v", p)
	}
}

func TestReactCommand(t *testing.T) {
	h := openGeneral(t, nil)
	h.keys(":")
	h.typeText("react :+1:")
	h.keys("enter")
	if p := h.lastCall("reactions.add"); p["emoji"] != "+1" || p["ts"] != tsB {
		t.Fatalf("reactions.add %v", p)
	}
	h.keys(":")
	h.typeText("react")
	h.keys("enter")
	if h.m.status != "Usage: :react <emoji>" {
		t.Fatalf("status %q", h.m.status)
	}
}

func TestReactInTheThreadAndWithoutMessages(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{
		"reactions.add":    client.Result(nil),
		"messages.replies": client.Result([]wire.Message{{TS: tsB, Author: "U1"}, {TS: "1700000250.000000", ThreadTS: tsB, Author: "U2", Text: "yes"}}),
	}), 120, 40)
	h.keys("r")
	if h.m.status != "No message to react to." || h.m.pick.open != nil {
		t.Fatalf("status %q", h.m.status)
	}
	h.keys("j", "enter", "k", "enter") // the thread of "standup at 10?", focused
	h.keys(":")
	h.typeText("react eyes")
	h.keys("enter")
	if p := h.lastCall("reactions.add"); p["ts"] != "1700000250.000000" || p["emoji"] != "eyes" {
		t.Fatalf("reactions.add %v", p)
	}
}
