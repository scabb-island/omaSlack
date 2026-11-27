package app

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func entry(team, title string, c wire.Conversation) finderEntry {
	return finderEntry{team: team, title: title, conv: c}
}

func titles(es []finderEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.team+" "+e.title)
	}
	return out
}

func TestRankConversations(t *testing.T) {
	entries := []finderEntry{
		entry("T1", "#random", wire.Conversation{Latest: tsA}),
		entry("T1", "#general", wire.Conversation{Latest: tsB}),
		entry("T1", "@Ana", wire.Conversation{Latest: tsA, Unread: true, Mentions: 1}),
		entry("T2", "#gen-eral", wire.Conversation{Latest: tsC, Unread: true}),
		entry("T2", "#general", wire.Conversation{Latest: tsA}),
		entry("T2", "~ops", wire.Conversation{Latest: tsC}),
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		// No query: mentions, then unread, then the most recent, then by name.
		{"", []string{"T1 @Ana", "T2 #gen-eral", "T2 ~ops", "T1 #general", "T2 #general", "T1 #random"}},
		// An exact name beats an unread partial match; ties go to the newest.
		{"general", []string{"T1 #general", "T2 #general", "T2 #gen-eral"}},
		// A sign restricts the kind and is not matched as text.
		{"~o", []string{"T2 ~ops"}},
		{"#a", []string{"T2 #gen-eral", "T1 #general", "T2 #general", "T1 #random"}},
		{"@", []string{"T1 @Ana"}},
		{"zz", nil},
	} {
		if got := titles(rankConversations(entries, tc.query)); !slices.Equal(got, tc.want) {
			t.Errorf("rank(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestFinderOpensAConversationInAnotherWorkspace(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{
		"conversations.list": func(p json.RawMessage) (any, error) {
			if strings.Contains(string(p), `"T2"`) {
				return []wire.Conversation{{ID: "C9", Kind: "channel", Name: "beta-news", Latest: tsA}, {ID: "D9", Kind: "im", User: "U9"}}, nil
			}
			return acmeConversations(), nil
		},
		"users.info": func(p json.RawMessage) (any, error) {
			if strings.Contains(string(p), `"T2"`) {
				return []wire.User{{ID: "U9", Name: "cy"}}, nil
			}
			return []wire.User{{ID: "U1", Name: "ana", DisplayName: "Ana"}, {ID: "U2", Name: "bo", RealName: "Bo Li"}}, nil
		},
	}), 120, 40)
	h.keys("ctrl+k")
	if p := h.lastCall("conversations.list"); p["team"] != "T2" {
		t.Fatalf("the finder did not load the other workspace: %v", p)
	}
	if !strings.Contains(h.screen(), "@cy") {
		t.Fatalf("the other workspace's DM is not named:\n%s", h.screen())
	}
	h.typeText("bnews")
	h.keys("enter")
	if h.m.team != "T2" || h.m.openConv() != "C9" || h.m.pick.open != nil {
		t.Fatalf("team %q, open %q, finder %v", h.m.team, h.m.openConv(), h.m.pick.open)
	}
	if p := h.lastCall("conversations.open"); p["team"] != "T2" || p["conv"] != "C9" {
		t.Fatalf("open %v", p)
	}
}

func TestFinderMovesAndCloses(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("ctrl+k", "down", "down", "up", "ctrl+n")
	if h.m.pick.open.cursor != 2 {
		t.Fatalf("cursor %d", h.m.pick.open.cursor)
	}
	h.typeText("j") // typing filters: j is text here, and the cursor goes back to the top
	if h.m.pick.open.query != "j" || h.m.pick.open.cursor != 0 {
		t.Fatalf("query %q cursor %d", h.m.pick.open.query, h.m.pick.open.cursor)
	}
	h.keys("backspace", "esc")
	if h.m.pick.open != nil || h.m.openConv() != "" {
		t.Fatalf("Esc did not close the finder")
	}
	h.keys("ctrl+k", "enter") // the top entry: @Ana, which has a mention
	if h.m.openConv() != "D1" {
		t.Fatalf("opened %q", h.m.openConv())
	}
}
