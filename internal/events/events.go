// Package events delivers domain events to subscribed clients (spec
// daemon-api, "Event subscription"; task 2.4). The Hub is every sync
// engine's syncengine.Publisher and answers events.subscribe.
//
// Delivery:
//   - A client calls events.subscribe{topics[]} and from then on receives
//     one JSON-RPC notification per event of those topics on the same
//     connection: method = topic, params = wire.FromEvent(ev). A second
//     call replaces the topic set. An unknown topic is -32602.
//   - Publish never blocks the engine: each subscriber has its own queue.
//   - State is coalesced while queued: a newer conversation.updated (team,
//     conversation), read.changed (team, conversation), presence.changed
//     (team, user), connection.state (team), draft.changed (team,
//     conversation, thread), typing (team, conversation, user) or
//     unread.summary replaces the queued one with the same key and moves to
//     the end of the queue. What a client receives is therefore always the
//     published sequence minus superseded states, in publish order: no
//     event overtakes one published before it (a conversation.updated never
//     arrives before the message.created that preceded it), and each key's
//     latest state is delivered. A key that keeps changing faster than the
//     client reads waits until it settles; the engine's state changes come
//     in bursts, so it does.
//   - message.*, reaction.changed and ui.focus are never coalesced or
//     dropped. They are the bound: a subscriber with Options.QueueSize of
//     them queued is too slow to be trusted with a consistent view, so its
//     connection is closed (rpc.Conn.Close) and logged; the client
//     reconnects and reloads. Queued state is bounded by its distinct keys
//     (conversations, users, teams), not by how many events were published,
//     so a reconcile burst on a large workspace does not count against it.
//   - unread.summary is coalesced: after anything that can change unread
//     counts (message.created, message.deleted, read.changed,
//     conversation.updated, connection.state) the hub recomputes the
//     summary with Options.Summary and sends it at most once per
//     Options.Interval, and only to subscribers it differs for: a summary
//     equal to the last one a subscriber was sent is not sent again. A new
//     subscriber to unread.summary gets the current one at once.
//   - ui.focus is not a domain event: Focus sends it to the ui.focus
//     subscribers.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// The topics that are not domain events.
const (
	TopicUnreadSummary = "unread.summary"
	TopicFocus         = "ui.focus"
)

// Topics is every topic a client may subscribe to.
var Topics = []string{
	"message.created", "message.updated", "message.deleted",
	"reaction.changed", "conversation.updated", "read.changed",
	"draft.changed", "typing", "presence.changed", "connection.state",
	TopicUnreadSummary, TopicFocus,
}

// Options configures a Hub. Summary is required.
type Options struct {
	// Summary computes the current unread summary across workspaces.
	Summary func(ctx context.Context) (wire.Summary, error)

	Interval  time.Duration // unread.summary coalescing; zero means 250 ms
	QueueSize int           // un-coalescible events queued per subscriber; zero means 1024
	Log       *slog.Logger  // default: discard

	// After starts the coalescing timer; default time.After. Tests inject
	// a clock they fire by hand.
	After func(d time.Duration) <-chan time.Time
}

// Defaults for the zero Options fields.
const (
	DefaultInterval  = 250 * time.Millisecond
	DefaultQueueSize = 1024
)

// summaryTopics are the events that can change the unread summary.
var summaryTopics = map[string]bool{
	"message.created": true, "message.deleted": true, "read.changed": true,
	"conversation.updated": true, "connection.state": true,
}

// Hub fans events out to subscribers.
type Hub struct {
	opts  Options
	dirty chan struct{} // the summary may have changed; holds at most one signal

	mu   sync.Mutex
	subs map[peer]*subscriber
}

var _ syncengine.Publisher = (*Hub)(nil)

// New returns a Hub. Call Run before publishing.
func New(opts Options) *Hub {
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = DefaultQueueSize
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	if opts.After == nil {
		opts.After = time.After
	}
	return &Hub{opts: opts, dirty: make(chan struct{}, 1), subs: make(map[peer]*subscriber)}
}

// Run sends the coalesced unread summaries until ctx ends. Events reach
// their subscribers without it.
func (h *Hub) Run(ctx context.Context) error {
	var c coalescer
	var window <-chan time.Time
	for {
		emit := false
		select {
		case <-ctx.Done():
			return nil
		case <-h.dirty:
			emit = c.change()
		case <-window:
			window = nil
			emit = c.tick()
		}
		if emit {
			h.sendSummary(ctx)
			window = h.opts.After(h.opts.Interval)
		}
	}
}

// Publish queues ev for every subscriber of its topic. It never blocks.
func (h *Hub) Publish(ev domain.Event) {
	topic := ev.Topic()
	h.fanOut(topic, stateKey(ev), func() any { return wire.FromEvent(ev) })
	if summaryTopics[topic] {
		select {
		case h.dirty <- struct{}{}:
		default: // a change is already signaled
		}
	}
}

// Focus sends f to the ui.focus subscribers and reports how many it
// reached.
func (h *Hub) Focus(f wire.FocusEvent) int {
	return h.fanOut(TopicFocus, key{}, func() any { return f })
}

// Subscribe is the events.subscribe method: params {topics[]}, result
// {topics[]} as accepted.
func (h *Hub) Subscribe() rpc.Handler {
	return rpc.HandlerFunc(func(ctx context.Context, _ string, params json.RawMessage) (any, error) {
		conn, ok := rpc.ConnFromContext(ctx)
		if !ok {
			return nil, errNoConn
		}
		topics, err := parseTopics(params)
		if err != nil {
			return nil, err
		}
		if err := h.subscribe(conn, topics); err != nil {
			return nil, err
		}
		if slices.Contains(topics, TopicUnreadSummary) {
			h.sendSummaryTo(ctx, conn)
		}
		return subscribeResult{Topics: topics}, nil
	})
}
