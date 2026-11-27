package app

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/tui/client"
	"gitlab.com/scabb-island/omaslack/internal/tui/image"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// clock is the fixed time every test runs at: 2023-11-14 22:20 UTC, a
// little after the test messages.
var clock = time.Unix(1700000400, 0).UTC()

// harness drives a Model the way bubbletea does, without a terminal: it
// runs every command synchronously and feeds its message back.
type harness struct {
	t    *testing.T
	f    *client.Fake
	m    *Model
	quit bool
}

func newHarness(t *testing.T, f *client.Fake, w, h int) *harness {
	t.Helper()
	m := New(Options{Client: f, Theme: theme.Default(), Images: image.HalfBlock, Now: func() time.Time { return clock }, Loc: time.UTC}).(*Model)
	hs := &harness{t: t, f: f, m: m}
	hs.run(m.Init())
	hs.send(tea.WindowSizeMsg{Width: w, Height: h})
	return hs
}

// send delivers msg and everything its commands answer.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		msg, queue = queue[0], queue[1:]
		if _, ok := msg.(tea.QuitMsg); ok {
			h.quit = true
			continue
		}
		_, cmd := h.m.Update(msg)
		queue = append(queue, h.exec(cmd)...)
	}
}

func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	for _, msg := range h.exec(cmd) {
		h.send(msg)
	}
}

// exec runs cmd, flattening batches; a command that blocks fails the test.
func (h *harness) exec(cmd tea.Cmd) []tea.Msg {
	h.t.Helper()
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(2 * time.Second):
		h.t.Fatal("a command blocked")
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, h.exec(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// keys types each key: a single character, or a name like "enter".
func (h *harness) keys(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(keyMsg(k))
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	named := map[string]tea.Key{
		"enter": {Code: tea.KeyEnter}, "esc": {Code: tea.KeyEscape}, "tab": {Code: tea.KeyTab},
		"backspace": {Code: tea.KeyBackspace}, "ctrl+b": {Code: 'b', Mod: tea.ModCtrl},
		"ctrl+k": {Code: 'k', Mod: tea.ModCtrl}, "alt+enter": {Code: tea.KeyEnter, Mod: tea.ModAlt},
		"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift}, "space": {Code: tea.KeySpace, Text: " "},
	}
	if k, ok := named[k]; ok {
		return tea.KeyPressMsg(k)
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

// typeText types s in INSERT mode, one key per character.
func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		if r == ' ' {
			h.keys("space")
			continue
		}
		h.keys(string(r))
	}
}

// event pushes a daemon event and delivers it.
func (h *harness) event(topic string, params any) {
	h.t.Helper()
	b, err := json.Marshal(params)
	if err != nil {
		h.t.Fatal(err)
	}
	h.send(NoteMsg{Method: topic, Params: b})
}

// calls lists the methods called, in order, leaving events.subscribe out.
func (h *harness) calls() []string {
	var out []string
	for _, c := range h.f.Calls() {
		out = append(out, c.Method)
	}
	return out
}

// lastCall is the params of the last call to method.
func (h *harness) lastCall(method string) map[string]any {
	h.t.Helper()
	calls := h.f.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Method == method {
			var p map[string]any
			if err := json.Unmarshal(calls[i].Params, &p); err != nil {
				h.t.Fatal(err)
			}
			return p
		}
	}
	h.t.Fatalf("no call to %s in %v", method, h.calls())
	return nil
}

func (h *harness) count(method string) int {
	n := 0
	for _, c := range h.calls() {
		if c == method {
			n++
		}
	}
	return n
}

// screen is the view without ANSI styling and trailing blanks, as the
// golden files hold it.
func (h *harness) screen() string {
	lines := strings.Split(ansi.Strip(h.m.View().Content), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// golden compares the screen with testdata/<name>.golden; -update
// rewrites it.
func (h *harness) golden(name string) {
	h.t.Helper()
	got := h.screen()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			h.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatalf("%v (run go test ./internal/tui/app -update)", err)
	}
	if got != string(want) {
		h.t.Errorf("%s differs from %s:\n%s", name, path, got)
	}
}

// The daemon in these tests: two workspaces, Acme with a few
// conversations, Beta empty.

func acmeConversations() []wire.Conversation {
	return []wire.Conversation{
		{ID: "C1", Kind: "channel", Name: "general", Latest: tsC, LastRead: tsB, Unread: true},
		{ID: "C2", Kind: "channel", Name: "random", Latest: tsA, LastRead: tsA},
		{ID: "G1", Kind: "private", Name: "ops", Latest: tsA, LastRead: tsA},
		{ID: "D1", Kind: "im", User: "U1", Latest: tsB, LastRead: tsA, Unread: true, Mentions: 1},
		{ID: "D2", Kind: "im", User: "U2", Latest: tsA, LastRead: tsA},
	}
}

func generalMessages() []wire.Message {
	// conversations.open answers newest first.
	return []wire.Message{
		{TS: tsC, Author: "U2", Text: "see you there"},
		{TS: tsB, Author: "U1", Text: "standup at 10?", Reactions: []wire.Reaction{{Emoji: "+1", Users: []string{"U2"}}}},
		{TS: tsA, Author: "U1", Text: "good morning"},
	}
}

func daemon(handlers map[string]client.Handler) *client.Fake {
	h := map[string]client.Handler{
		"events.subscribe":   client.Result(map[string]any{"topics": subscribedTopics}),
		"conversations.list": client.Result(acmeConversations()),
		"conversations.open": client.Result(openResult{Messages: generalMessages(), Draft: ""}),
		"users.info": client.Result([]wire.User{
			{ID: "U1", Name: "ana", DisplayName: "Ana"},
			{ID: "U2", Name: "bo", RealName: "Bo Li"},
		}),
		"emoji.list":             client.Result(wire.EmojiList{Emoji: map[string]string{}}),
		"conversations.markRead": client.Result(nil),
		"conversations.close":    client.Result(nil),
		"drafts.set":             client.Result(nil),
		"drafts.get":             client.Result(draftResult{}),
		"messages.replies":       client.Result([]wire.Message{}),
		"messages.history":       client.Result([]wire.Message{}),
		"typing.send":            client.Result(nil),
	}
	for k, v := range handlers {
		h[k] = v
	}
	return client.NewFake(hello(acme(), wire.Workspace{Team: "T2", Name: "Beta", State: "connected"}), h)
}

// notFound answers like a daemon without the method.
func notFound(json.RawMessage) (any, error) {
	return nil, &rpc.Error{Code: rpc.CodeMethodNotFound, Message: "Method not found"}
}

func failing(msg string) client.Handler {
	return func(json.RawMessage) (any, error) { return nil, errors.New(msg) }
}
