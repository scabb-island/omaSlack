package app

import (
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func TestConversationTitles(t *testing.T) {
	s := newTestState(t)
	s.PutUsers("T1", []wire.User{{ID: "U1", Name: "ana", DisplayName: "Ana"}})
	cases := []struct {
		conv wire.Conversation
		want string
	}{
		{wire.Conversation{ID: "C1", Kind: "channel", Name: "general"}, "#general"},
		{wire.Conversation{ID: "G1", Kind: "private", Name: "ops"}, "~ops"},
		{wire.Conversation{ID: "D1", Kind: "im", User: "U1"}, "@Ana"},
		{wire.Conversation{ID: "D2", Kind: "im", User: "U2"}, "@U2"}, // users.info has not told
		{wire.Conversation{ID: "D3", Kind: "im"}, "@D3"},             // daemon without the user field
		{wire.Conversation{ID: "G2", Kind: "mpim", Name: "mpdm-ana--bo--cy-1"}, "&ana, bo, cy"},
		{wire.Conversation{ID: "G3", Kind: "mpim"}, "&G3"},
	}
	for _, c := range cases {
		if got := s.Title("T1", c.conv); got != c.want {
			t.Errorf("Title(%+v) = %q, want %q", c.conv, got, c.want)
		}
	}
}

func TestSidebarSectionsUnreadFirst(t *testing.T) {
	s := newTestState(t)
	s.SetConversations("T1", []wire.Conversation{
		{ID: "C1", Kind: "channel", Name: "random"},
		{ID: "C2", Kind: "channel", Name: "general"},
		{ID: "C3", Kind: "channel", Name: "alerts", Latest: tsC, Unread: true},
		{ID: "D1", Kind: "im", User: "U1", Latest: tsA, Unread: true, Mentions: 2},
		{ID: "D2", Kind: "im", User: "U2"},
		{ID: "C4", Kind: "channel", Name: "dev", Latest: tsB, Unread: true},
		{ID: "G1", Kind: "private", Name: "ops", Mentions: 1},
	})
	var got []string
	for _, r := range s.Sidebar("T1") {
		if r.Header != "" {
			got = append(got, "["+r.Header+"]")
		} else {
			got = append(got, r.Title)
		}
	}
	want := []string{
		"[Unread]", "@U1", "~ops", "#alerts", "#dev",
		"[Channels]", "#general", "#random",
		"[Direct messages]", "@U2",
	}
	if !equal(got, want) {
		t.Fatalf("sidebar =\n%v\nwant\n%v", got, want)
	}
	if rows := s.Sidebar("T9"); len(rows) != 0 {
		t.Fatalf("unknown team rows = %v", rows)
	}
}
