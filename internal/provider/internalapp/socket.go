package internalapp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// connectionStates maps the Socket Mode client's own events to connection
// states. A connection error is followed by another attempt, which emits
// connecting again.
var connectionStates = map[socketmode.EventType]domain.ConnectionState{
	socketmode.EventTypeConnecting:      domain.Connecting,
	socketmode.EventTypeConnected:       domain.Connected,
	socketmode.EventTypeConnectionError: domain.Disconnected,
}

// Connect opens Socket Mode with the app token. slack-go's client
// reconnects by itself whenever Slack asks it to or a connection drops; it
// gives up only when apps.connections.open fails for good (rejected
// credentials, for example). Then the stream ends with AuthExpired or
// Disconnected and closes, and the sync engine decides what comes next.
func (p *Provider) Connect(ctx context.Context) (<-chan domain.Event, error) {
	api := slack.New(p.creds.UserToken,
		slack.OptionAppLevelToken(p.creds.AppToken),
		slack.OptionAPIURL(p.apiURL),
		slack.OptionHTTPClient(p.http),
	)
	client := socketmode.New(api, socketmode.OptionLog(slog.NewLogLogger(p.log.Handler(), slog.LevelDebug)))
	out := make(chan domain.Event)
	go p.run(ctx, client, out)
	return out, nil
}

// run is the thin network loop: everything it decides is in needsAck,
// domainEvent and finalState. RunContext returns only when it cannot
// connect again or ctx ends, so no envelope is pending at that point.
func (p *Provider) run(ctx context.Context, client *socketmode.Client, out chan<- domain.Event) {
	defer close(out)
	done := make(chan error) // run always receives it, so no buffer
	go func() { done <- client.RunContext(ctx) }()
	for {
		select {
		case evt := <-client.Events:
			p.handle(ctx, client, evt, out)
		case err := <-done:
			if ctx.Err() == nil {
				p.emit(ctx, out, domain.ConnectionChanged{Team: p.team, State: finalState(err)})
			}
			return
		}
	}
}

// handle acknowledges evt when it is an envelope, before mapping it, so
// Slack does not redeliver events this provider drops. With no payload,
// AckCtx fails only once ctx has ended, and then nothing is left to do.
func (p *Provider) handle(ctx context.Context, client *socketmode.Client, evt socketmode.Event, out chan<- domain.Event) {
	if needsAck(evt) {
		_ = client.AckCtx(ctx, evt.Request.EnvelopeID, nil)
	}
	if ev := p.domainEvent(evt); ev != nil {
		p.emit(ctx, out, ev)
	}
}

// emit hands ev to the consumer unless ctx has ended. The check comes
// first because a select with both cases ready picks one at random. An
// event already waiting in the select when ctx ends can still go through:
// the guarantee is that the channel closes, not that nothing in flight
// arrives (see provider.Provider.Connect).
func (p *Provider) emit(ctx context.Context, out chan<- domain.Event, ev domain.Event) {
	if ctx.Err() != nil {
		return
	}
	select {
	case out <- ev:
	case <-ctx.Done():
	}
}

// needsAck reports whether evt carries a Socket Mode envelope, which Slack
// expects acknowledged within three seconds.
func needsAck(evt socketmode.Event) bool {
	return evt.Request != nil && evt.Request.EnvelopeID != ""
}

// domainEvent maps one Socket Mode event to the domain event it means, or
// nil. Malformed Events API payloads are logged and dropped.
func (p *Provider) domainEvent(evt socketmode.Event) domain.Event {
	if state, ok := connectionStates[evt.Type]; ok {
		return domain.ConnectionChanged{Team: p.team, State: state}
	}
	if evt.Type != socketmode.EventTypeEventsAPI {
		return nil
	}
	ev, err := mapCallback(p.team, evt.Request.Payload)
	if err != nil {
		p.log.Warn("internal-app: dropped an event", "team", p.team, "err", err)
	}
	return ev
}

// finalState is the state a stream ends in when Socket Mode gives up with
// err: AuthExpired when Slack rejected the credentials.
func finalState(err error) domain.ConnectionState {
	var slackErr slack.SlackErrorResponse
	if errors.As(err, &slackErr) && slackwire.IsAuthError(slackErr.Err) {
		return domain.AuthExpired
	}
	return domain.Disconnected
}
