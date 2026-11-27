package app

import (
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Golden views (task 3.7): the screen, without ANSI styling, at fixed
// sizes, theme and clock. Rewrite them with
//
//	go test ./internal/tui/app -update
//
// and review the diff. The message pane shows what render.Messages
// draws, so render's changes (task 3.4) show up here too.

func summary(t1 wire.Counts) wire.Summary {
	return wire.Summary{Workspaces: map[string]wire.Counts{"T1": t1, "T2": {}}, Total: t1}
}

func TestGoldenEmpty(t *testing.T) {
	f := client.NewFake(map[string]any{"protocol": 1, "workspaces": []wire.Workspace{}}, map[string]client.Handler{
		"events.subscribe": client.Result(nil),
	})
	h := newHarness(t, f, 120, 40)
	h.golden("empty_120x40")
}

func TestGoldenWorkspace(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.event("unread.summary", summary(wire.Counts{DMs: 1, Mentions: 1, Channels: 1}))
	h.golden("workspace_120x40")
}

func TestGoldenOpenConversation(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("j", "enter") // Unread: @Ana, #general
	h.event("typing", wire.TypingEvent{Team: "T1", Conv: "C1", User: "U1"})
	h.golden("open_120x40")
}

func TestGoldenComposeInsert(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter", "i")
	h.typeText("on my way")
	h.golden("insert_120x40")
}

func TestGoldenHelp(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("?")
	h.golden("help_120x40")
}

func TestGoldenNarrow(t *testing.T) {
	h := newHarness(t, daemon(nil), 70, 30)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter")
	h.golden("narrow_70x30")
	h.keys("ctrl+b")
	h.golden("narrow_sidebar_70x30")
}

func TestGoldenConnectionLost(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.event("connection.state", wire.ConnectionEvent{Team: "T1", State: "disconnected"})
	h.golden("disconnected_120x40")
	h.send(DisconnectedMsg{})
	h.golden("daemon_gone_120x40")
}
