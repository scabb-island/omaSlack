package desktopsession

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// recordFrames keeps the socket open and sends every frame the client
// writes to got, until the client closes it.
func recordFrames(got chan<- string) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			got <- string(data)
		}
	}
}

// liveProvider connects a provider to a fake socket that records what it
// is sent, and returns once Slack's hello made the socket live.
func liveProvider(t *testing.T, got chan<- string) (*Provider, context.CancelFunc, <-chan domain.Event) {
	t.Helper()
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, []string{string(fixture(t, "frames/hello.json"))}, recordFrames(got)),
	})
	p, _ := f.provider(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := connect(t, p, ctx)
	next(t, events) // connecting
	next(t, events) // connected
	return p, cancel, events
}

// frameJSON decodes a frame the client wrote, failing after a timeout.
func frameJSON(t *testing.T, got <-chan string) map[string]any {
	t.Helper()
	select {
	case s := <-got:
		var v map[string]any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Fatalf("frame %q: %v", s, err)
		}
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5s")
	}
	return nil
}

func TestSendTypingWritesATypingFrame(t *testing.T) {
	got := make(chan string, 1)
	p, _, _ := liveProvider(t, got)

	if err := p.SendTyping(context.Background(), parse(t, domain.ParseConversationID, "C0001")); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{"type": "typing", "channel": "C0001"}
	if frame := frameJSON(t, got); !reflect.DeepEqual(frame, want) {
		t.Errorf("frame = %v, want %v", frame, want)
	}
}

func TestSubscribePresenceSendsTheWholeSubscription(t *testing.T) {
	got := make(chan string, 2)
	p, _, _ := liveProvider(t, got)
	users := []domain.UserID{parse(t, domain.ParseUserID, "U0002"), parse(t, domain.ParseUserID, "W0003")}

	if err := p.SubscribePresence(context.Background(), users); err != nil {
		t.Fatal(err)
	}
	// Each call replaces the subscription, so an empty one clears it:
	// the frame must say [] and not null.
	if err := p.SubscribePresence(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	for _, want := range []map[string]any{
		{"type": "presence_sub", "ids": []any{"U0002", "W0003"}},
		{"type": "presence_sub", "ids": []any{}},
	} {
		if frame := frameJSON(t, got); !reflect.DeepEqual(frame, want) {
			t.Errorf("frame = %v, want %v", frame, want)
		}
	}
}

func TestTypingWithoutALiveSocketFailsWithErrNotConnected(t *testing.T) {
	conv := parse(t, domain.ParseConversationID, "C0001")
	writes := func(p *Provider) map[string]error {
		return map[string]error{
			"typing": p.SendTyping(context.Background(), conv),
		}
	}

	never, _ := newFakeSlack(t, nil).provider(t)
	for name, err := range writes(never) {
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("before Connect, %s err = %v, want ErrNotConnected", name, err)
		}
	}

	got := make(chan string, 1)
	closed, cancel, events := liveProvider(t, got)
	cancel()
	rest(t, events)
	for name, err := range writes(closed) {
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("after the stream closed, %s err = %v, want ErrNotConnected", name, err)
		}
	}
}

// A stream that ends after a newer one opened must not take the newer
// socket away from the writers.
func TestAnOlderStreamEndingKeepsTheNewerSocket(t *testing.T) {
	got := make(chan string, 1)
	p, cancelOld, oldEvents := liveProvider(t, got)
	newer := connect(t, p, t.Context())
	next(t, newer) // connecting
	next(t, newer) // connected

	cancelOld()
	rest(t, oldEvents)

	if err := p.SendTyping(context.Background(), parse(t, domain.ParseConversationID, "C0001")); err != nil {
		t.Fatalf("err = %v, want the newer socket to take the frame", err)
	}
	if frame := frameJSON(t, got); frame["type"] != "typing" {
		t.Errorf("frame = %v", frame)
	}
}

func TestWritesHonorTheirContext(t *testing.T) {
	got := make(chan string, 1)
	p, _, _ := liveProvider(t, got)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := p.SendTyping(ctx, parse(t, domain.ParseConversationID, "C0001")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	select {
	case s := <-got:
		t.Errorf("a cancelled write still sent %q", s)
	case <-time.After(50 * time.Millisecond):
	}
}

// Concurrent writers alongside the read loop: run with -race. gorilla
// allows one writer at a time, so a missing lock is a race or a garbled
// frame.
func TestConcurrentWritesAreSafe(t *testing.T) {
	const writers = 20
	got := make(chan string, writers)
	p, _, _ := liveProvider(t, got)
	conv := parse(t, domain.ParseConversationID, "C0001")

	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			if err := p.SendTyping(context.Background(), conv); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	for range writers {
		if frame := frameJSON(t, got); frame["type"] != "typing" {
			t.Errorf("frame = %v", frame)
		}
	}
}

// Slack forgets the presence subscription whenever the socket reconnects,
// so the provider remembers it: a subscription made while no socket is
// open is sent when one opens, and the last one is sent again on every
// reconnect.
func TestPresenceSubscriptionIsSentWhenTheSocketOpens(t *testing.T) {
	got := make(chan string, 1)
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, []string{string(fixture(t, "frames/hello.json"))}, recordFrames(got)),
	})
	p, _ := f.provider(t)
	users := []domain.UserID{parse(t, domain.ParseUserID, "U0001")}
	if err := p.SubscribePresence(context.Background(), users); err != nil {
		t.Fatalf("SubscribePresence before Connect = %v, want it remembered", err)
	}

	events := connect(t, p, t.Context())
	next(t, events) // connecting
	next(t, events) // connected

	want := map[string]any{"type": "presence_sub", "ids": []any{"U0001"}}
	if frame := frameJSON(t, got); !reflect.DeepEqual(frame, want) {
		t.Errorf("frame = %v, want %v", frame, want)
	}
}

func TestPresenceSubscriptionIsSentAgainAfterAReconnect(t *testing.T) {
	got := make(chan string, 2)
	p, cancelOld, oldEvents := liveProvider(t, got)
	users := []domain.UserID{parse(t, domain.ParseUserID, "U0002")}
	if err := p.SubscribePresence(context.Background(), users); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"type": "presence_sub", "ids": []any{"U0002"}}
	if frame := frameJSON(t, got); !reflect.DeepEqual(frame, want) {
		t.Fatalf("first frame = %v, want %v", frame, want)
	}

	cancelOld()
	rest(t, oldEvents)
	newer := connect(t, p, t.Context())
	next(t, newer) // connecting
	next(t, newer) // connected

	if frame := frameJSON(t, got); !reflect.DeepEqual(frame, want) {
		t.Errorf("after the reconnect frame = %v, want %v", frame, want)
	}
}

func TestNoPresenceFrameWithoutASubscription(t *testing.T) {
	got := make(chan string, 1)
	liveProvider(t, got)
	select {
	case s := <-got:
		t.Errorf("a socket with no subscription was sent %q", s)
	case <-time.After(50 * time.Millisecond):
	}
}
