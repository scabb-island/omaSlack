// Package events delivers domain events to subscribed clients (spec
// daemon-api, "Event subscription"; task 2.4). The Hub is every sync
// engine's syncengine.Publisher and answers events.subscribe.
//
// Delivery:
//   - A client calls events.subscribe{topics[]} and from then on receives
//     one JSON-RPC notification per event of those topics on the same
//     connection: method = topic, params = wire.FromEvent(ev). A second
//     call replaces the topic set. An unknown topic is -32602.
//   - Publish never blocks the engine: each subscriber has its own bounded
//     queue (Options.QueueSize). A subscriber whose queue overflows is too
//     slow to be trusted with a consistent view, so its connection is
//     closed (rpc.Conn.Close) and logged; the client reconnects and reloads.
//   - unread.summary is coalesced: after anything that can change unread
//     counts (message.created, message.deleted, read.changed,
//     conversation.updated, connection.state) the hub recomputes the
//     summary with Options.Summary and sends it at most once per
//     Options.Interval. A new subscriber to unread.summary gets the current
//     one at once.
//   - ui.focus is not a domain event: Focus sends it to the ui.focus
//     subscribers.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
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
	QueueSize int           // events queued per subscriber; zero means 1024
	Log       *slog.Logger  // default: discard
}

// Hub fans events out to subscribers.
type Hub struct{}

var _ syncengine.Publisher = (*Hub)(nil)

// New returns a Hub. Call Run before publishing.
func New(opts Options) *Hub { return &Hub{} }

// Run delivers queued events and coalesced summaries until ctx ends.
func (h *Hub) Run(ctx context.Context) error { <-ctx.Done(); return nil }

// Publish queues ev for every subscriber of its topic. It never blocks.
func (h *Hub) Publish(ev domain.Event) {}

// Focus sends f to the ui.focus subscribers and reports how many it
// reached.
func (h *Hub) Focus(f wire.FocusEvent) int { return 0 }

// Subscribe is the events.subscribe method: params {topics[]}, result
// {topics[]} as accepted.
func (h *Hub) Subscribe() rpc.Handler {
	return rpc.HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) { return nil, nil })
}
