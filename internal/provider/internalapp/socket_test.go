package internalapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// socketServer is a fake Socket Mode endpoint: it says hello, sends each
// envelope in order and reports every acknowledgment it reads.
func socketServer(t *testing.T, envelopes []string, acks chan<- string) http.HandlerFunc {
	// slack-go dials with Origin https://api.slack.com.
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		send := func(msg string) bool { return conn.WriteMessage(websocket.TextMessage, []byte(msg)) == nil }
		if !send(`{"type":"hello","num_connections":1,"connection_info":{"app_id":"A0001"}}`) {
			return
		}
		for _, env := range envelopes {
			if !send(env) {
				return
			}
		}
		for {
			var ack struct {
				EnvelopeID string `json:"envelope_id"`
			}
			if err := conn.ReadJSON(&ack); err != nil {
				return
			}
			acks <- ack.EnvelopeID
		}
	}
}

func eventsAPIEnvelope(t *testing.T, id string, payload []byte) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":                     "events_api",
		"envelope_id":              id,
		"payload":                  json.RawMessage(payload),
		"accepts_response_payload": false,
		"retry_attempt":            0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// next reads one event or fails after a timeout.
func next(t *testing.T, events <-chan domain.Event) domain.Event {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("event channel closed early")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5s")
	}
	return nil
}

// rest reads events until the channel closes.
func rest(t *testing.T, events <-chan domain.Event) []domain.Event {
	t.Helper()
	var got []domain.Event
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-time.After(5 * time.Second):
			t.Fatalf("channel still open after 5s; got %v", got)
		}
	}
}

func TestConnectStreamsDomainEventsAndAcknowledgesEveryEnvelope(t *testing.T) {
	acks := make(chan string, 2)
	malformed := `{"type":"event_callback","event":{"type":"message","channel":"C0001","user":"U0002","ts":"later"}}`
	envelopes := []string{
		eventsAPIEnvelope(t, "env-malformed", []byte(malformed)),
		eventsAPIEnvelope(t, "env-message", readFixture(t, "events/message.json")),
	}
	var f *fakeSlack
	f = newFakeSlack(t, map[string]http.HandlerFunc{
		"apps.connections.open": func(w http.ResponseWriter, r *http.Request) {
			wsURL := "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/api/link"
			okJSON([]byte(`{"ok":true,"url":"`+wsURL+`"}`))(w, r)
		},
		"link": socketServer(t, envelopes, acks),
	})
	p, logs := newTestProvider(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := p.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	team := mustTeam(t, "T0001")
	want := []domain.Event{
		domain.ConnectionChanged{Team: team, State: domain.Connecting},
		domain.ConnectionChanged{Team: team, State: domain.Connected},
		domain.MessageCreated{
			Team:         team,
			Conversation: mustConv(t, "C0001"),
			Message:      domain.Message{TS: mustTS(t, "1700000600.000600"), Author: mustUser(t, "U0002"), Text: "hello <@U0001>"},
		},
	}
	for i, w := range want {
		if got := next(t, events); !reflect.DeepEqual(got, w) {
			t.Fatalf("event %d = %#v, want %#v", i, got, w)
		}
	}
	for _, id := range []string{"env-malformed", "env-message"} {
		select {
		case got := <-acks:
			if got != id {
				t.Fatalf("ack = %q, want %q", got, id)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no ack for %s", id)
		}
	}
	if !strings.Contains(logs.String(), "later") {
		t.Errorf("the malformed event is not logged; logs:\n%s", logs)
	}
	if c := f.recorded()[0]; c.Auth != "Bearer "+testAppToken {
		t.Errorf("apps.connections.open Authorization = %q, want the app token", c.Auth)
	}

	// After cancel the channel must close. An event already in flight (a
	// reconnect's "connecting", say) may still arrive first; emit's own
	// test pins that nothing new is produced once ctx has ended.
	cancel()
	for _, ev := range rest(t, events) {
		if _, ok := ev.(domain.ConnectionChanged); !ok {
			t.Errorf("a %T arrived after cancel; only an in-flight connection change may", ev)
		}
	}
}

func TestConnectEndsInAuthExpiredWhenSlackRejectsTheAppToken(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"apps.connections.open": okJSON([]byte(`{"ok":false,"error":"invalid_auth"}`)),
	})
	p, _ := newTestProvider(t, f)

	events, err := p.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	got := rest(t, events)
	want := domain.ConnectionChanged{Team: mustTeam(t, "T0001"), State: domain.AuthExpired}
	if len(got) == 0 || got[len(got)-1] != want {
		t.Fatalf("events = %v, want them to end in %v", got, want)
	}
}

func TestConnectEndsDisconnectedOnOtherFatalErrors(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"apps.connections.open": respond(http.StatusNotFound, nil, nil),
	})
	p, _ := newTestProvider(t, f)

	events, err := p.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	got := rest(t, events)
	want := domain.ConnectionChanged{Team: mustTeam(t, "T0001"), State: domain.Disconnected}
	if len(got) == 0 || got[len(got)-1] != want {
		t.Fatalf("events = %v, want them to end in %v", got, want)
	}
}

func TestSocketModeEventsMapToConnectionStates(t *testing.T) {
	p := New(mustTeam(t, "T0001"), testCreds())
	cases := map[socketmode.EventType]domain.Event{
		socketmode.EventTypeConnecting:      domain.ConnectionChanged{Team: p.Team(), State: domain.Connecting},
		socketmode.EventTypeConnected:       domain.ConnectionChanged{Team: p.Team(), State: domain.Connected},
		socketmode.EventTypeConnectionError: domain.ConnectionChanged{Team: p.Team(), State: domain.Disconnected},
		socketmode.EventTypeHello:           nil,
		socketmode.EventTypeDisconnect:      nil,
		socketmode.EventTypeInteractive:     nil,
	}
	for typ, want := range cases {
		if got := p.domainEvent(socketmode.Event{Type: typ, Request: &socketmode.Request{}}); got != want {
			t.Errorf("%s: domainEvent() = %v, want %v", typ, got, want)
		}
	}
}

func TestOnlyEnvelopesAreAcknowledged(t *testing.T) {
	cases := []struct {
		evt  socketmode.Event
		want bool
	}{
		{socketmode.Event{Type: socketmode.EventTypeConnected}, false},
		{socketmode.Event{Type: socketmode.EventTypeHello, Request: &socketmode.Request{Type: "hello"}}, false},
		{socketmode.Event{Type: socketmode.EventTypeEventsAPI, Request: &socketmode.Request{EnvelopeID: "e1"}}, true},
		{socketmode.Event{Type: socketmode.EventTypeInteractive, Request: &socketmode.Request{EnvelopeID: "e2"}}, true},
	}
	for _, tc := range cases {
		if got := needsAck(tc.evt); got != tc.want {
			t.Errorf("needsAck(%s) = %v, want %v", tc.evt.Type, got, tc.want)
		}
	}
}

func TestOnlyRejectedCredentialsEndInAuthExpired(t *testing.T) {
	cases := map[string]struct {
		err  error
		want domain.ConnectionState
	}{
		"token_revoked":      {slack.SlackErrorResponse{Err: "token_revoked"}, domain.AuthExpired},
		"other slack error":  {slack.SlackErrorResponse{Err: "internal_error"}, domain.Disconnected},
		"wrapped auth error": {fmt.Errorf("open: %w", slack.SlackErrorResponse{Err: "not_authed"}), domain.AuthExpired},
		"HTTP status":        {slack.StatusCodeError{Code: http.StatusNotFound}, domain.Disconnected},
	}
	for name, tc := range cases {
		if got := finalState(tc.err); got != tc.want {
			t.Errorf("%s: finalState() = %v, want %v", name, got, tc.want)
		}
	}
}

// Once ctx has ended, emit must not hand over another event, even when a
// reader is ready: a plain select picks at random between the two.
func TestEmitDropsEventsOnceCtxHasEnded(t *testing.T) {
	p := &Provider{team: mustTeam(t, "T0001")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 200 {
		out := make(chan domain.Event, 1) // a ready reader
		p.emit(ctx, out, domain.ConnectionChanged{Team: p.team, State: domain.Connecting})
		if len(out) != 0 {
			t.Fatal("emit delivered an event after ctx ended")
		}
	}
}
