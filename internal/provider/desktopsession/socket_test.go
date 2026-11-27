package desktopsession

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

var _ provider.Provider = (*Provider)(nil)

const authOK = `{"ok":true,"url":"https://test.slack.com/","team":"Test","user":"me","team_id":"T0001","user_id":"U0001"}`

// socketServer is a fake web client socket: it sends frames in order and
// then hands the connection to then.
func socketServer(t *testing.T, frames []string, then func(*websocket.Conn)) http.HandlerFunc {
	// The provider dials with Origin https://app.slack.com, like a browser.
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for _, f := range frames {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(f)); err != nil {
				return
			}
		}
		then(conn)
	}
}

// hangUp closes the socket at once.
func hangUp(*websocket.Conn) {}

// waitForClose keeps the socket open until the client closes it.
func waitForClose(conn *websocket.Conn) {
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
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

func state(t *testing.T, s domain.ConnectionState) domain.Event {
	return domain.ConnectionChanged{Team: parse(t, domain.ParseTeamID, "T0001"), State: s}
}

func connect(t *testing.T, p *Provider, ctx context.Context) <-chan domain.Event {
	t.Helper()
	events, err := p.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestConnectStreamsFramesUntilTheSocketCloses(t *testing.T) {
	frames := []string{
		string(fixture(t, "frames/hello.json")),
		`{"type":"message","channel":"C0001","user":"U0002","ts":"later"}`,
		string(fixture(t, "frames/message.json")),
	}
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, frames, hangUp),
	})
	p, logs := f.provider(t)

	got := rest(t, connect(t, p, context.Background()))

	team := parse(t, domain.ParseTeamID, "T0001")
	want := []domain.Event{
		state(t, domain.Connecting),
		state(t, domain.Connected),
		domain.MessageCreated{Team: team, Conversation: parse(t, domain.ParseConversationID, "C0001"), Message: domain.Message{
			TS: parse(t, domain.ParseTS, "1700000600.000600"), Author: parse(t, domain.ParseAuthor, "U0002"), Text: "hello <@U0001>",
		}},
		state(t, domain.Disconnected),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events =\n%#v\nwant\n%#v", got, want)
	}
	if !strings.Contains(logs.String(), "later") {
		t.Errorf("the malformed frame is not logged; logs:\n%s", logs)
	}
	reqs := f.requests()
	if len(reqs) != 2 || reqs[0].Path != "/api/auth.test" || reqs[0].Form.Get("token") != testToken {
		t.Fatalf("requests = %+v, want auth.test with the token, then the socket", reqs)
	}
	assertSocketRequest(t, reqs[1])
}

// assertSocketRequest checks the upgrade request the web client sends.
func assertSocketRequest(t *testing.T, req request) {
	t.Helper()
	q := req.Form
	if q.Get("token") != testToken || q.Get("gateway_server") != "T0001-1" || q.Get("slack_client") != "desktop" {
		t.Errorf("socket query = %v, want the token, the team's gateway and slack_client=desktop", q)
	}
	if args := q.Get("start_args"); !strings.Contains(args, "agent=client") || !strings.Contains(args, "connect_only=true") {
		t.Errorf("start_args = %q, want the web client's", args)
	}
	if req.Cookies != testCookies {
		t.Errorf("socket cookies = %q, want every cookie of the session", req.Cookies)
	}
	assertBrowserHeaders(t, req.Header)
}

func TestConnectEndsInAuthExpiredWhenSlackRejectsTheSession(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, `{"ok":false,"error":"invalid_auth"}`),
		"/ws/":           socketServer(t, nil, hangUp),
	})
	p, _ := f.provider(t)

	got := rest(t, connect(t, p, context.Background()))

	if want := []domain.Event{state(t, domain.Connecting), state(t, domain.AuthExpired)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("%d requests, want only auth.test", n)
	}
}

func TestConnectEndsInDisconnectedWhenItCannotOpenTheSocket(t *testing.T) {
	for name, routes := range map[string]map[string]http.HandlerFunc{
		"auth.test fails": {"/api/auth.test": jsonBody(http.StatusBadGateway, `{}`)},
		"handshake fails": {
			"/api/auth.test": jsonBody(http.StatusOK, authOK),
			"/ws/":           jsonBody(http.StatusForbidden, `{}`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := newFakeSlack(t, routes).provider(t)

			got := rest(t, connect(t, p, context.Background()))

			if want := []domain.Event{state(t, domain.Connecting), state(t, domain.Disconnected)}; !reflect.DeepEqual(got, want) {
				t.Fatalf("events = %v, want %v", got, want)
			}
		})
	}
}

func TestConnectClosesQuietlyWhenTheContextEnds(t *testing.T) {
	frames := []string{string(fixture(t, "frames/hello.json"))}
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, frames, waitForClose),
	})
	p, _ := f.provider(t)
	ctx, cancel := context.WithCancel(context.Background())
	events := connect(t, p, ctx)
	next(t, events) // connecting
	next(t, events) // connected

	cancel()

	if got := rest(t, events); got != nil {
		t.Fatalf("events after the context ended = %v, want none", got)
	}
}

func TestASilentSocketIsDisconnected(t *testing.T) {
	frames := []string{string(fixture(t, "frames/hello.json"))}
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, frames, waitForClose),
	})
	p, _ := f.provider(t)
	p.readTimeout = 50 * time.Millisecond

	got := rest(t, connect(t, p, context.Background()))

	want := []domain.Event{state(t, domain.Connecting), state(t, domain.Connected), state(t, domain.Disconnected)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestPingsKeepTheSocketAliveAndAreAnswered(t *testing.T) {
	pongs := make(chan string, 10)
	keepPinging := func(conn *websocket.Conn) {
		conn.SetPongHandler(func(data string) error { pongs <- data; return nil })
		closed := make(chan struct{})
		go func() { waitForClose(conn); close(closed) }() // reads, so the pong handler runs
		for i := range 6 {
			time.Sleep(30 * time.Millisecond)
			if conn.WriteControl(websocket.PingMessage, []byte{byte('0' + i)}, time.Now().Add(time.Second)) != nil {
				return
			}
		}
		_ = conn.WriteMessage(websocket.TextMessage, fixture(t, "frames/user_typing.json"))
		<-closed
	}
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, nil, keepPinging),
	})
	p, _ := f.provider(t)
	p.readTimeout = 100 * time.Millisecond // shorter than the 180ms of pings
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := connect(t, p, ctx)
	next(t, events) // connecting

	if ev := next(t, events); reflect.TypeOf(ev) != reflect.TypeFor[domain.UserTyping]() {
		t.Fatalf("event = %#v, want the typing frame sent after the pings", ev)
	}
	if got := <-pongs; got != "0" {
		t.Errorf("first pong = %q, want the first ping's payload", got)
	}
}

// pingFrom is one frame the fake socket read from the provider.
type pingFrom struct {
	Type string `json:"type"`
	ID   int    `json:"id"`
}

// readPings reads the provider's frames into pings until the client
// closes the socket, answering each ping with answer (nil: no answer).
// It is the fake socket's only writer once it starts.
func readPings(pings chan<- pingFrom, answer func(conn *websocket.Conn, n int, ping pingFrom)) func(*websocket.Conn) {
	return func(conn *websocket.Conn) {
		for n := 1; ; n++ {
			var ping pingFrom
			if err := conn.ReadJSON(&ping); err != nil {
				return
			}
			pings <- ping
			if answer != nil {
				answer(conn, n, ping)
			}
		}
	}
}

func TestClientPingsAnsweredWithPongsKeepAQuietSocketAlive(t *testing.T) {
	const quietPings = 10 // 300ms of quiet, three times readTimeout
	pings := make(chan pingFrom, 2*quietPings)
	pong := func(conn *websocket.Conn, n int, ping pingFrom) {
		_ = conn.WriteJSON(map[string]any{"type": "pong", "reply_to": ping.ID})
		if n == quietPings {
			_ = conn.WriteMessage(websocket.TextMessage, fixture(t, "frames/user_typing.json"))
		}
	}
	frames := []string{string(fixture(t, "frames/hello.json"))}
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, frames, readPings(pings, pong)),
	})
	p, _ := f.provider(t)
	p.readTimeout = 100 * time.Millisecond
	p.pingInterval = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := connect(t, p, ctx)
	next(t, events) // connecting
	next(t, events) // connected

	if ev := next(t, events); reflect.TypeOf(ev) != reflect.TypeFor[domain.UserTyping]() {
		t.Fatalf("event = %#v, want the typing frame sent after %d pongs", ev, quietPings)
	}
	for want := 1; want <= quietPings; want++ {
		if got := <-pings; got != (pingFrom{Type: "ping", ID: want}) {
			t.Fatalf("ping %d = %+v, want type ping with id %d", want, got, want)
		}
	}
}

func TestPingsWithoutPongsStillEndASilentSocket(t *testing.T) {
	pings := make(chan pingFrom, 100)
	frames := []string{string(fixture(t, "frames/hello.json"))}
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/auth.test": jsonBody(http.StatusOK, authOK),
		"/ws/":           socketServer(t, frames, readPings(pings, nil)),
	})
	p, _ := f.provider(t)
	p.readTimeout = 100 * time.Millisecond
	p.pingInterval = 30 * time.Millisecond

	got := rest(t, connect(t, p, context.Background()))

	want := []domain.Event{state(t, domain.Connecting), state(t, domain.Connected), state(t, domain.Disconnected)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if len(pings) == 0 {
		t.Error("no ping reached the socket, so the test proves nothing")
	}
}
