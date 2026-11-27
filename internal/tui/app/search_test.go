package app

import (
	"encoding/json"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// A month before the test clock, in #random (C2), and a reply in a thread
// of #general (C1).
const (
	tsOld      = "1697400000.000100"
	tsOldReply = "1697400100.000200"
)

func searchHits() []wire.SearchHit {
	return []wire.SearchHit{
		{Conv: "C2", Message: wire.Message{TS: tsOld, Author: "U1", Text: "the *standup* moved to 10"}},
		{Conv: "C1", Message: wire.Message{TS: tsOldReply, ThreadTS: tsB, Author: "U2", Text: "standup notes are in the doc"}},
		// A bot shows the name Slack put on its message, not its B… ID.
		{Conv: "C1", Message: wire.Message{TS: tsOld, Author: "B9", AuthorName: "deploybot", Text: "standup bot reminder"}},
	}
}

func TestJustAfter(t *testing.T) {
	for ts, want := range map[string]string{
		"1700000100.000100": "1700000100.000101",
		"1700000100.999999": "1700000101.000000",
	} {
		if got := justAfter(ts); got != want {
			t.Errorf("justAfter(%s) = %s, want %s", ts, got, want)
		}
	}
}

func TestSearchListsHitsAndJumpsToAMonthOldMessage(t *testing.T) {
	// The cache starts at the newest messages: two pages come back empty
	// (the daemon loads older pages) before the hit's page arrives.
	pages := 0
	h := newHarness(t, daemon(map[string]client.Handler{
		"search.messages": client.Result(searchHits()),
		"messages.history": func(json.RawMessage) (any, error) {
			pages++
			if pages < 3 {
				return []wire.Message{}, nil
			}
			return []wire.Message{{TS: tsOld, Author: "U1", Text: "the *standup* moved to 10"}, {TS: "1697300000.000000", Text: "older"}}, nil
		},
	}), 120, 40)
	h.keys("/")
	h.typeText("standup")
	h.keys("enter")
	if p := h.lastCall("search.messages"); p["team"] != "T1" || p["query"] != "standup" {
		t.Fatalf("search %v", p)
	}
	if !strings.Contains(h.screen(), "#random · Ana · 2023-10-15") || !strings.Contains(h.screen(), "· deploybot ·") {
		t.Fatalf("results:\n%s", h.screen())
	}
	h.keys("j", "k", "enter")
	if p := h.lastCall("conversations.open"); p["conv"] != "C2" {
		t.Fatalf("open %v", p)
	}
	if p := h.lastCall("messages.history"); p["conv"] != "C2" || p["before"] != "1697400000.000101" || pages != 3 {
		t.Fatalf("history %v after %d pages", p, pages)
	}
	if h.m.msgSel != tsOld || h.m.focus != messagesPane || h.m.pick.open != nil {
		t.Fatalf("selection %q focus %v", h.m.msgSel, h.m.focus)
	}
	if h.count("conversations.markRead") != 0 {
		t.Fatal("jumping to an old message marked the conversation read")
	}
}

func TestSearchJumpsToAReplyInItsThread(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{
		"search.messages":  client.Result(searchHits()),
		"messages.history": client.Result([]wire.Message{{TS: tsB, Author: "U1", Text: "standup at 10?"}}),
	}), 120, 40)
	h.keys(":")
	h.typeText("search standup")
	h.keys("enter", "j", "enter")
	if h.m.openConv() != "C1" || h.m.msgSel != tsB || h.m.thread != tsB || h.m.threadSel != tsOldReply {
		t.Fatalf("conv %q sel %q thread %q reply %q", h.m.openConv(), h.m.msgSel, h.m.thread, h.m.threadSel)
	}
	if p := h.lastCall("messages.replies"); p["threadTS"] != tsB {
		t.Fatalf("replies %v", p)
	}
}

func TestSearchGivesUpAfterJumpPages(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{"search.messages": client.Result(searchHits())}), 120, 40)
	h.keys(":")
	h.typeText("search standup")
	h.keys("enter", "enter")
	if n := h.count("messages.history"); n != jumpPages {
		t.Fatalf("history asked %d times", n)
	}
	if !strings.Contains(h.m.status, "Could not reach") {
		t.Fatalf("status %q", h.m.status)
	}
}

func TestSearchWithoutTheCapability(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{
		"search.messages": func(json.RawMessage) (any, error) {
			return nil, &rpc.Error{Code: rpc.CodeUnsupportedCapability, Message: "unsupported capability: search"}
		},
	}), 120, 40)
	h.keys("/")
	h.typeText("x")
	h.keys("enter")
	if h.m.status != "Search is not available for this workspace." || !h.m.statusIsError || h.m.pick.open != nil {
		t.Fatalf("status %q", h.m.status)
	}
}

func TestSearchWithoutResultsOrQuery(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{"search.messages": client.Result([]wire.SearchHit{})}), 120, 40)
	h.keys(":")
	h.typeText("search nothing")
	h.keys("enter")
	if h.m.status != "No messages match nothing." || h.m.pick.open != nil {
		t.Fatalf("status %q", h.m.status)
	}
	h.keys(":")
	h.typeText("search")
	h.keys("enter")
	if h.m.status != "Usage: :search <query>" || h.count("search.messages") != 1 {
		t.Fatalf("status %q", h.m.status)
	}
	h.keys("/", "esc") // / types "search " on the command line; Esc leaves it
	if h.m.mode != normalMode {
		t.Fatalf("mode %v", h.m.mode)
	}
}

func TestSearchFailureShowsTheError(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{"search.messages": failing("slack: ratelimited")}), 120, 40)
	h.keys(":")
	h.typeText("search x")
	h.keys("enter")
	if !strings.Contains(h.m.status, "ratelimited") {
		t.Fatalf("status %q", h.m.status)
	}
}
