// Adapted from github.com/gammons/slk internal/slack/client.go and internal/slack/connection.go (MIT, Copyright (c) 2026 Grant Ammons).

package desktopsession

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// defaultReadTimeout is how long the socket may stay silent before it is
// taken for dead. Slack pings about every 30 seconds, and every frame or
// ping restarts the clock, so a dropped TCP connection (a wifi change, a
// suspend) is noticed within a minute instead of never.
const defaultReadTimeout = 60 * time.Second

// agentVersion is the web client build the socket declares: a unix
// timestamp, the _x_version_ts of the web client's API calls. Checked
// against Slack on 2026-11-26: an empty agent_version (what slk sent) gets
// a socket that never says hello, "1" gets an error frame, and any
// plausible build time, even a years-old one, gets hello. This is the build
// the web client used that day.
const agentVersion = "1795655257"

// startArgs are the web client's start arguments, key for key and in its
// order, as the web client sent them on 2026-11-26 (the same keys slk
// captured). With fewer of them Slack stopped sending user_typing.
const startArgs = "?agent=client&org_wide_aware=true&agent_version=" + agentVersion + "&eac_cache_ts=true&cache_ts=0" +
	"&name_tagging=true&only_self_subteams=true&connect_only=true&ms_latest=true"

// Connect opens the web client's WebSocket. It first checks the session
// with auth.test, because the socket URL carries the token and a rejected
// one would only show up as a failed handshake. The stream starts with
// Connecting, says Connected when Slack's hello arrives, and ends with
// AuthExpired (Slack rejected the session) or Disconnected (anything
// else) before it closes. It never reconnects: that is the sync engine's
// job. Once ctx ends it closes without another event.
func (p *Provider) Connect(ctx context.Context) (<-chan domain.Event, error) {
	out := make(chan domain.Event)
	go p.run(ctx, out)
	return out, nil
}

func (p *Provider) run(ctx context.Context, out chan<- domain.Event) {
	defer close(out)
	provider.Emit(ctx, out, p.state(domain.Connecting))
	conn, err := p.open(ctx)
	if err != nil {
		p.log.Warn("desktop-session: cannot open the socket", "team", p.team, "err", err)
		provider.Emit(ctx, out, p.state(finalState(err)))
		return
	}
	defer conn.Close()
	// Writes may use the socket from now until the stream ends; the
	// deferred clear runs before the deferred Close.
	p.live.set(conn)
	defer p.live.clear(conn)
	// Closing the connection is what unblocks a read when ctx ends.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	err = p.read(ctx, conn, out)
	p.log.Info("desktop-session: socket closed", "team", p.team, "err", err)
	provider.Emit(ctx, out, p.state(domain.Disconnected))
}

// open checks the session and dials the socket with the same cookies and
// headers as the Web API calls.
func (p *Provider) open(ctx context.Context) (*websocket.Conn, error) {
	if err := p.Call(ctx, "auth.test", url.Values{}, &slackwire.Envelope{}); err != nil {
		return nil, err
	}
	header := http.Header{}
	p.browserHeaders(header)
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, p.socketAddress(), header)
	if err != nil {
		return nil, fmt.Errorf("open socket: %w", err)
	}
	_ = resp.Body.Close()
	// Every ping restarts the read deadline; gorilla's own handler still
	// answers it.
	answer := conn.PingHandler()
	conn.SetPingHandler(func(data string) error {
		_ = conn.SetReadDeadline(time.Now().Add(p.readTimeout))
		return answer(data)
	})
	return conn, nil
}

// socketAddress is the web client's socket URL for this team, its query
// in the web client's order.
func (p *Provider) socketAddress() string {
	return fmt.Sprintf(
		"%s?token=%s&sync_desync=1&slack_client=desktop&start_args=%s&no_query_on_subscribe=1"+
			"&flannel=3&lazy_channels=1&gateway_server=%s-1&batch_presence_aware=1",
		p.socketURL, url.QueryEscape(p.creds.Token), url.QueryEscape(startArgs), p.team,
	)
}

// read emits the events of every frame until the socket fails, goes
// silent for readTimeout, or is closed. Malformed frames are logged and
// dropped.
func (p *Provider) read(ctx context.Context, conn *websocket.Conn, out chan<- domain.Event) error {
	greeted := false
	for {
		_ = conn.SetReadDeadline(time.Now().Add(p.readTimeout))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return withoutHello(err, greeted)
		}
		evs, err := mapFrame(p.team, data)
		if err != nil {
			p.log.Warn("desktop-session: dropped a frame", "team", p.team, "err", err)
		}
		for _, ev := range evs {
			greeted = greeted || isHello(ev)
			provider.Emit(ctx, out, ev)
		}
	}
}

// errNoHello marks a socket that closed or went silent before Slack's
// hello: Slack accepted the handshake but not what the socket URL asked
// for, which is how a change in the web client protocol shows up.
var errNoHello = errors.New("the socket never sent hello (the web client protocol may have changed)")

func withoutHello(err error, greeted bool) error {
	if greeted {
		return err
	}
	return fmt.Errorf("%w: %w", errNoHello, err)
}

func isHello(ev domain.Event) bool {
	c, ok := ev.(domain.ConnectionChanged)
	return ok && c.State == domain.Connected
}

func (p *Provider) state(s domain.ConnectionState) domain.Event {
	return domain.ConnectionChanged{Team: p.team, State: s}
}

// finalState is the state a stream ends in when the socket cannot open
// because of err: AuthExpired when Slack rejected the session.
func finalState(err error) domain.ConnectionState {
	if errors.Is(err, provider.ErrAuthExpired) {
		return domain.AuthExpired
	}
	return domain.Disconnected
}
