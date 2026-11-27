package notify

import (
	"cmp"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Sender shows a notification and returns its ID, which a click
// (Notifier.Clicked) or a close (Notifier.Closed) names later.
type Sender interface {
	Notify(ctx context.Context, n Notification) (uint32, error)
}

// Lookup completes a new message with what the rules and the text need:
// its conversation, the signed-in user and the names to show.
type Lookup func(ctx context.Context, ev domain.MessageCreated) (Details, error)

// Focus is what a click opens: a conversation, at a message (for a
// reply, its thread's root).
type Focus struct {
	Team domain.TeamID
	Conv domain.ConversationID
	TS   domain.TS
}

// Options configures a Notifier. Sender and Lookup are required.
type Options struct {
	Sender Sender
	Lookup Lookup
	// Open opens a clicked notification's target in the client the config
	// prefers. nil: clicks do nothing.
	Open func(Focus, Open)
	// Views is what the clients view; nil: none.
	Views *Views
	// Started is when the daemon started; messages older than that were
	// pending then and never notify.
	Started time.Time
	Now     func() time.Time // default time.Now
	Log     *slog.Logger     // default discard
	Queue   int              // events waiting for Run; zero means 256
}

// seenSize bounds how many messages the Notifier remembers having
// decided, so a message published again (a reconnect's catch-up) is not
// notified twice.
const seenSize = 1024

// Notifier decides and sends the notifications. Publish feeds it events
// (it is a syncengine.Publisher); Run decides them one at a time.
type Notifier struct {
	opts   Options
	events chan domain.Event
	cfg    atomic.Pointer[Config]

	// Only Run touches these.
	dnd  map[domain.TeamID]time.Time
	seen *seen

	mu    sync.Mutex
	shown map[uint32]Focus // notifications on screen, by ID
}

// New returns a Notifier with DefaultConfig. Call Run to start it.
func New(opts Options) *Notifier {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	opts.Log = cmp.Or(opts.Log, slog.New(slog.DiscardHandler))
	opts.Queue = cmp.Or(opts.Queue, 256)
	if opts.Views == nil {
		opts.Views = &Views{}
	}
	n := &Notifier{
		opts: opts, events: make(chan domain.Event, opts.Queue),
		dnd: map[domain.TeamID]time.Time{}, seen: newSeen(seenSize), shown: map[uint32]Focus{},
	}
	n.SetConfig(DefaultConfig())
	return n
}

// SetConfig replaces the configuration, for messages from now on.
func (n *Notifier) SetConfig(cfg Config) { n.cfg.Store(&cfg) }

// Config is the configuration in use.
func (n *Notifier) Config() Config { return *n.cfg.Load() }

// Publish queues the events the notifier needs (new messages and Do Not
// Disturb changes) and ignores the rest. It never blocks: when Run falls
// behind, the event is dropped and logged.
func (n *Notifier) Publish(ev domain.Event) {
	switch ev.(type) {
	case domain.MessageCreated, domain.DNDChanged:
	default:
		return
	}
	select {
	case n.events <- ev:
	default:
		n.opts.Log.Warn("notify: falling behind; an event is dropped", "topic", ev.Topic(), "team", ev.TeamID())
	}
}

// Run decides the queued events until ctx ends.
func (n *Notifier) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-n.events:
			n.handle(ctx, ev)
		}
	}
}

func (n *Notifier) handle(ctx context.Context, ev domain.Event) {
	switch e := ev.(type) {
	case domain.DNDChanged:
		n.dnd[e.Team] = e.Until
	case domain.MessageCreated:
		if n.seen.add(e.Team, e.Conversation, e.Message.TS) {
			n.message(ctx, e)
		}
	}
}

// message notifies e when the rules say so.
func (n *Notifier) message(ctx context.Context, e domain.MessageCreated) {
	d, err := n.opts.Lookup(ctx, e)
	if err != nil {
		n.opts.Log.Warn("notify: cannot weigh a message", "team", e.Team, "conv", e.Conversation, "err", err)
		return
	}
	mo := Moment{
		Started: domain.TSAt(n.opts.Started), Now: n.opts.Now(), DND: n.dnd[e.Team],
		Viewed: n.opts.Views.Shows(e.Team, e.Conversation, e.Message),
	}
	reason := Decide(n.Config(), mo, d.Candidate)
	if reason == Skip {
		return
	}
	id, err := n.opts.Sender.Notify(ctx, Format(d))
	if err != nil {
		n.opts.Log.Warn("notify: cannot show a notification", "team", e.Team, "conv", e.Conversation, "err", err)
		return
	}
	n.opts.Log.Debug("notify: shown", "team", e.Team, "conv", e.Conversation, "reason", reason, "id", id)
	n.remember(id, focusOf(e))
}

func focusOf(e domain.MessageCreated) Focus {
	at := e.Message.TS
	if e.Message.IsReply() {
		at = e.Message.ThreadTS
	}
	return Focus{Team: e.Team, Conv: e.Conversation, TS: at}
}

func (n *Notifier) remember(id uint32, f Focus) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.shown[id] = f
}

// Clicked is the default action of notification id: open its target.
// An ID the notifier did not show is ignored (other applications share
// the notification service).
func (n *Notifier) Clicked(id uint32) {
	n.mu.Lock()
	t, ok := n.shown[id]
	n.mu.Unlock()
	if ok && n.opts.Open != nil {
		n.opts.Open(t, n.Config().Open)
	}
}

// Closed forgets notification id: it is no longer on screen.
func (n *Notifier) Closed(id uint32) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.shown, id)
}

// seen is a bounded set of messages, forgetting the oldest first.
type seen struct {
	keys  map[seenKey]bool
	order []seenKey
	next  int
}

type seenKey struct {
	team domain.TeamID
	conv domain.ConversationID
	ts   domain.TS
}

func newSeen(size int) *seen {
	return &seen{keys: make(map[seenKey]bool, size), order: make([]seenKey, size)}
}

// add records the message and reports whether it is new.
func (s *seen) add(team domain.TeamID, conv domain.ConversationID, ts domain.TS) bool {
	k := seenKey{team, conv, ts}
	if s.keys[k] {
		return false
	}
	delete(s.keys, s.order[s.next])
	s.order[s.next] = k
	s.next = (s.next + 1) % len(s.order)
	s.keys[k] = true
	return true
}
