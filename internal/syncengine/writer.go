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

	status status
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
		status: status{link: domain.Disconnected},
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
