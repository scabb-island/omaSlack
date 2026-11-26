package events

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func must[T any](t *testing.T, parse func(string) (T, error), s string) T {
	t.Helper()
	v, err := parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func created(t *testing.T, i int) domain.MessageCreated {
	return domain.MessageCreated{
		Team:         must(t, domain.ParseTeamID, "T0001"),
		Conversation: must(t, domain.ParseConversationID, "C0001"),
		Message:      domain.Message{TS: must(t, domain.ParseTS, fmt.Sprintf("1700000000.%06d", i)), Text: fmt.Sprint(i)},
	}
}

func typing(t *testing.T) domain.UserTyping {
	return domain.UserTyping{
		Team:         must(t, domain.ParseTeamID, "T0001"),
		Conversation: must(t, domain.ParseConversationID, "C0001"),
		User:         must(t, domain.ParseUserID, "U0001"),
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// noSummary is a Summary for tests that do not look at it.
func noSummary(context.Context) (wire.Summary, error) { return wire.Summary{}, nil }

// serveHub answers events.subscribe with h on a fresh socket until the
// test ends and returns its path.
func serveHub(t *testing.T, h *Hub) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "omaslack.sock")
	l, err := rpc.Listen(context.Background(), path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := rpc.NewServer(rpc.Router{"events.subscribe": h.Subscribe()}, rpc.Options{})
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	t.Cleanup(func() {
		_ = srv.Close()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	return path
}

// line is anything the server writes: a response or a notification.
type line struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpc.Error      `json:"error"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// client speaks the protocol by hand, the way the QML Service does.
type client struct {
	t  *testing.T
	c  net.Conn
	r  *bufio.Reader
	id int

	pending []line // notifications read while waiting for a response
}

func connect(t *testing.T, path string) *client {
	t.Helper()
	var d net.Dialer
	c, err := d.DialContext(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	cl := &client{t: t, c: c, r: bufio.NewReader(c)}
	if r := cl.call("hello", `{"client":"test","protocol":1}`); r.Error != nil {
		t.Fatalf("hello: %v", r.Error)
	}
	return cl
}

func (c *client) recv() line {
	c.t.Helper()
	b, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("recv: %v", err)
	}
	var l line
	if err := json.Unmarshal(b, &l); err != nil {
		c.t.Fatalf("recv %q: %v", b, err)
	}
	return l
}

// call sends a request and returns its response. Notifications that
// arrive first are kept for next.
func (c *client) call(method, params string) line {
	c.t.Helper()
	c.id++
	if _, err := fmt.Fprintf(c.c, `{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`+"\n", c.id, method, params); err != nil {
		c.t.Fatalf("send: %v", err)
	}
	for {
		l := c.recv()
		if l.Method == "" {
			return l
		}
		c.pending = append(c.pending, l)
	}
}

// next returns the next notification.
func (c *client) next() line {
	c.t.Helper()
	if len(c.pending) > 0 {
		l := c.pending[0]
		c.pending = c.pending[1:]
		return l
	}
	l := c.recv()
	if l.Method == "" {
		c.t.Fatalf("got a response %s, want a notification", l.ID)
	}
	return l
}

func (c *client) subscribe(topics string) line {
	c.t.Helper()
	return c.call("events.subscribe", `{"topics":`+topics+`}`)
}

// fakeClock hands out timers the test fires by hand.
type fakeClock struct{ timers chan chan time.Time }

func newFakeClock() *fakeClock { return &fakeClock{timers: make(chan chan time.Time, 64)} }

func (f *fakeClock) after(time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	f.timers <- c
	return c
}

// fire waits for the next timer the hub starts and fires it.
func (f *fakeClock) fire(t *testing.T) {
	t.Helper()
	select {
	case c := <-f.timers:
		c <- time.Time{}
	case <-time.After(5 * time.Second):
		t.Fatal("the hub started no timer")
	}
}

// fakePeer is a connection whose writes the test controls.
type fakePeer struct {
	mu     sync.Mutex
	got    chan string // methods notified
	block  bool        // Notify waits until the peer is done
	closed bool
	done   chan struct{}
}

func newFakePeer(block bool) *fakePeer {
	return &fakePeer{block: block, got: make(chan string, 64), done: make(chan struct{})}
}

func (p *fakePeer) Notify(method string, _ any) error {
	if p.block {
		<-p.done
		return net.ErrClosed
	}
	p.got <- method
	return nil
}

// next waits for the next notification.
func (p *fakePeer) next(t *testing.T) string {
	t.Helper()
	select {
	case m := <-p.got:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no notification")
		return ""
	}
}

func (p *fakePeer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *fakePeer) Done() <-chan struct{} { return p.done }

func (p *fakePeer) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// end ends the connection, as rpc does once it is closed.
func (p *fakePeer) end() { close(p.done) }

// logBuffer collects log lines from any goroutine.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *logBuffer) logger() *slog.Logger { return slog.New(slog.NewTextHandler(l, nil)) }

// delivered is one notification a gatedPeer wrote.
type delivered struct {
	method string
	params any
}

// gatedPeer is a client that reads nothing until open is called and then
// reads everything at once, like a client that was busy while a burst was
// published. Its first notification is the primer stalled sends to fill
// the hub's in-flight slot; got leaves it out.
type gatedPeer struct {
	mu     sync.Mutex
	gate   chan struct{}
	notes  []delivered
	closed bool
	done   chan struct{}
}

func (p *gatedPeer) Notify(method string, params any) error {
	select {
	case <-p.gate:
	case <-p.done:
		return net.ErrClosed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notes = append(p.notes, delivered{method, params})
	return nil
}

func (p *gatedPeer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *gatedPeer) Done() <-chan struct{} { return p.done }

func (p *gatedPeer) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// open lets the client read.
func (p *gatedPeer) open() { close(p.gate) }

// got waits until want notifications after the primer have arrived,
// checks that no more follow, and returns them.
func (p *gatedPeer) got(t *testing.T, want int) []delivered {
	t.Helper()
	count := func() int {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.notes) - 1
	}
	for deadline := time.Now().Add(5 * time.Second); count() < want && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // anything extra would show up now
	if n := count(); n != want {
		t.Fatalf("got %d notifications after the primer, want %d", n, want)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]delivered(nil), p.notes[1:]...)
}

// stalled subscribes a gatedPeer to topics (plus ui.focus) and sends it a
// primer, which the hub takes from the queue and holds in flight: from
// then on everything published stays queued until open.
func stalled(t *testing.T, h *Hub, topics ...string) *gatedPeer {
	t.Helper()
	p := &gatedPeer{gate: make(chan struct{}), done: make(chan struct{})}
	t.Cleanup(func() { close(p.done) })
	if err := h.subscribe(p, append(topics, TopicFocus)); err != nil {
		t.Fatal(err)
	}
	h.Focus(wire.FocusEvent{})
	return p
}

func team(t *testing.T, s string) domain.TeamID { return must(t, domain.ParseTeamID, s) }
func conv(t *testing.T, s string) domain.ConversationID {
	return must(t, domain.ParseConversationID, s)
}
func user(t *testing.T, s string) domain.UserID { return must(t, domain.ParseUserID, s) }
func ts(t *testing.T, s string) domain.TS       { return must(t, domain.ParseTS, s) }
