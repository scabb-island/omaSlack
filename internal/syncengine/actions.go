package syncengine

import (
	"context"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
)

// Client actions (task 2.7). The RPC layer calls these for whatever a
// client does; the engine is the single writer, so every action that
// changes Slack also lands in the Store and is published, in that order,
// before the call returns. Provider calls go through the budget at
// Interactive priority. An action the provider cannot do (it lacks the
// optional interface or the capability) returns an error wrapping
// domain.ErrUnsupported. Every action returns ErrStopped when Run is not
// running.
//
// Once Slack has accepted a change, it is stored even if the caller gave
// up meanwhile: Slack has it, so clients must see it. Slack also echoes
// the change on the stream. The Store takes it twice without harm; a
// message event is then published twice with the same content (clients
// key messages by conversation and ts), while a reaction is published
// only once (writer.inEffect).

// The budget keys of the client actions: the Slack methods behind them.
const (
	methodPost           = "chat.postMessage"
	methodMarkRead       = "conversations.mark"
	methodEdit           = "chat.update"
	methodDelete         = "chat.delete"
	methodReactionAdd    = "reactions.add"
	methodReactionRemove = "reactions.remove"
	methodSearchMessages = "search.messages"
	methodSearchUsers    = "users.search"
	methodSetPresence    = "users.setPresence"
	methodSnoozeDND      = "dnd.setSnooze"
	methodEndDND         = "dnd.endSnooze"
)

// State is the workspace's connection state as last published.
func (e *Engine) State() domain.ConnectionState { return *e.w.shown.Load() }

// Send posts text to conv (inside thread when it is not zero), stores the
// message, publishes MessageCreated and clears the matching draft.
func (e *Engine) Send(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) (domain.Message, error) {
	m, err := ask(ctx, e, methodPost, func(ctx context.Context) (domain.Message, error) {
		return e.cfg.Provider.Post(ctx, conv, thread, text)
	})
	if err != nil {
		return domain.Message{}, err
	}
	return m, e.settle(ctx, sentOp(conv, thread, m))
}

// Edit replaces a message's text, stores it and publishes MessageUpdated.
func (e *Engine) Edit(ctx context.Context, conv domain.ConversationID, ts domain.TS, text string) (domain.Message, error) {
	ed, err := optional[provider.MessageEditor](e, "edit")
	if err != nil {
		return domain.Message{}, err
	}
	m, err := ask(ctx, e, methodEdit, func(ctx context.Context) (domain.Message, error) {
		return ed.Edit(ctx, conv, ts, text)
	})
	if err != nil {
		return domain.Message{}, err
	}
	return m, e.settle(ctx, applyOp(domain.MessageUpdated{Team: e.w.team, Conversation: conv, Message: m}))
}

// Delete deletes a message, tombstones it and publishes MessageDeleted.
func (e *Engine) Delete(ctx context.Context, conv domain.ConversationID, ts domain.TS) error {
	ed, err := optional[provider.MessageEditor](e, "delete")
	if err != nil {
		return err
	}
	if err := e.interactive(ctx, methodDelete, func(ctx context.Context) error { return ed.Delete(ctx, conv, ts) }); err != nil {
		return err
	}
	return e.settle(ctx, applyOp(domain.MessageDeleted{Team: e.w.team, Conversation: conv, TS: ts}))
}

// React adds (add) or removes the user's reaction, stores it and
// publishes ReactionChanged.
func (e *Engine) React(ctx context.Context, conv domain.ConversationID, ts domain.TS, emoji string, add bool) error {
	r, err := optional[provider.Reactor](e, "react")
	if err != nil {
		return err
	}
	method, change := methodReactionRemove, r.RemoveReaction
	if add {
		method, change = methodReactionAdd, r.AddReaction
	}
	if err := e.interactive(ctx, method, func(ctx context.Context) error { return change(ctx, conv, ts, emoji) }); err != nil {
		return err
	}
	if e.cfg.Self.IsZero() {
		return nil
	}
	return e.settle(ctx, applyOp(domain.ReactionChanged{Team: e.w.team, Conversation: conv, TS: ts, User: e.cfg.Self, Emoji: emoji, Added: add}))
}

// MarkRead moves the read marker to upTo in Slack and in the Store, and
// publishes ReadChanged and ConversationUpdated. A marker that would move
// back is a no-op (the read state is shared by every client).
func (e *Engine) MarkRead(ctx context.Context, conv domain.ConversationID, upTo domain.TS) error {
	if err := e.live(); err != nil {
		return err
	}
	current, err := e.cfg.Store.LastRead(ctx, conv)
	if err != nil || !current.Before(upTo) {
		return err
	}
	if err := e.interactive(ctx, methodMarkRead, func(ctx context.Context) error {
		return e.cfg.Provider.MarkRead(ctx, conv, upTo)
	}); err != nil {
		return err
	}
	return e.settle(ctx, readOp(conv, upTo))
}

// SetDraft stores a draft and publishes DraftChanged. It never reaches
// Slack.
func (e *Engine) SetDraft(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) error {
	if err := e.live(); err != nil {
		return err
	}
	return e.do(ctx, func(ctx context.Context, w *writer) error { return w.setDraft(ctx, conv, thread, text) })
}

// SearchMessages, SearchUsers, SetPresence, SnoozeDND and EndDND pass
// through to the provider's optional interface, through the budget. They
// touch neither the Store nor the published events.

func (e *Engine) SearchMessages(ctx context.Context, query string, limit int) ([]domain.SearchHit, error) {
	s, err := optional[provider.MessageSearcher](e, "search messages", domain.Search)
	if err != nil {
		return nil, err
	}
	return ask(ctx, e, methodSearchMessages, func(ctx context.Context) ([]domain.SearchHit, error) {
		return s.SearchMessages(ctx, query, limit)
	})
}

func (e *Engine) SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error) {
	s, err := optional[provider.UserSearcher](e, "search users")
	if err != nil {
		return nil, err
	}
	return ask(ctx, e, methodSearchUsers, func(ctx context.Context) ([]domain.User, error) {
		return s.SearchUsers(ctx, query, limit)
	})
}

func (e *Engine) SetPresence(ctx context.Context, away bool) error {
	p, err := optional[provider.PresenceSetter](e, "set presence", domain.PresenceWrite)
	if err != nil {
		return err
	}
	return e.interactive(ctx, methodSetPresence, func(ctx context.Context) error { return p.SetPresence(ctx, away) })
}

func (e *Engine) SnoozeDND(ctx context.Context, minutes int) error {
	d, err := optional[provider.DNDController](e, "snooze notifications")
	if err != nil {
		return err
	}
	return e.interactive(ctx, methodSnoozeDND, func(ctx context.Context) error { return d.SnoozeDND(ctx, minutes) })
}

func (e *Engine) EndDND(ctx context.Context) error {
	d, err := optional[provider.DNDController](e, "end the snooze")
	if err != nil {
		return err
	}
	return e.interactive(ctx, methodEndDND, d.EndDND)
}

// live is ErrStopped unless Run is serving ops. Checking first keeps an
// action from reaching Slack when its result could not be stored.
func (e *Engine) live() error {
	if !e.running.Load() {
		return ErrStopped
	}
	return nil
}

// optional finds the provider's optional interface T, once the engine is
// running, and checks it declares every capability in need.
func optional[T any](e *Engine, what string, need ...domain.Capability) (T, error) {
	var none T
	if err := e.live(); err != nil {
		return none, err
	}
	for _, c := range need {
		if err := e.cfg.Provider.Capabilities().Require(c); err != nil {
			return none, fmt.Errorf("syncengine: %s: %w", what, err)
		}
	}
	p, ok := e.cfg.Provider.(T)
	if !ok {
		return none, fmt.Errorf("syncengine: %s: %w", what, domain.ErrUnsupported)
	}
	return p, nil
}

// interactive runs fn under the budget at Interactive priority, once the
// engine is running.
func (e *Engine) interactive(ctx context.Context, method string, fn func(context.Context) error) error {
	if err := e.live(); err != nil {
		return err
	}
	return e.call(ctx, method, ratebudget.Interactive, fn)
}

// ask is interactive for a call that returns a result.
func ask[T any](ctx context.Context, e *Engine, method string, fn func(context.Context) (T, error)) (T, error) {
	var out T
	err := e.interactive(ctx, method, func(ctx context.Context) error {
		var err error
		out, err = fn(ctx)
		return err
	})
	return out, err
}

// settle stores and publishes what Slack accepted, even if the caller has
// given up meanwhile.
func (e *Engine) settle(ctx context.Context, o op) error {
	return e.do(context.WithoutCancel(ctx), o)
}

// sentOp stores and publishes a sent message, then clears the draft it
// came from.
func sentOp(conv domain.ConversationID, thread domain.TS, m domain.Message) op {
	return func(ctx context.Context, w *writer) error {
		created := applyOp(domain.MessageCreated{Team: w.team, Conversation: conv, Message: m})
		if err := created(ctx, w); err != nil {
			return err
		}
		return w.clearDraft(ctx, conv, thread)
	}
}

// setDraft stores a draft and publishes it.
func (w *writer) setDraft(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) error {
	if err := w.store.SetDraft(ctx, conv, thread, text); err != nil {
		return err
	}
	w.pub.Publish(domain.DraftChanged{Team: w.team, Conversation: conv, Thread: thread, Text: text})
	return nil
}

// clearDraft deletes a draft and publishes that, when there was one.
func (w *writer) clearDraft(ctx context.Context, conv domain.ConversationID, thread domain.TS) error {
	text, err := w.store.Draft(ctx, conv, thread)
	if err != nil || text == "" {
		return err
	}
	return w.setDraft(ctx, conv, thread, "")
}
