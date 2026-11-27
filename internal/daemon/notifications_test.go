package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/notify"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// desktop is a fake notification sender: it numbers what it shows from
// 1 and can click it, as the session bus would report.
type desktop struct {
	mu     sync.Mutex
	shown  chan notify.Notification
	events notify.Events
	last   uint32
}

func (d *desktop) Notify(_ context.Context, n notify.Notification) (uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.last++
	d.shown <- n
	return d.last, nil
}

// clickOn clicks notification id until c receives ui.focus: the notifier
// learns the ID only once Notify returned, a moment after it was shown.
func (d *desktop) clickOn(t *testing.T, id uint32, c *rpcclient.Client) wire.FocusEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d.events.Clicked(id)
		select {
		case note := <-c.Notifications():
			var f wire.FocusEvent
			if note.Method != "ui.focus" || json.Unmarshal(note.Params, &f) != nil {
				t.Fatalf("got %s %s, want ui.focus", note.Method, note.Params)
			}
			return f
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("no ui.focus")
	return wire.FocusEvent{}
}

// clickUntilLaunched clicks notification id until the TUI is launched.
func (n *notifying) clickUntilLaunched(id uint32) {
	n.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n.desk.events.Clicked(id)
		select {
		case <-n.launched:
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	n.t.Fatal("the TUI was not started")
}

// notifying is a harness with notifications on, one workspace streaming
// live events and a fake TUI launcher.
type notifying struct {
	*harness
	desk     *desktop
	live     chan domain.Event
	launched chan struct{}
	config   string
}

func newNotifying(t *testing.T) *notifying {
	t.Helper()
	n := &notifying{
		harness: newHarness(t), desk: &desktop{shown: make(chan notify.Notification, 16)},
		live: make(chan domain.Event), launched: make(chan struct{}, 4),
	}
	n.config = filepath.Join(t.TempDir(), "config.toml")
	n.cfg.Paths.Config = n.config
	n.cfg.Notifications = func(ev notify.Events) notify.Sender { n.desk.events = ev; return n.desk }
	n.cfg.LaunchTUI = func() error { n.launched <- struct{}{}; return nil }
	team := n.addTeam("T0ACME")
	f := n.provs.fake(team)
	f.SetConversations(
		domain.Conversation{ID: mustConv(t, "C0GENERAL"), Kind: domain.Channel, Name: "general"},
		domain.Conversation{ID: mustConv(t, "D0BOB"), Kind: domain.DirectMessage},
	)
	f.Script(providertest.Session{
		Events: []domain.Event{domain.ConnectionChanged{Team: team, State: domain.Connected}},
		Live:   n.live,
	})
	return n
}

// send streams a new message from U0BOB in conv, sent now.
func (n *notifying) send(conv, text string) {
	n.t.Helper()
	n.stream(conv, domain.Message{Author: domain.UserAuthor(mustUser(n.t, "U0BOB")), Text: text})
}

// stream streams m in conv, stamped now unless it has a TS.
func (n *notifying) stream(conv string, m domain.Message) {
	n.t.Helper()
	if m.TS.IsZero() {
		m.TS = domain.TSAt(time.Now())
	}
	select {
	case n.live <- domain.MessageCreated{Team: mustTeam(n.t, "T0ACME"), Conversation: mustConv(n.t, conv), Message: m}:
	case <-time.After(5 * time.Second):
		n.t.Fatal("the engine did not take the message")
	}
}

func (n *notifying) expect(body string) {
	n.t.Helper()
	select {
	case got := <-n.desk.shown:
		if got.Body != body {
			n.t.Errorf("notification %+v, want body %q", got, body)
		}
	case <-time.After(5 * time.Second):
		n.t.Fatalf("no notification %q", body)
	}
}

// settle sends a DM and waits for its notification, so every message
// before it has been decided.
func (n *notifying) settle() {
	n.t.Helper()
	n.send("D0BOB", "settle")
	n.expect("settle")
	select {
	case got := <-n.desk.shown:
		n.t.Errorf("unexpected notification %+v", got)
	default:
	}
}

func (n *notifying) dialAs(client string) *rpcclient.Client {
	n.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := rpcclient.Dial(ctx, rpcclient.Options{
		Client: client, Path: n.cfg.Paths.Socket,
		Run: func(context.Context, string, ...string) error { return nil },
	})
	if err != nil {
		n.t.Fatal(err)
	}
	n.t.Cleanup(func() { _ = c.Close() })
	return c
}

// ready waits until the daemon serves the workspace and has its
// conversation list, so messages are weighed with their conversation.
func (n *notifying) ready() {
	n.t.Helper()
	c := n.dial()
	eventually(n.t, "the conversation list", func() bool {
		l := call[[]wire.Conversation](n.t, c, "conversations.list", map[string]string{"team": "T0ACME"})
		return len(l) == 2
	})
}

func focusOf(t *testing.T, c *rpcclient.Client) wire.FocusEvent {
	t.Helper()
	select {
	case note := <-c.Notifications():
		var f wire.FocusEvent
		if note.Method != "ui.focus" || json.Unmarshal(note.Params, &f) != nil {
			t.Fatalf("got %s %s, want ui.focus", note.Method, note.Params)
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no ui.focus")
		return wire.FocusEvent{}
	}
}

// One notification per message, however many clients are connected; a
// channel message that matches no rule notifies nobody.
func TestNotifiesOncePerMessage(t *testing.T) {
	n := newNotifying(t)
	n.start()
	n.ready()
	n.dialAs(pluginClient)
	n.dialAs(tuiClient)
	n.send("D0BOB", "hi there")
	n.expect("hi there")
	n.send("C0GENERAL", "chatter")
	n.send("C0GENERAL", "hey <@U0ME>")
	n.expect("hey @U0ME")
	bot, _ := domain.ParseBotID("B0CI")
	n.stream("D0BOB", domain.Message{Author: domain.BotAuthor(bot), AuthorName: "CI", Text: "build green"})
	if got := <-n.desk.shown; got.Summary != "CI" {
		t.Errorf("a bot's notification is %+v, want it named CI", got)
	}
	n.settle()
}

// A reply in a thread the user started notifies; one in someone else's
// thread does not.
func TestRepliesInFollowedThreads(t *testing.T) {
	n := newNotifying(t)
	n.start()
	n.ready()
	mine := domain.TSAt(time.Now())
	n.stream("C0GENERAL", domain.Message{TS: mine, Author: domain.UserAuthor(mustUser(t, "U0ME")), Text: "my question"})
	theirs := mine.Next()
	n.stream("C0GENERAL", domain.Message{TS: theirs, Author: domain.UserAuthor(mustUser(t, "U0BOB")), Text: "bob's question"})
	n.stream("C0GENERAL", domain.Message{ThreadTS: theirs, Author: domain.UserAuthor(mustUser(t, "U0BOB")), Text: "in bob's thread"})
	n.stream("C0GENERAL", domain.Message{ThreadTS: mine, Author: domain.UserAuthor(mustUser(t, "U0BOB")), Text: "an answer"})
	n.expect("an answer")
	n.settle()
}

func TestViewingSuppresses(t *testing.T) {
	n := newNotifying(t)
	n.start()
	n.ready()
	tui := n.dialAs(tuiClient)
	call[json.RawMessage](t, tui, "ui.viewing", map[string]any{"team": "T0ACME", "conv": "C0GENERAL", "focused": true})
	n.send("C0GENERAL", "<@U0ME> seen")
	n.settle() // decided while the view stands

	call[json.RawMessage](t, tui, "ui.viewing", map[string]any{})
	n.send("C0GENERAL", "<@U0ME> cleared")
	n.expect("@U0ME cleared")

	call[json.RawMessage](t, tui, "ui.viewing", map[string]any{"team": "T0ACME", "conv": "D0BOB", "focused": true})
	_ = tui.Close() // its view goes with it, once the daemon sees it close
	deadline := time.Now().Add(5 * time.Second)
	for {
		n.send("D0BOB", "after the TUI left")
		select {
		case got := <-n.desk.shown:
			if got.Body != "after the TUI left" {
				t.Errorf("notification %+v", got)
			}
			return
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the closed connection's view still suppresses")
		}
	}
}

func TestViewingRejectsBadParams(t *testing.T) {
	n := newNotifying(t)
	n.start()
	n.ready()
	c := n.dial()
	for _, params := range []any{
		map[string]any{"team": "bad", "conv": "D0BOB"},
		map[string]any{"team": "T0ACME"},
		map[string]any{"team": "T0ACME", "conv": "D0BOB", "threadTS": "nope"},
		[]int{1},
	} {
		_, err := c.Call(context.Background(), "ui.viewing", params)
		var rerr *rpc.Error
		if !errors.As(err, &rerr) || rerr.Code != rpc.CodeInvalidParams {
			t.Errorf("%v: err %v, want invalid params", params, err)
		}
	}
	if _, err := c.Call(context.Background(), "ui.viewing", nil); err != nil {
		t.Errorf("no params clears: %v", err)
	}
}

// A click opens the plugin's window when the plugin is connected.
func TestClickFocusesThePlugin(t *testing.T) {
	n := newNotifying(t)
	n.start()
	n.ready()
	plugin := n.dialAs(pluginClient)
	n.send("D0BOB", "open me")
	n.expect("open me")
	if f := n.desk.clickOn(t, 1, plugin); f.Team != "T0ACME" || f.Conv != "D0BOB" || f.TS == "" {
		t.Errorf("focus %+v", f)
	}
	select {
	case <-n.launched:
		t.Error("the TUI was started although the plugin is preferred and connected")
	default:
	}
}

// Without the plugin, a click starts the TUI, and the TUI receives the
// focus once it subscribes.
func TestClickStartsTheTUI(t *testing.T) {
	n := newNotifying(t)
	n.start()
	n.ready()
	n.send("D0BOB", "open me")
	n.expect("open me")
	n.clickUntilLaunched(1)
	tui := n.dialAs(tuiClient)
	call[json.RawMessage](t, tui, "events.subscribe", map[string]any{"topics": []string{"ui.focus"}})
	if f := focusOf(t, tui); f.Conv != "D0BOB" {
		t.Errorf("focus %+v", f)
	}
}

// open = "tui" prefers the TUI even with the plugin connected; the
// config is read again on reload, and a bad file keeps the previous one.
func TestConfigReloads(t *testing.T) {
	n := newNotifying(t)
	writeConfig(t, n.config, "[notifications]\nopen = \"tui\"\nmuted = [\"#general\"]\n")
	n.start()
	n.ready()
	plugin := n.dialAs(pluginClient)
	tui := n.dialAs(tuiClient)
	n.send("C0GENERAL", "<@U0ME> muted")
	n.send("D0BOB", "to the TUI")
	n.expect("to the TUI")
	if f := n.desk.clickOn(t, 1, tui); f.Conv != "D0BOB" {
		t.Errorf("focus %+v", f)
	}
	select {
	case note := <-plugin.Notifications():
		t.Errorf("the plugin got %s", note.Method)
	default:
	}

	<-n.launched

	writeConfig(t, n.config, "[notifications]\nkeywords = [\"deploy\"]\n")
	n.reloadNow()
	n.reloadNow() // taken once the previous reload is done
	n.send("C0GENERAL", "deploy done")
	n.expect("deploy done")
	writeConfig(t, n.config, "[notifications\n")
	n.reloadNow()
	n.reloadNow() // taken once the previous reload is done
	n.send("C0GENERAL", "deploy again")
	n.expect("deploy again")
}

func writeConfig(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustConv(t *testing.T, s string) domain.ConversationID {
	t.Helper()
	id, err := domain.ParseConversationID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLaunch(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "args")
	script := filepath.Join(dir, "launcher")
	writeConfig(t, script, "#!/bin/sh\necho \"$@\" > "+out+"\n")
	if err := os.Chmod(script, 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}
	if err := launch(script, func() (string, error) { return "", errors.New("unknown") }); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the launcher to run", func() bool {
		b, _ := os.ReadFile(out) //nolint:gosec // the test's own file
		return string(b) == "omaslack tui\n"
	})
	if err := launch(filepath.Join(dir, "missing"), os.Executable); err == nil {
		t.Error("a missing launcher is not an error")
	}
}
