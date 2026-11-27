package app

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// people is the workspace's directory as users.search answers it: the
// users whose names start with q.
func people(raw json.RawMessage) (any, error) {
	var p struct{ Q string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	all := []wire.User{
		{ID: "U1", Name: "ana", DisplayName: "Ana", RealName: "Ana Perez"},
		{ID: "U3", Name: "andres", RealName: "Andres Gil"},
		{ID: "U2", Name: "bo", RealName: "Bo Li"},
	}
	var out []wire.User
	for _, u := range all {
		if strings.HasPrefix(u.Name, strings.ToLower(p.Q)) {
			out = append(out, u)
		}
	}
	return out, nil
}

func composeDaemon() *client.Fake {
	return daemon(map[string]client.Handler{
		"users.search": people,
		"emoji.list":   client.Result(wire.EmojiList{Emoji: map[string]string{"tacocat": "https://x/tacocat.png"}}),
		"messages.send": func(raw json.RawMessage) (any, error) {
			var p struct{ Text string }
			err := json.Unmarshal(raw, &p)
			return wire.Message{TS: "1700000500.000100", Author: "U9", Text: p.Text}, err
		},
	})
}

// writing opens #general and enters INSERT.
func writing(t *testing.T, f *client.Fake) *harness {
	t.Helper()
	h := newHarness(t, f, 120, 40)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter", "i")
	return h
}

func TestMentionSendsWireSyntax(t *testing.T) {
	h := writing(t, composeDaemon())
	h.typeText("hi @an")
	if got := h.lastCall("users.search"); got["q"] != "an" || got["team"] != "T1" {
		t.Errorf("users.search params = %v", got)
	}
	if !h.m.popupOpen() || len(h.m.cx.items) != 2 {
		t.Fatalf("popup = %+v", h.m.cx.items)
	}
	h.keys("tab")
	if got := h.m.compose.Value(); got != "hi @Ana " {
		t.Errorf("compose shows %q", got)
	}
	h.typeText("ok")
	h.keys("enter")
	if got := h.lastCall("messages.send")["text"]; got != "hi <@U1> ok" {
		t.Errorf("sent %q", got)
	}
}

func TestPopupMovesAndEnterAccepts(t *testing.T) {
	h := writing(t, composeDaemon())
	h.typeText("@an")
	h.send(tea.KeyPressMsg{Code: tea.KeyDown})
	h.send(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	h.send(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	h.keys("enter")
	if got := h.m.compose.Value(); got != "@Andres Gil " {
		t.Errorf("compose shows %q", got)
	}
	if h.count("messages.send") != 0 {
		t.Error("Enter on the popup sent the message")
	}
}

func TestEscDismissesThenLeavesInsert(t *testing.T) {
	h := writing(t, composeDaemon())
	h.typeText("@an")
	h.keys("esc")
	if h.m.popupOpen() || h.m.mode != insertMode {
		t.Fatalf("first Esc: popup %v, mode %v", h.m.popupOpen(), h.m.mode)
	}
	h.typeText("d")
	if h.m.popupOpen() {
		t.Error("the dismissed popup came back on the same word")
	}
	h.keys("esc")
	if h.m.mode != normalMode {
		t.Errorf("second Esc: mode %v", h.m.mode)
	}
}

func TestStaleSearchIsDropped(t *testing.T) {
	h := writing(t, composeDaemon())
	h.typeText("@")
	_, stale := h.m.Update(keyMsg("a")) // its debounce has not fired yet
	h.typeText("n")
	before := h.count("users.search")
	h.run(stale)
	if h.count("users.search") != before {
		t.Error("a debounce older than the last key still searched")
	}
	if q := h.lastCall("users.search")["q"]; q != "an" {
		t.Errorf("searched %q", q)
	}
}

func TestChannelCompletion(t *testing.T) {
	h := writing(t, composeDaemon())
	h.typeText("see #ra")
	h.keys("tab")
	h.keys("enter")
	if got := h.lastCall("messages.send")["text"]; got != "see <#C2> " {
		t.Errorf("sent %q", got)
	}
}

func TestEmojiCompletion(t *testing.T) {
	h := writing(t, composeDaemon())
	h.typeText("yum :tacoc")
	if len(h.m.cx.items) != 1 || h.m.cx.items[0].text != ":tacocat:" {
		t.Fatalf("custom emoji not offered: %+v", h.m.cx.items)
	}
	h.keys("tab")
	if got := h.m.compose.Value(); got != "yum :tacocat: " {
		t.Errorf("compose shows %q", got)
	}
}

func TestSearchUnsupportedIsQuiet(t *testing.T) {
	f := composeDaemon()
	f.Handlers["users.search"] = notFound
	h := writing(t, f)
	h.typeText("@an")
	h.typeText("d")
	if h.m.status != "" || h.count("users.search") != 1 {
		t.Errorf("status %q after %d searches", h.m.status, h.count("users.search"))
	}
}

func TestGoldenMentionPopup(t *testing.T) {
	h := writing(t, composeDaemon())
	h.typeText("thanks @an")
	h.golden("mention_popup_120x40")
}
