package syncengine

import (
	"cmp"
	"context"
	"slices"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

// The signed-in user's own Do Not Disturb snooze and presence (task 5.3).
// The engine keeps the latest of each, from whichever source spoke last:
// Slack's dnd.info on every connect, users.getPresence when a client asks
// before anything else said, the stream (dnd_updated,
// manual_presence_change and presence_change about the user), and the
// user's own SnoozeDND, EndDND and SetPresence. Every change is published
// (DNDChanged, PresenceChanged with the user's ID); a repeat is not.
// Clients start from "not snoozed" and "active", so learning either for
// the first time is not news either.

// The budget keys of the reads and of the presence subscription (a socket
// frame).
const (
	methodDNDInfo     = "dnd.info"
	methodGetPresence = "users.getPresence"
	methodPresenceSub = "presence_sub"
)

// presenceSubLimit bounds the presence subscription: the user and the
// partners of their most recent DMs, which is what a sidebar shows.
const presenceSubLimit = 100

// own is what the engine last learned of the user's own state; nil until
// learned. Only the writer touches it.
type own struct {
	dnd      *time.Time
	presence *domain.Presence
}

// resolve fills in the user's ID on a PresenceChanged about the signed-in
// user (a zero User: the provider does not know who that is), and drops
// it when the engine does not know either.
func (w *writer) resolve(ev domain.Event) (domain.Event, bool) {
	pc, ok := ev.(domain.PresenceChanged)
	if !ok || !pc.User.IsZero() {
		return ev, true
	}
	pc.User = w.self
	return pc, !w.self.IsZero()
}

// repeats reports whether ev tells clients nothing new about the user's
// own state, and remembers it otherwise.
func (w *writer) repeats(ev domain.Event) bool {
	switch e := ev.(type) {
	case domain.DNDChanged:
		known := cmp.Or(w.own.dnd, new(time.Time))
		w.own.dnd = &e.Until
		return known.Equal(e.Until)
	case domain.PresenceChanged:
		return e.User == w.self && w.learnPresence(e.Presence)
	}
	return false
}

// learnPresence remembers the user's presence and reports whether it is
// what clients already assume.
func (w *writer) learnPresence(p domain.Presence) bool {
	known := domain.Active
	if w.own.presence != nil {
		known = *w.own.presence
	}
	w.own.presence = &p
	return known == p
}

// ownOp copies what the writer knows of the user's own state into out.
func ownOp(out *own) op {
	return func(_ context.Context, w *writer) error {
		*out = w.own
		return nil
	}
}

// DND returns when the user's snooze ends, zero when it is off: what the
// engine last learned, or Slack's dnd.info when it has learned nothing
// yet.
func (e *Engine) DND(ctx context.Context) (time.Time, error) {
	d, err := optional[provider.DNDController](e, "read do not disturb")
	if err != nil {
		return time.Time{}, err
	}
	return learn(ctx, e, func(o own) *time.Time { return o.dnd }, func(ctx context.Context) (time.Time, error) {
		return e.readDND(ctx, d, ratebudget.Interactive)
	})
}

// Presence returns the user's own presence: what the engine last learned,
// or Slack's users.getPresence when it has learned nothing yet.
func (e *Engine) Presence(ctx context.Context) (domain.Presence, error) {
	r, err := optional[provider.PresenceReader](e, "read presence")
	if err != nil {
		return 0, err
	}
	return learn(ctx, e, func(o own) *domain.Presence { return o.presence }, func(ctx context.Context) (domain.Presence, error) {
		p, err := ask(ctx, e, methodGetPresence, r.Presence)
		if err != nil {
			return 0, err
		}
		return p, e.settle(ctx, applyOp(domain.PresenceChanged{Team: e.w.team, User: e.cfg.Self, Presence: p}))
	})
}

// learn returns what the writer knows (known picks it out), or fetches it.
func learn[T any](ctx context.Context, e *Engine, known func(own) *T, fetch func(context.Context) (T, error)) (T, error) {
	var o own
	if err := e.do(ctx, ownOp(&o)); err != nil {
		var zero T
		return zero, err
	}
	if v := known(o); v != nil {
		return *v, nil
	}
	return fetch(ctx)
}

// readDND asks Slack for the snooze at priority p, then stores and
// publishes it.
func (e *Engine) readDND(ctx context.Context, d provider.DNDController, p ratebudget.Priority) (time.Time, error) {
	var until time.Time
	err := e.call(ctx, methodDNDInfo, p, func(ctx context.Context) error {
		var err error
		until, err = d.DND(ctx)
		return err
	})
	if err != nil {
		return time.Time{}, err
	}
	return until, e.settle(ctx, applyOp(domain.DNDChanged{Team: e.w.team, Until: until}))
}

// SetPresence sets the user's presence (away, or auto) and publishes it:
// auto is published as active, which the user is while connected.
func (e *Engine) SetPresence(ctx context.Context, away bool) error {
	p, err := optional[provider.PresenceSetter](e, "set presence", domain.PresenceWrite)
	if err != nil {
		return err
	}
	if err := e.interactive(ctx, methodSetPresence, func(ctx context.Context) error { return p.SetPresence(ctx, away) }); err != nil {
		return err
	}
	now := domain.Active
	if away {
		now = domain.Away
	}
	return e.settle(ctx, applyOp(domain.PresenceChanged{Team: e.w.team, User: e.cfg.Self, Presence: now}))
}

// SnoozeDND snoozes notifications for minutes and publishes when the
// snooze ends.
func (e *Engine) SnoozeDND(ctx context.Context, minutes int) error {
	d, err := optional[provider.DNDController](e, "snooze notifications")
	if err != nil {
		return err
	}
	until, err := ask(ctx, e, methodSnoozeDND, func(ctx context.Context) (time.Time, error) { return d.SnoozeDND(ctx, minutes) })
	if err != nil {
		return err
	}
	return e.settle(ctx, applyOp(domain.DNDChanged{Team: e.w.team, Until: until}))
}

// EndDND ends the snooze and publishes that.
func (e *Engine) EndDND(ctx context.Context) error {
	d, err := optional[provider.DNDController](e, "end the snooze")
	if err != nil {
		return err
	}
	if err := e.interactive(ctx, methodEndDND, d.EndDND); err != nil {
		return err
	}
	return e.settle(ctx, applyOp(domain.DNDChanged{Team: e.w.team}))
}

// refreshOwn runs after every connection, at Background priority: the
// snooze may have changed while omaslack was not connected. A failure is
// logged; DND asks again when a client wants it.
func (e *Engine) refreshOwn(ctx context.Context) {
	d, ok := e.cfg.Provider.(provider.DNDController)
	if !ok {
		return
	}
	if _, err := e.readDND(ctx, d, ratebudget.Background); err != nil && !stopping(err) {
		e.cfg.Log.Warn("sync: could not read do not disturb", "team", e.w.team, "err", err)
	}
}

// subscribePresence asks for the presence of the user and of the
// partners of their most recent DMs in list, when the provider streams
// presence only for subscribed users.
func (e *Engine) subscribePresence(ctx context.Context, list []domain.Conversation) {
	s, ok := e.cfg.Provider.(provider.PresenceSubscriber)
	if !ok {
		return
	}
	users := presenceSet(e.cfg.Self, list)
	err := e.call(ctx, methodPresenceSub, ratebudget.Background, func(ctx context.Context) error {
		return s.SubscribePresence(ctx, users)
	})
	if err != nil && !stopping(err) {
		e.cfg.Log.Warn("sync: could not subscribe to presence", "team", e.w.team, "err", err)
	}
}

// presenceSet is self (when known) and the partners of list's DMs, the
// most recent first, each once, at most presenceSubLimit in all.
func presenceSet(self domain.UserID, list []domain.Conversation) []domain.UserID {
	dms := slices.DeleteFunc(slices.Clone(list), func(c domain.Conversation) bool {
		return c.Kind != domain.DirectMessage || c.User.IsZero()
	})
	slices.SortStableFunc(dms, func(a, b domain.Conversation) int { return newestFirst(a.Latest, b.Latest) })
	users := make([]domain.UserID, 0, len(dms)+1)
	if !self.IsZero() {
		users = append(users, self)
	}
	for _, c := range dms {
		if !slices.Contains(users, c.User) {
			users = append(users, c.User)
		}
	}
	return users[:min(len(users), presenceSubLimit)]
}

// newestFirst orders a before b when a is newer.
func newestFirst(a, b domain.TS) int {
	switch {
	case b.Before(a):
		return -1
	case a.Before(b):
		return 1
	}
	return 0
}
