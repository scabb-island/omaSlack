package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// ── events.subscribe over a real socket ──

func TestSubscribersGetTheirTopicsAsNotifications(t *testing.T) {
	h := New(Options{Summary: noSummary})
	c := connect(t, serveHub(t, h))
	r := c.subscribe(`["message.created"]`)
	if r.Error != nil || string(r.Result) != `{"topics":["message.created"]}` {
		t.Fatalf("subscribe = %s (error %v)", r.Result, r.Error)
	}

	ev := created(t, 1)
	h.Publish(typing(t)) // not subscribed
	h.Publish(ev)

	n := c.next()
	if n.Method != "message.created" || string(n.Params) != jsonOf(t, wire.FromEvent(ev)) {
		t.Fatalf("notification = %s %s", n.Method, n.Params)
	}
	if n.ID != nil {
		t.Errorf("a notification has id %s", n.ID)
	}
}

func TestSubscribingAgainReplacesTheTopics(t *testing.T) {
	h := New(Options{Summary: noSummary})
	c := connect(t, serveHub(t, h))
	c.subscribe(`["message.created"]`)
	c.subscribe(`["typing","typing"]`)

	h.Publish(created(t, 1))
	h.Publish(typing(t))
	if n := c.next(); n.Method != "typing" {
		t.Fatalf("got %s, want only typing", n.Method)
	}
}

func TestUnknownTopicsAreInvalidParams(t *testing.T) {
	h := New(Options{Summary: noSummary})
	c := connect(t, serveHub(t, h))
	for _, params := range []string{`{"topics":["message.created","nope"]}`, `{"topics":"typing"}`, `[1]`} {
		r := c.call("events.subscribe", params)
		if r.Error == nil || r.Error.Code != rpc.CodeInvalidParams {
			t.Errorf("%s: response %s (error %v), want %d", params, r.Result, r.Error, rpc.CodeInvalidParams)
		}
	}
	// A failed call leaves the old topics alone.
	c.subscribe(`["typing"]`)
	c.call("events.subscribe", `{"topics":["nope"]}`)
	h.Publish(typing(t))
	if n := c.next(); n.Method != "typing" {
		t.Fatalf("got %s, want typing", n.Method)
	}
}

func TestSubscribeNeedsAConnection(t *testing.T) {
	h := New(Options{Summary: noSummary})
	_, err := h.Subscribe().Handle(context.Background(), "events.subscribe", json.RawMessage(`{"topics":[]}`))
	if err == nil {
		t.Fatal("subscribe without a connection succeeded")
	}
}

func TestNewSummarySubscribersGetTheCurrentSummary(t *testing.T) {
	sum := wire.Summary{Workspaces: map[string]wire.Counts{"T0001": {DMs: 2}}, Total: wire.Counts{DMs: 2}}
	h := New(Options{Summary: func(context.Context) (wire.Summary, error) { return sum, nil }})
	c := connect(t, serveHub(t, h))
	c.subscribe(`["unread.summary"]`)

	n := c.next()
	if n.Method != TopicUnreadSummary || string(n.Params) != jsonOf(t, sum) {
		t.Fatalf("notification = %s %s", n.Method, n.Params)
	}
}

// Spec daemon-api, "Message burst".
func TestABurstReachesEveryMessageSubscriberAndIsCoalescedForTheSummary(t *testing.T) {
	var unread atomic.Int64
	summary := func(context.Context) (wire.Summary, error) {
		return wire.Summary{Total: wire.Counts{DMs: int(unread.Load())}}, nil
	}
	clock := newFakeClock()
	h := New(Options{Summary: summary, After: clock.after})
	path := serveHub(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Run(ctx) }()

	msgs, bar := connect(t, path), connect(t, path)
	msgs.subscribe(`["message.created"]`)
	bar.subscribe(`["unread.summary"]`)
	bar.next() // the current summary

	for i := range 40 {
		unread.Add(1)
		h.Publish(created(t, i))
	}
	for i := range 40 {
		n := msgs.next()
		if want := jsonOf(t, wire.FromEvent(created(t, i))); n.Method != "message.created" || string(n.Params) != want {
			t.Fatalf("event %d = %s %s, want %s", i, n.Method, n.Params, want)
		}
	}

	summaries, ticks := 0, 0
	for {
		n := bar.next()
		summaries++
		if !strings.Contains(string(n.Params), `"dms":40`) {
			clock.fire(t)
			ticks++
			continue
		}
		break
	}
	if summaries > ticks+1 {
		t.Errorf("%d summaries after %d intervals, want at most one leading plus one per interval", summaries, ticks)
	}
}

func TestSummaryIsNotSentForEventsThatCannotChangeIt(t *testing.T) {
	var calls atomic.Int64
	summary := func(context.Context) (wire.Summary, error) { calls.Add(1); return wire.Summary{}, nil }
	clock := newFakeClock()
	h := New(Options{Summary: summary, After: clock.after})
	c := connect(t, serveHub(t, h))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Run(ctx) }()
	c.subscribe(`["unread.summary","typing"]`)
	c.next() // the current summary

	h.Publish(typing(t))
	h.Publish(created(t, 1))
	if n := c.next(); n.Method != "typing" {
		t.Fatalf("got %s, want typing", n.Method)
	}
	if n := c.next(); n.Method != TopicUnreadSummary {
		t.Fatalf("got %s, want unread.summary", n.Method)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("summary computed %d times, want 2 (on subscribe and after message.created)", got)
	}
}

func TestTheSummaryIsOnlyComputedForSomeone(t *testing.T) {
	var calls atomic.Int64
	summary := func(context.Context) (wire.Summary, error) { calls.Add(1); return wire.Summary{}, nil }
	clock := newFakeClock()
	h := New(Options{Summary: summary, After: clock.after})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Run(ctx) }()

	h.Publish(created(t, 1))
	clock.fire(t) // the window opened: the change was handled
	if got := calls.Load(); got != 0 {
		t.Errorf("summary computed %d times with no unread.summary subscriber", got)
	}
}

func TestASummaryThatFailsIsLoggedAndNotSent(t *testing.T) {
	var logs logBuffer
	var fail atomic.Bool
	fail.Store(true)
	summary := func(context.Context) (wire.Summary, error) {
		if fail.Load() {
			return wire.Summary{}, errors.New("store is gone")
		}
		return wire.Summary{Total: wire.Counts{Mentions: 1}}, nil
	}
	clock := newFakeClock()
	h := New(Options{Summary: summary, After: clock.after, Log: logs.logger()})
	c := connect(t, serveHub(t, h))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Run(ctx) }()
	c.subscribe(`["unread.summary","typing"]`) // no summary: it failed

	h.Publish(created(t, 1))
	clock.fire(t) // the failed leading emission still opened a window
	fail.Store(false)
	h.Publish(typing(t))
	if n := c.next(); n.Method != "typing" {
		t.Fatalf("got %s %s, want typing first", n.Method, n.Params)
	}
	if !strings.Contains(logs.String(), "store is gone") {
		t.Errorf("log = %q, want the failure", logs.String())
	}
}

func TestSubscribingAnEndedConnectionFails(t *testing.T) {
	h := New(Options{Summary: noSummary})
	p := newFakePeer(false)
	p.end()
	if err := h.subscribe(p, []string{TopicFocus}); err == nil {
		t.Fatal("subscribed an ended connection")
	}
}

// ── Slow subscribers (spec daemon-api, "Slow subscribers are disconnected") ──

func TestASubscriberThatOverflowsIsClosedAndLogged(t *testing.T) {
	var logs logBuffer
	h := New(Options{Summary: noSummary, QueueSize: 2, Log: logs.logger()})
	slow, fast := newFakePeer(true), newFakePeer(false)
	defer slow.end()
	if err := h.subscribe(slow, []string{"message.created"}); err != nil {
		t.Fatal(err)
	}
	if err := h.subscribe(fast, []string{"message.created", TopicFocus}); err != nil {
		t.Fatal(err)
	}

	for i := range 10 {
		h.Publish(created(t, i)) // never blocks, or the test hangs
		if m := fast.next(t); m != "message.created" {
			t.Fatalf("fast subscriber got %s", m)
		}
	}
	if !slow.isClosed() {
		t.Error("the slow subscriber is still connected")
	}
	if !strings.Contains(logs.String(), "slow subscriber") {
		t.Errorf("log = %q, want the disconnection", logs.String())
	}
	if n := h.Focus(wire.FocusEvent{Team: "T0001", Conv: "C0001"}); n != 1 {
		t.Errorf("Focus reached %d, want only the fast subscriber", n)
	}
	fast.end()
	if n := h.Focus(wire.FocusEvent{}); n != 0 {
		t.Errorf("Focus reached %d after the connection ended, want 0", n)
	}
}

func TestFocusReachesOnlyFocusSubscribers(t *testing.T) {
	h := New(Options{Summary: noSummary})
	c := connect(t, serveHub(t, h))
	other := connect(t, serveHub(t, h))
	other.subscribe(`["typing"]`)
	if n := h.Focus(wire.FocusEvent{Team: "T0001", Conv: "C0001"}); n != 0 {
		t.Fatalf("Focus reached %d before anyone subscribed", n)
	}
	c.subscribe(`["ui.focus"]`)

	f := wire.FocusEvent{Team: "T0001", Conv: "C0001", TS: "1700000000.000001"}
	if n := h.Focus(f); n != 1 {
		t.Fatalf("Focus reached %d, want 1", n)
	}
	if n := c.next(); n.Method != TopicFocus || string(n.Params) != jsonOf(t, f) {
		t.Fatalf("notification = %s %s", n.Method, n.Params)
	}
}

func TestAClosedConnectionIsDropped(t *testing.T) {
	h := New(Options{Summary: noSummary})
	c := connect(t, serveHub(t, h))
	c.subscribe(`["ui.focus"]`)
	p := newFakePeer(false)
	if err := h.subscribe(p, []string{TopicFocus}); err != nil {
		t.Fatal(err)
	}
	p.end()
	if n := h.Focus(wire.FocusEvent{}); n != 1 {
		t.Fatalf("Focus reached %d, want only the live connection", n)
	}
}

// Spec daemon-api, "A client stops reading", on a real socket.
func TestAClientThatStopsReadingIsDisconnectedAndOthersKeepGettingEvents(t *testing.T) {
	const events = 2000 // 2 MB: far more than the socket buffers
	var logs logBuffer
	h := New(Options{Summary: noSummary, QueueSize: 16, Log: logs.logger()})
	path := serveHub(t, h)
	stuck, reader := connect(t, path), connect(t, path)
	stuck.subscribe(`["message.created"]`)
	reader.subscribe(`["message.created"]`)

	big := strings.Repeat("x", 1000)
	for i := range events {
		ev := created(t, i)
		ev.Message.Text = big
		h.Publish(ev)
		if _, err := reader.r.ReadBytes('\n'); err != nil {
			t.Fatalf("the reading client lost event %d: %v", i, err)
		}
	}

	got := 0
	for {
		if _, err := stuck.r.ReadBytes('\n'); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
				t.Fatalf("read: %v, want the connection closed", err)
			}
			break
		}
		got++
	}
	if got >= events {
		t.Errorf("the stuck client got all %d events and was never disconnected", got)
	}
	if !strings.Contains(logs.String(), "slow subscriber") {
		t.Errorf("log = %q, want the disconnection", logs.String())
	}
}
