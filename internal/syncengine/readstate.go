package syncengine

import (
	"context"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

// The read-state fill (spec sync-engine, "Provider list without read
// state"). A provider whose list carries no latest message per
// conversation (provider.ConversationStateReader, internal-app) would
// leave unread counts at zero until messages arrive live. After each
// connection's list is stored, the engine reads every entry's state one
// conversation at a time, at Background priority through the budget:
// one conversations.info call, plus a one-message History call when that
// does not name the latest message (Slack names it for DMs, not for
// channels). Each entry goes through the single writer, which publishes
// it only when it changed.

// fillRest is the least time between the end of one full pass and the
// start of the next. In between, live events and the read-marker poll
// keep the list current.
const fillRest = 30 * time.Minute

// filler is the fill's own state. Only fillLoop's goroutine and the one
// pass it runs at a time touch it, and fillLoop reads it only once that
// pass has ended.
type filler struct {
	r        provider.ConversationStateReader
	done     map[domain.ConversationID]bool // filled in the unfinished pass
	finished time.Time                      // when the last full pass ended; zero before the first
}

// due reports whether a pass may start at now.
func (f *filler) due(now time.Time) bool {
	return f.finished.IsZero() || !now.Before(f.finished.Add(fillRest))
}

// fillLoop runs one pass per list reconcile stored, at most one at a
// time: a newer list stops the pass in progress and starts again with
// it, skipping the conversations that pass already filled. It returns
// once ctx ends and the pass in progress has stopped.
func (e *Engine) fillLoop(ctx context.Context, f *filler) {
	stop := func() {}
	defer func() { stop() }()
	for {
		select {
		case list := <-e.lists:
			stop()
			stop = e.startPass(ctx, f, list)
		case <-ctx.Done():
			return
		}
	}
}

// startPass starts a pass over list when one is due, and returns what
// stops it and waits until it has stopped.
func (e *Engine) startPass(ctx context.Context, f *filler, list []domain.Conversation) (stop func()) {
	if !f.due(e.cfg.Clock.Now()) {
		e.cfg.Log.Info("sync: read state is recent; not filling it in again yet", "team", e.w.team)
		return func() {}
	}
	pctx, cancel := context.WithCancel(ctx)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		e.pass(pctx, f, list)
	}()
	return func() { cancel(); <-ended }
}

// pass fills in every conversation fillPlan picks from list. A
// conversation that fails is logged and skipped; an ended ctx or
// rejected credentials stop the pass unfinished.
func (e *Engine) pass(ctx context.Context, f *filler, list []domain.Conversation) {
	var plan []domain.Conversation
	if err := e.do(ctx, planOp(list, &plan)); err != nil {
		return
	}
	if f.done == nil {
		f.done = make(map[domain.ConversationID]bool)
	}
	for _, c := range plan {
		if f.done[c.ID] {
			continue
		}
		if err := e.fill(ctx, f.r, c); err != nil {
			if stopping(err) || ctx.Err() != nil {
				return
			}
			e.cfg.Log.Warn("sync: could not fill in the read state of a conversation", "team", e.w.team, "conversation", c.ID, "err", err)
		}
		f.done[c.ID] = true
	}
	clear(f.done)
	f.finished = e.cfg.Clock.Now()
	e.cfg.Log.Info("sync: read state filled in", "team", e.w.team, "conversations", len(plan))
}

// fill reads c's state and stores it in c's entry.
func (e *Engine) fill(ctx context.Context, r provider.ConversationStateReader, c domain.Conversation) error {
	var got domain.Conversation
	err := e.call(ctx, methodReadMarker, ratebudget.Background, func(ctx context.Context) error {
		var err error
		got, err = r.ConversationState(ctx, c.ID)
		return err
	})
	if err != nil {
		return err
	}
	if got.Latest.IsZero() {
		page, err := e.history(ctx, c.ID, domain.TS{}, 1, ratebudget.Background)
		if err != nil {
			return err
		}
		if len(page) > 0 {
			got.Latest = page[0].TS
		}
	}
	c.Latest, c.LastRead, c.Mentions = got.Latest, got.LastRead, got.Mentions
	return e.do(ctx, fillOp(c))
}

// offerList hands a stored list to fillLoop, when the provider needs the
// fill.
func (e *Engine) offerList(ctx context.Context, list []domain.Conversation) {
	if e.lists == nil {
		return
	}
	select {
	case e.lists <- list:
	case <-ctx.Done():
	}
}

// planOp copies into out the entries of list a pass fills in, in order.
func planOp(list []domain.Conversation, out *[]domain.Conversation) op {
	return func(ctx context.Context, w *writer) error {
		w.loadViews(ctx)
		*out = fillPlan(list, func(c domain.ConversationID) bool {
			_, open := w.open[c]
			_, active := w.active[c]
			return open || active || !w.views[c].Latest.IsZero()
		})
		return nil
	}
}

// fillOp moves c's read marker, publishing it when it moved, and stores
// c's entry, publishing it when it changed.
func fillOp(c domain.Conversation) op {
	return refreshing(c.ID, func(ctx context.Context, w *writer) error {
		if err := w.moveRead(ctx, c.ID, c.LastRead); err != nil {
			return err
		}
		return w.store.UpdateConversation(ctx, c)
	})
}
