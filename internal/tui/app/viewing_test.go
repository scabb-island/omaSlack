package app

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func (h *harness) countCalls(method string) int {
	n := 0
	for _, m := range h.calls() {
		if m == method {
			n++
		}
	}
	return n
}

// The TUI tells the daemon what it shows, so the daemon does not notify
// it: the open conversation, the open thread, and whether the terminal
// has focus (true until the terminal says otherwise).
func TestViewingIsReported(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{
		"messages.replies": client.Result([]wire.Message{{TS: tsB, Text: "standup at 10?"}, {TS: tsC, ThreadTS: tsB, Text: "yes"}}),
	}), 120, 40)
	if slices.Contains(h.calls(), "ui.viewing") {
		t.Fatal("reported a view before anything was open")
	}
	h.keys("j", "enter")
	conv := h.m.openConv()
	if p := h.lastCall("ui.viewing"); p["team"] != h.m.team || p["conv"] != conv || p["focused"] != true || p["threadTS"] != nil {
		t.Fatalf("ui.viewing %v", p)
	}
	sent := h.countCalls("ui.viewing")
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	if h.countCalls("ui.viewing") != sent {
		t.Fatal("reported the same view again")
	}
	h.keys("k", "enter")
	if p := h.lastCall("ui.viewing"); p["threadTS"] != tsB {
		t.Fatalf("thread view %v", p)
	}
	h.send(tea.BlurMsg{})
	if p := h.lastCall("ui.viewing"); p["focused"] != false {
		t.Fatalf("blurred view %v", p)
	}
	h.send(tea.FocusMsg{})
	h.keys("esc")
	if p := h.lastCall("ui.viewing"); p["focused"] != true || p["threadTS"] != nil {
		t.Fatalf("view after closing the thread %v", p)
	}
	if !h.m.View().ReportFocus {
		t.Error("the TUI does not ask the terminal for focus reports")
	}
}

func TestViewingWithAnOlderDaemon(t *testing.T) {
	f := daemon(nil)
	delete(f.Handlers, "ui.viewing")
	h := newHarness(t, f, 120, 40)
	h.keys("j", "enter")
	h.keys("k", "enter")
	if n := h.countCalls("ui.viewing"); n != 1 || h.m.statusIsError {
		t.Fatalf("asked %d times, status %q", n, h.m.status)
	}
}
