package syncengine

import (
	"context"
	"log/slog"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// writer is the state only Run's goroutine touches: the Store's writes,
// what clients were last told about the connection, and which
// conversations matter. Nothing else reads or writes it, so it needs no
// lock; other goroutines reach it by sending an op (see Engine.do).
type writer struct {
	team  domain.TeamID
	store store.Store
	pub   Publisher
	log   *slog.Logger
	page  int // rows per Store read

	status status
	open   map[domain.ConversationID]int  // how many clients have it open
	active map[domain.ConversationID]bool // had message activity since startup
}

// op is one unit of work on the writer's goroutine.
type op func(ctx context.Context, w *writer) error

// request carries an op and returns its result.
type request struct {
	op   op
	done chan error // buffered: the writer never waits on the caller
}

func newWriter(cfg Config) *writer {
	return &writer{
		team:   cfg.Provider.Team(),
		store:  cfg.Store,
		pub:    cfg.Publisher,
		log:    cfg.Log,
		page:   cfg.PageSize,
		status: status{link: domain.Disconnected},
		open:   make(map[domain.ConversationID]int),
		active: make(map[domain.ConversationID]bool),
	}
}

// serve runs ops one at a time until ctx ends. An op that arrives once
// ctx has ended is refused, so nothing is written after that.
func (w *writer) serve(ctx context.Context, ops <-chan request) {
	for {
		select {
		case r := <-ops:
			r.done <- w.run(ctx, r.op)
		case <-ctx.Done():
			return
		}
	}
}

func (w *writer) run(ctx context.Context, o op) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return o(ctx, w)
}

// apply stores ev and then publishes it. Connection states are not
// stored: they only move the status clients see.
func (w *writer) apply(ctx context.Context, ev domain.Event) error {
	if cc, ok := ev.(domain.ConnectionChanged); ok {
		w.setLink(cc.State)
		return nil
	}
	if err := w.store.Apply(ctx, ev); err != nil {
		return err
	}
	if conv, ok := activityIn(ev); ok {
		w.active[conv] = true
	}
	w.pub.Publish(ev)
	return nil
}

func (w *writer) setLink(s domain.ConnectionState) {
	w.update(func(st *status) { st.link = s })
}

// update changes the status and tells clients when what they see changed.
func (w *writer) update(change func(*status)) {
	before := w.status.shown()
	change(&w.status)
	if now := w.status.shown(); now != before {
		w.pub.Publish(domain.ConnectionChanged{Team: w.team, State: now})
	}
}

// applyOp and the other *Op helpers wrap writer methods as ops.
func applyOp(ev domain.Event) op {
	return func(ctx context.Context, w *writer) error { return w.apply(ctx, ev) }
}

func linkOp(s domain.ConnectionState) op {
	return func(_ context.Context, w *writer) error { w.setLink(s); return nil }
}

// limitOp counts one more (delta 1) or one fewer (delta -1) call waiting
// out a 429.
func limitOp(delta int) op {
	return func(_ context.Context, w *writer) error {
		w.update(func(st *status) { st.limited += delta })
		return nil
	}
}

// catchUp stores fetched (newest first) and publishes, oldest first, the
// messages the Store did not have.
func (w *writer) catchUp(ctx context.Context, conv domain.ConversationID, fetched []domain.Message) error {
	known, err := w.storedSince(ctx, conv, fetched[len(fetched)-1].TS)
	if err != nil {
		return err
	}
	if err := w.store.PutHistory(ctx, conv, fetched); err != nil {
		return err
	}
	for _, m := range missed(fetched, known) {
		w.pub.Publish(domain.MessageCreated{Team: w.team, Conversation: conv, Message: m})
	}
	return nil
}

// storedSince is the set of conv's top-level messages in the Store from
// since on (and possibly a few older).
func (w *writer) storedSince(ctx context.Context, conv domain.ConversationID, since domain.TS) (map[domain.TS]bool, error) {
	msgs, err := pageBack(func(before domain.TS) ([]domain.Message, error) {
		return w.store.Messages(ctx, conv, before, w.page)
	}, func(page []domain.Message) bool {
		return len(page) < w.page || page[len(page)-1].TS.Before(since)
	})
	known := make(map[domain.TS]bool, len(msgs))
	for _, m := range msgs {
		known[m.TS] = true
	}
	return known, err
}

// moveRead stores and publishes conv's read marker when at moves it
// forward. A marker never moves back (store.Store.Apply), so an older one
// is not news.
func (w *writer) moveRead(ctx context.Context, conv domain.ConversationID, at domain.TS) error {
	current, err := w.store.LastRead(ctx, conv)
	if err != nil || !current.Before(at) {
		return err
	}
	return w.apply(ctx, domain.ReadChanged{Team: w.team, Conversation: conv, LastRead: at})
}

func putOp(conv domain.ConversationID, msgs []domain.Message) op {
	return func(ctx context.Context, w *writer) error { return w.store.PutHistory(ctx, conv, msgs) }
}

func catchUpOp(conv domain.ConversationID, fetched []domain.Message) op {
	return func(ctx context.Context, w *writer) error { return w.catchUp(ctx, conv, fetched) }
}

// openOp counts one client more (delta 1) or fewer (delta -1) with conv
// open; a conversation nobody has open is forgotten.
func openOp(conv domain.ConversationID, delta int) op {
	return func(_ context.Context, w *writer) error {
		w.open[conv] += delta
		if w.open[conv] <= 0 {
			delete(w.open, conv)
		}
		return nil
	}
}
