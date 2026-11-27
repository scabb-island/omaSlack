package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func TestStartSubscribesAndLoadsTheFirstWorkspace(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	sub := h.lastCall("events.subscribe")
	if topics, _ := sub["topics"].([]any); len(topics) != len(subscribedTopics) {
		t.Fatalf("subscribed to %v", sub)
	}
	if p := h.lastCall("conversations.list"); p["team"] != "T1" {
		t.Fatalf("conversations.list %v", p)
	}
	if p := h.lastCall("users.info"); p["team"] != "T1" || len(p["ids"].([]any)) != 2 {
		t.Fatalf("users.info %v", p)
	}
	h.lastCall("emoji.list")
	if !strings.Contains(h.screen(), "@Bo Li") {
		t.Fatalf("DM not named from users.info:\n%s", h.screen())
	}
}

func TestOlderDaemonsWithoutUsersInfo(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{"users.info": notFound, "emoji.list": notFound}), 120, 40)
	if !strings.Contains(h.screen(), "@U2") || h.m.status != "" {
		t.Fatalf("status %q, screen:\n%s", h.m.status, h.screen())
	}
	h.keys("j", "enter") // opens #general; its authors are not asked for
	if n := h.count("users.info"); n != 1 {
		t.Fatalf("users.info asked %d times after the daemon said it has none", n)
	}
}

func TestOpenMarksReadOnceAndFollowsNewMessages(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("j", "enter")
	if p := h.lastCall("conversations.open"); p["team"] != "T1" || p["conv"] != "C1" {
		t.Fatalf("open %v", p)
	}
	if p := h.lastCall("conversations.markRead"); p["ts"] != tsC {
		t.Fatalf("markRead %v", p)
	}
	// Published twice, shown once, and marked read once.
	created := wire.MessageEvent{Team: "T1", Conv: "C1", Message: wire.Message{TS: "1700000350.000000", Author: "U2", Text: "new"}}
	h.event("message.created", created)
	h.event("message.created", created)
	if n := len(h.m.messages()); n != 4 {
		t.Fatalf("%d messages", n)
	}
	if n := h.count("conversations.markRead"); n != 2 {
		t.Fatalf("markRead called %d times, want 2", n)
	}
	// Reading older messages stops marking; G goes back to the newest.
	h.keys("l", "k")
	h.event("message.created", wire.MessageEvent{Team: "T1", Conv: "C1", Message: wire.Message{TS: "1700000360.000000", Text: "newer"}})
	if n := h.count("conversations.markRead"); n != 2 {
		t.Fatalf("marked read while reading older messages (%d)", n)
	}
	h.keys("G")
	if p := h.lastCall("conversations.markRead"); p["ts"] != "1700000360.000000" {
		t.Fatalf("markRead after G %v", p)
	}
}

func TestUpAtTheTopLoadsOlderHistory(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("j", "enter", "k", "k", "k")
	if p := h.lastCall("messages.history"); p["before"] != tsA || p["conv"] != "C1" {
		t.Fatalf("history %v", p)
	}
	if h.m.msgSel != tsA {
		t.Fatalf("selection %q", h.m.msgSel)
	}
}

func TestDraftsAreRestoredAndSavedOnLeave(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{
		"conversations.open": func(p json.RawMessage) (any, error) {
			if strings.Contains(string(p), `"C1"`) {
				return openResult{Messages: generalMessages(), Draft: "from the window"}, nil
			}
			return openResult{}, nil
		},
	}), 120, 40)
	h.keys("j", "enter")
	if got := h.m.compose.Value(); got != "from the window" {
		t.Fatalf("draft not restored: %q", got)
	}
	h.keys("i")
	h.typeText("!")
	h.keys("esc")
	if p := h.lastCall("drafts.set"); p["text"] != "from the window!" || p["conv"] != "C1" {
		t.Fatalf("drafts.set on Esc %v", p)
	}
	// Another client edits the draft: NORMAL mode shows it.
	h.event("draft.changed", wire.DraftEvent{Team: "T1", Conv: "C1", Text: "edited elsewhere"})
	if got := h.m.compose.Value(); got != "edited elsewhere" {
		t.Fatalf("draft.changed not shown: %q", got)
	}
	// Leaving with unsaved text saves it and closes the conversation.
	h.keys("i")
	h.typeText("?")
	h.keys("esc", "h", "j", "enter")
	if p := h.lastCall("conversations.close"); p["conv"] != "C1" {
		t.Fatalf("close %v", p)
	}
	if n := h.count("drafts.set"); n != 2 {
		t.Fatalf("drafts.set %d times", n)
	}
	if h.m.compose.Value() != "" {
		t.Fatalf("compose kept the other conversation's draft: %q", h.m.compose.Value())
	}
}

func TestSendClearsComposeAndRestoresOnFailure(t *testing.T) {
	sent := wire.Message{TS: "1700000390.000000", Author: "U1", Text: "hi all"}
	h := newHarness(t, daemon(map[string]client.Handler{"messages.send": client.Result(sent)}), 120, 40)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter", "i")
	h.typeText("hi all")
	h.keys("enter")
	if p := h.lastCall("messages.send"); p["text"] != "hi all" || p["conv"] != "C1" || p["threadTS"] != nil {
		t.Fatalf("send %v", p)
	}
	if h.m.compose.Value() != "" || h.m.mode != insertMode {
		t.Fatalf("after send: compose %q mode %v", h.m.compose.Value(), h.m.mode)
	}
	if n := h.count("typing.send"); n != 1 {
		t.Fatalf("typing.send %d times, want 1 (throttled)", n)
	}
	h.keys("enter") // nothing to send
	if n := h.count("messages.send"); n != 1 {
		t.Fatalf("sent an empty message")
	}

	h.f.Handlers["messages.send"] = failing("slack said no")
	h.typeText("again")
	h.keys("alt+enter")
	h.typeText("x")
	h.keys("enter")
	if got := h.m.compose.Value(); got != "again\nx" || !h.m.statusIsError || !strings.Contains(h.m.status, "slack said no") {
		t.Fatalf("failed send: compose %q status %q", got, h.m.status)
	}
}

func TestThreads(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{
		"messages.replies": client.Result([]wire.Message{{TS: tsB, Text: "standup at 10?"}, {TS: tsC, ThreadTS: tsB, Text: "yes"}}),
	}), 120, 40)
	h.keys("j", "enter", "k", "enter")
	if p := h.lastCall("messages.replies"); p["threadTS"] != tsB {
		t.Fatalf("replies %v", p)
	}
	if p := h.lastCall("drafts.get"); p["threadTS"] != tsB {
		t.Fatalf("drafts.get %v", p)
	}
	if h.m.focus != threadPane || len(h.m.replies()) != 2 {
		t.Fatalf("focus %v replies %v", h.m.focus, h.m.replies())
	}
	h.keys("i")
	h.typeText("ok")
	h.keys("enter", "esc")
	if p := h.lastCall("messages.send"); p["threadTS"] != tsB {
		t.Fatalf("reply %v", p)
	}
	if !strings.Contains(h.screen(), "Thread") {
		t.Fatal("no thread pane")
	}
	h.keys("esc")
	if h.m.thread != "" || h.m.focus != messagesPane || strings.Contains(h.screen(), "─ Thread ─") {
		t.Fatal("Esc did not close the thread")
	}
}

func TestPanesAndSidebar(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("l")
	if h.m.focus != messagesPane {
		t.Fatalf("l: focus %v", h.m.focus)
	}
	h.keys("l") // no thread: stays
	h.keys("tab")
	if h.m.focus != sidebarPane {
		t.Fatalf("tab wraps: focus %v", h.m.focus)
	}
	h.keys("shift+tab", "h")
	if h.m.focus != sidebarPane {
		t.Fatalf("shift+tab, h: focus %v", h.m.focus)
	}
	h.keys(">", ">")
	if h.m.sidebarWidth != defaultSidebarWidth+2*sidebarStep {
		t.Fatalf("wider: %d", h.m.sidebarWidth)
	}
	for range 20 {
		h.keys("<")
	}
	if h.m.sidebarWidth != minSidebarWidth {
		t.Fatalf("narrower: %d", h.m.sidebarWidth)
	}
	h.keys("ctrl+b")
	if h.m.sidebarVisible() || h.m.focus != messagesPane {
		t.Fatal("ctrl+b did not hide the sidebar")
	}
	h.send(tea.WindowSizeMsg{Width: 70, Height: 30})
	h.keys("ctrl+b")
	if !h.m.sidebarVisible() {
		t.Fatal("ctrl+b did not show the sidebar")
	}
}

func TestWorkspaceSwitching(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("2")
	if h.m.team != "T2" || h.lastCall("conversations.list")["team"] != "T2" {
		t.Fatalf("team %s", h.m.team)
	}
	if !strings.Contains(h.screen(), "Beta") {
		t.Fatal("status bar does not name Beta")
	}
	h.keys("1")
	if n := h.count("conversations.list"); n != 2 {
		t.Fatalf("conversations.list %d times: the loaded workspace was asked again", n)
	}
	h.keys("9")
	if !h.m.statusIsError || !strings.Contains(h.m.status, "no workspace 9") {
		t.Fatalf("status %q", h.m.status)
	}
	h.keys("1") // the same workspace: nothing
}

func TestCommandLine(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys(":")
	if !strings.Contains(h.screen(), ":█") {
		t.Fatal("no command line")
	}
	h.typeText("nope")
	h.keys("enter")
	if !strings.Contains(h.m.status, "Not a command: nope") {
		t.Fatalf("status %q", h.m.status)
	}
	h.keys(":", "x", "backspace", "backspace")
	if h.m.mode != normalMode {
		t.Fatal("backspace on an empty line did not leave COMMAND mode")
	}
	h.keys(":", "esc", ":", "enter")
	h.keys(":")
	h.typeText("open")
	h.keys("enter")
	if !strings.Contains(h.m.status, "Usage") {
		t.Fatalf("status %q", h.m.status)
	}
	h.keys(":")
	h.typeText("open #nowhere")
	h.keys("enter")
	if !strings.Contains(h.m.status, "No conversation") {
		t.Fatalf("status %q", h.m.status)
	}
	h.keys(":")
	h.typeText("open o") // prefix of ~ops
	h.keys("enter")
	if h.m.openConv() != "G1" {
		t.Fatalf("open o opened %q", h.m.openConv())
	}
	h.keys(":")
	h.typeText("help")
	h.keys("enter")
	if !h.m.help {
		t.Fatal(":help")
	}
	h.keys("x")
	if h.m.help {
		t.Fatal("a key did not close help")
	}
	h.keys(":", "q", "enter")
	if !h.quit {
		t.Fatal(":q did not quit")
	}
}

func TestQuitSavesTheDraft(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("j", "enter", "i")
	h.typeText("unsent")
	h.keys("esc")
	h.m.compose.SetValue("unsent, longer") // as if edited without Esc
	h.keys("q")
	if !h.quit || h.lastCall("drafts.set")["text"] != "unsent, longer" {
		t.Fatalf("quit %v, calls %v", h.quit, h.calls())
	}
}

func TestInsertNeedsAConversation(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("i")
	if h.m.mode != normalMode || !strings.Contains(h.m.status, "Open a conversation") {
		t.Fatalf("mode %v status %q", h.m.mode, h.m.status)
	}
}

func TestFocusFromAnotherClient(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.event("ui.focus", wire.FocusEvent{Team: "T2", Conv: "C9"})
	if h.m.team != "T2" || h.m.openConv() != "C9" {
		t.Fatalf("team %s conv %s", h.m.team, h.m.openConv())
	}
	h.event("ui.focus", wire.FocusEvent{Team: "T404", Conv: "C9"})
	h.send(NoteMsg{Method: "ui.focus", Params: json.RawMessage(`[`)})
	if !h.m.statusIsError {
		t.Fatal("bad ui.focus not reported")
	}
	h.send(NoteMsg{Method: "message.created", Params: json.RawMessage(`[`)})
	if !strings.Contains(h.m.status, "message.created") {
		t.Fatalf("bad event status %q", h.m.status)
	}
}

func TestThemeReload(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	th := theme.Default()
	th.Accent = lipgloss.Color("#ff0000")
	h.send(ThemeMsg(th))
	if !strings.Contains(h.m.View().Content, "38;2;255;0;0") {
		t.Fatal("the new accent is not drawn")
	}
}

func TestBadHello(t *testing.T) {
	f := client.NewFake(nil, map[string]client.Handler{"events.subscribe": client.Result(nil)})
	f.HelloResult = json.RawMessage(`{"workspaces":"no"}`)
	h := newHarness(t, f, 80, 24)
	if !h.m.statusIsError || !strings.Contains(h.screen(), "No workspaces") {
		t.Fatalf("status %q", h.m.status)
	}
}

func TestTinyTerminal(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.send(tea.WindowSizeMsg{Width: 0, Height: 0})
	if h.screen() != "\n" {
		t.Fatalf("screen %q", h.screen())
	}
}

func TestForward(t *testing.T) {
	f := client.NewFake(hello(), nil)
	themes := make(chan theme.Theme, 1)
	got := make(chan tea.Msg, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go forward(ctx, Options{Client: f, Themes: themes}, func(m tea.Msg) { got <- m })

	f.Push("typing", wire.TypingEvent{Team: "T1"})
	if m := <-got; m.(NoteMsg).Method != "typing" {
		t.Fatalf("got %#v", m)
	}
	themes <- theme.Default()
	if _, ok := (<-got).(ThemeMsg); !ok {
		t.Fatal("no theme")
	}
	close(themes)
	_ = f.Close()
	if _, ok := (<-got).(DisconnectedMsg); !ok {
		t.Fatal("no disconnect")
	}
	select {
	case m := <-got:
		t.Fatalf("more after the end: %#v", m)
	case <-time.After(50 * time.Millisecond):
	}
	_ = rpcclient.Notification{}
}
