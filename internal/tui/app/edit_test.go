package app

import (
	"encoding/json"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func editDaemon() *client.Fake {
	f := composeDaemon()
	f.Handlers["conversations.open"] = client.Result(openResult{
		Messages: append([]wire.Message{{TS: tsC, Author: "U2", Text: "ping <@U1> in <#C2>"}}, generalMessages()[1:]...),
		Draft:    "half a thought",
	})
	f.Handlers["messages.edit"] = func(raw json.RawMessage) (any, error) {
		var p struct{ TS, Text string }
		err := json.Unmarshal(raw, &p)
		return wire.Message{TS: p.TS, Author: "U2", Text: p.Text, Edited: "1700000600.000000"}, err
	}
	f.Handlers["messages.delete"] = client.Result(nil)
	return f
}

// inGeneral opens #general, its messages pane focused.
func inGeneral(t *testing.T, f *client.Fake) *harness {
	t.Helper()
	h := newHarness(t, f, 120, 40)
	h.keys(":")
	h.typeText("open general")
	h.keys("enter")
	return h
}

func TestEditLoadsTheMessageWithNamesAndSendsWireSyntax(t *testing.T) {
	h := inGeneral(t, editDaemon())
	h.keys("e")
	if h.m.mode != insertMode || h.m.compose.Value() != "ping @Ana in #random" {
		t.Fatalf("mode %v, compose %q", h.m.mode, h.m.compose.Value())
	}
	h.typeText("!")
	h.keys("enter")
	p := h.lastCall("messages.edit")
	if p["ts"] != tsC || p["conv"] != "C1" || p["text"] != "ping <@U1> in <#C2>!" {
		t.Errorf("messages.edit params = %v", p)
	}
	if h.m.mode != normalMode || h.m.compose.Value() != "half a thought" {
		t.Errorf("after the edit: mode %v, compose %q", h.m.mode, h.m.compose.Value())
	}
	if h.count("messages.send") != 0 {
		t.Error("an edit was sent as a new message")
	}
}

func TestEditEscCancelsAndKeepsTheDraft(t *testing.T) {
	h := inGeneral(t, editDaemon())
	h.keys("k", "e", "esc")
	if h.m.mode != normalMode || h.m.compose.Value() != "half a thought" || h.m.cx.edit != nil {
		t.Errorf("mode %v, compose %q", h.m.mode, h.m.compose.Value())
	}
	if h.count("messages.edit")+h.count("drafts.set") != 0 {
		t.Errorf("calls: %v", h.calls())
	}
}

func TestEditFailureReturnsToCompose(t *testing.T) {
	f := editDaemon()
	f.Handlers["messages.edit"] = failing("cant_update_message")
	h := inGeneral(t, f)
	h.keys("e")
	h.typeText("?")
	h.keys("enter")
	if h.m.mode != insertMode || h.m.compose.Value() != "ping @Ana in #random?" || h.m.cx.edit == nil {
		t.Errorf("mode %v, compose %q", h.m.mode, h.m.compose.Value())
	}
	if !h.m.statusIsError {
		t.Errorf("status %q", h.m.status)
	}
}

func TestDeleteAsksFirst(t *testing.T) {
	h := inGeneral(t, editDaemon())
	h.keys("k", "D")
	if h.m.status != "Delete this message? y/n" {
		t.Fatalf("status %q", h.m.status)
	}
	h.keys("n")
	if h.count("messages.delete") != 0 {
		t.Fatal("n deleted")
	}
	h.keys("D", "y")
	if p := h.lastCall("messages.delete"); p["ts"] != tsB || p["conv"] != "C1" {
		t.Errorf("messages.delete params = %v", p)
	}
}

func TestReplyOpensTheThreadAndWritesInIt(t *testing.T) {
	h := inGeneral(t, composeDaemon())
	h.keys("k", "R")
	if h.m.thread != tsB || h.m.focus != threadPane || h.m.mode != insertMode {
		t.Fatalf("thread %q, focus %v, mode %v", h.m.thread, h.m.focus, h.m.mode)
	}
	h.typeText("yes")
	h.keys("enter")
	if p := h.lastCall("messages.send"); p["threadTS"] != tsB || p["text"] != "yes" {
		t.Errorf("messages.send params = %v", p)
	}
}

func TestActionsNeedAMessage(t *testing.T) {
	h := newHarness(t, composeDaemon(), 120, 40)
	for _, k := range []string{"e", "D", "R"} {
		h.keys(k)
		if !h.m.statusIsError || h.m.cx.deleting != nil || h.m.mode != normalMode {
			t.Errorf("%s with no message: status %q", k, h.m.status)
		}
	}
}

func TestGoldenEditInProgress(t *testing.T) {
	h := inGeneral(t, editDaemon())
	h.keys("e")
	h.typeText(" now")
	h.golden("edit_120x40")
}
